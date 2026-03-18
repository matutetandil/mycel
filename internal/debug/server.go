package debug

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
)

// Server is the Mycel Studio Debug Protocol server.
// Handles WebSocket connections from IDE clients at /debug on the admin server.
type Server struct {
	inspector RuntimeInspector
	stream    *EventStream
	logger    *slog.Logger

	mu       sync.Mutex
	sessions map[string]*Session
	nextID   atomic.Uint64

	// OnClientChange is called when the number of connected clients changes
	// from 0→1 (true) or 1→0 (false). Used to toggle debug throttling.
	OnClientChange func(hasClients bool)

	upgrader websocket.Upgrader
}

// NewServer creates a new debug protocol server.
func NewServer(inspector RuntimeInspector, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Server{
		inspector: inspector,
		stream:    NewEventStream(),
		logger:    logger,
		sessions:  make(map[string]*Session),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for local development
			},
		},
	}
}

// Stream returns the event stream for injecting into flow contexts.
func (s *Server) Stream() *EventStream {
	return s.stream
}

// GetSession returns a session by ID.
func (s *Server) GetSession(id string) (*Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	return sess, ok
}

// ActiveSession returns the first active session (for single-client scenarios).
func (s *Server) ActiveSession() *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sess := range s.sessions {
		return sess
	}
	return nil
}

// HasClients returns true if any debug client is connected.
func (s *Server) HasClients() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions) > 0
}

// RegisterHandlers mounts the debug WebSocket endpoint on the given mux.
func (s *Server) RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/debug", s.handleWebSocket)
}

// handleWebSocket upgrades HTTP to WebSocket and runs the session loop.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("websocket upgrade failed", "error", err)
		return
	}
	defer conn.Close()

	// Session is created on attach, not on connect
	var session *Session
	sessionID := ""

	// Subscribe to events once session is created
	var eventCh <-chan *Notification
	defer func() {
		if sessionID != "" {
			s.stream.Unsubscribe(sessionID)
			s.mu.Lock()
			delete(s.sessions, sessionID)
			nowEmpty := len(s.sessions) == 0
			s.mu.Unlock()

			// Notify when last client disconnects (disable debug throttling)
			if nowEmpty && s.OnClientChange != nil {
				s.OnClientChange(false)
			}

			s.logger.Info("debug client disconnected", "session", sessionID)
		}
	}()

	// Event writer goroutine
	writeMu := &sync.Mutex{}
	writeJSON := func(v interface{}) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(v)
	}

	// Start event forwarding once session exists
	startEventForwarding := func() {
		go func() {
			for n := range eventCh {
				if err := writeJSON(n); err != nil {
					return
				}
			}
		}()
	}

	// Read loop
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				s.logger.Warn("websocket read error", "error", err)
			}
			return
		}

		var req Request
		if err := json.Unmarshal(message, &req); err != nil {
			writeJSON(newErrorResponse(nil, CodeParseError, "invalid JSON"))
			continue
		}

		if req.JSONRPC != "2.0" {
			writeJSON(newErrorResponse(req.ID, CodeInvalidRequest, "expected jsonrpc 2.0"))
			continue
		}

		// Handle attach specially — creates the session
		if req.Method == "debug.attach" {
			var params AttachParams
			if req.Params != nil {
				json.Unmarshal(req.Params, &params)
			}

			sessionID = fmt.Sprintf("s%d", s.nextID.Add(1))
			session = NewSession(sessionID, params.ClientName)

			s.mu.Lock()
			wasEmpty := len(s.sessions) == 0
			s.sessions[sessionID] = session
			s.mu.Unlock()

			// Notify when first client connects (enable debug throttling)
			if wasEmpty && s.OnClientChange != nil {
				s.OnClientChange(true)
			}

			eventCh = s.stream.Subscribe(sessionID)
			startEventForwarding()

			flows := s.inspector.ListFlows()
			writeJSON(newResponse(req.ID, &AttachResult{
				SessionID: sessionID,
				Flows:     flows,
			}))

			s.logger.Info("debug client attached", "session", sessionID, "client", params.ClientName)
			continue
		}

		// All other methods require an active session
		if session == nil {
			writeJSON(newErrorResponse(req.ID, CodeSessionNotFound, "not attached"))
			continue
		}

		resp := s.handleMethod(session, &req)
		writeJSON(resp)
	}
}

// handleMethod dispatches a JSON-RPC method to the appropriate handler.
func (s *Server) handleMethod(session *Session, req *Request) *Response {
	switch req.Method {
	case "debug.detach":
		return newResponse(req.ID, map[string]bool{"ok": true})

	case "debug.setBreakpoints":
		return s.handleSetBreakpoints(session, req)

	case "debug.continue":
		return s.handleContinue(session, req)

	case "debug.next":
		return s.handleNext(session, req)

	case "debug.stepInto":
		return s.handleStepInto(session, req)

	case "debug.evaluate":
		return s.handleEvaluate(session, req)

	case "debug.variables":
		return s.handleVariables(session, req)

	case "debug.threads":
		return s.handleThreads(session, req)

	case "inspect.flows":
		return s.handleInspectFlows(req)

	case "inspect.flow":
		return s.handleInspectFlow(req)

	case "inspect.connectors":
		return s.handleInspectConnectors(req)

	case "inspect.types":
		return s.handleInspectTypes(req)

	case "inspect.transforms":
		return s.handleInspectTransforms(req)

	default:
		return newErrorResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("unknown method: %s", req.Method))
	}
}

// --- Debug methods ---

func (s *Server) handleSetBreakpoints(session *Session, req *Request) *Response {
	var params SetBreakpointsParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return newErrorResponse(req.ID, CodeInvalidParams, err.Error())
	}

	session.SetBreakpoints(params.Flow, params.Breakpoints)
	return newResponse(req.ID, &SetBreakpointsResult{
		Breakpoints: params.Breakpoints,
	})
}

func (s *Server) handleContinue(session *Session, req *Request) *Response {
	var params ContinueParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return newErrorResponse(req.ID, CodeInvalidParams, err.Error())
	}

	thread, ok := session.GetThread(params.ThreadID)
	if !ok {
		return newErrorResponse(req.ID, CodeThreadNotFound, "thread not found")
	}

	thread.SetStepInto(false)
	thread.Resume(actionContinue)
	return newResponse(req.ID, map[string]bool{"ok": true})
}

func (s *Server) handleNext(session *Session, req *Request) *Response {
	var params NextParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return newErrorResponse(req.ID, CodeInvalidParams, err.Error())
	}

	thread, ok := session.GetThread(params.ThreadID)
	if !ok {
		return newErrorResponse(req.ID, CodeThreadNotFound, "thread not found")
	}

	thread.SetStepInto(false)
	thread.Resume(actionNext)
	return newResponse(req.ID, map[string]bool{"ok": true})
}

func (s *Server) handleStepInto(session *Session, req *Request) *Response {
	var params StepIntoParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return newErrorResponse(req.ID, CodeInvalidParams, err.Error())
	}

	thread, ok := session.GetThread(params.ThreadID)
	if !ok {
		return newErrorResponse(req.ID, CodeThreadNotFound, "thread not found")
	}

	thread.SetStepInto(true)
	thread.Resume(actionStepInto)
	return newResponse(req.ID, map[string]bool{"ok": true})
}

func (s *Server) handleEvaluate(session *Session, req *Request) *Response {
	var params EvaluateParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return newErrorResponse(req.ID, CodeInvalidParams, err.Error())
	}

	thread, ok := session.GetThread(params.ThreadID)
	if !ok {
		return newErrorResponse(req.ID, CodeThreadNotFound, "thread not found")
	}

	if !thread.IsPaused() {
		return newErrorResponse(req.ID, CodeInvalidParams, "thread not paused")
	}

	transformer := s.inspector.GetCELTransformer()
	if transformer == nil {
		return newErrorResponse(req.ID, CodeInternalError, "no CEL transformer available")
	}

	result, err := thread.EvaluateCEL(transformer, params.Expression)
	if err != nil {
		return newErrorResponse(req.ID, CodeEvalError, err.Error())
	}

	return newResponse(req.ID, &EvaluateResult{
		Result: result,
		Type:   fmt.Sprintf("%T", result),
	})
}

func (s *Server) handleVariables(session *Session, req *Request) *Response {
	var params VariablesParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return newErrorResponse(req.ID, CodeInvalidParams, err.Error())
	}

	thread, ok := session.GetThread(params.ThreadID)
	if !ok {
		return newErrorResponse(req.ID, CodeThreadNotFound, "thread not found")
	}

	return newResponse(req.ID, thread.GetVariables())
}

func (s *Server) handleThreads(session *Session, req *Request) *Response {
	threads := session.ListThreads()
	infos := make([]ThreadInfo, len(threads))
	for i, t := range threads {
		stage, name, _, _ := t.GetState()
		infos[i] = ThreadInfo{
			ID:       t.ID,
			FlowName: t.FlowName,
			Stage:    stage,
			Name:     name,
			Paused:   t.IsPaused(),
		}
	}
	return newResponse(req.ID, &ThreadsResult{Threads: infos})
}

// --- Inspect methods ---

func (s *Server) handleInspectFlows(req *Request) *Response {
	names := s.inspector.ListFlows()
	flows := make([]*FlowInfo, 0, len(names))
	for _, name := range names {
		if cfg, ok := s.inspector.GetFlowConfig(name); ok {
			flows = append(flows, buildFlowInfo(cfg))
		}
	}
	return newResponse(req.ID, flows)
}

func (s *Server) handleInspectFlow(req *Request) *Response {
	var params InspectFlowParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return newErrorResponse(req.ID, CodeInvalidParams, err.Error())
	}

	cfg, ok := s.inspector.GetFlowConfig(params.Name)
	if !ok {
		return newErrorResponse(req.ID, CodeFlowNotFound, "flow not found")
	}

	return newResponse(req.ID, buildFlowInfo(cfg))
}

func (s *Server) handleInspectConnectors(req *Request) *Response {
	names := s.inspector.ListConnectors()
	connectors := make([]*ConnectorInfo, 0, len(names))
	for _, name := range names {
		if cfg, ok := s.inspector.GetConnectorConfig(name); ok {
			connectors = append(connectors, buildConnectorInfo(cfg))
		}
	}
	return newResponse(req.ID, connectors)
}

func (s *Server) handleInspectTypes(req *Request) *Response {
	schemas := s.inspector.ListTypes()
	types := make([]*TypeInfo, len(schemas))
	for i, schema := range schemas {
		types[i] = buildTypeInfo(schema)
	}
	return newResponse(req.ID, types)
}

func (s *Server) handleInspectTransforms(req *Request) *Response {
	configs := s.inspector.ListTransforms()
	transforms := make([]*TransformInfo, len(configs))
	for i, cfg := range configs {
		transforms[i] = buildTransformInfo(cfg)
	}
	return newResponse(req.ID, transforms)
}
