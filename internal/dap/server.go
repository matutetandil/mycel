package dap

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/matutetandil/mycel/internal/trace"
)

var errDisconnected = fmt.Errorf("disconnected")

// Server implements the Debug Adapter Protocol over TCP.
// It handles one debug session at a time (single flow execution).
type Server struct {
	port     int
	logger   *slog.Logger
	session  *Session
	listener net.Listener

	// seq is the next sequence number for server-sent messages.
	seq atomic.Int64

	// conn is the current client connection.
	conn   net.Conn
	connMu sync.Mutex

	// onLaunch is called when the IDE sends a "launch" request.
	// The server passes the launch arguments and expects the caller
	// to start the flow execution asynchronously.
	onLaunch func(args LaunchArguments) error
}

// NewServer creates a new DAP server on the given port.
func NewServer(port int, logger *slog.Logger) *Server {
	return &Server{
		port:    port,
		logger:  logger,
		session: NewSession(),
	}
}

// Session returns the debug session (used to create the DAPBreakpoint).
func (s *Server) Session() *Session {
	return s.session
}

// OnLaunch sets the callback for when the IDE sends a launch request.
func (s *Server) OnLaunch(fn func(args LaunchArguments) error) {
	s.onLaunch = fn
}

// NotifyFlowDone is called when the flow execution completes.
func (s *Server) NotifyFlowDone(result interface{}, err error) {
	s.session.mu.Lock()
	s.session.result = result
	s.session.err = err
	s.session.finished = true
	s.session.mu.Unlock()

	close(s.session.doneCh)

	// Send terminated event
	s.sendEvent("terminated", &TerminatedEventBody{})
}

// ListenAndServe starts the DAP server and blocks until a session completes.
func (s *Server) ListenAndServe() error {
	var err error
	s.listener, err = net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %w", s.port, err)
	}
	defer s.listener.Close()

	s.logger.Info("DAP server listening", "port", s.port)
	fmt.Printf("DAP server listening on port %d — connect your IDE debugger\n", s.port)

	// Accept one connection
	conn, err := s.listener.Accept()
	if err != nil {
		return fmt.Errorf("failed to accept connection: %w", err)
	}
	s.connMu.Lock()
	s.conn = conn
	s.connMu.Unlock()
	defer conn.Close()

	s.logger.Info("IDE connected", "remote", conn.RemoteAddr())

	// Start listening for pause events from breakpoints
	go s.watchBreakpoints()

	// Process messages
	return s.processMessages(conn)
}

// Close shuts down the server.
func (s *Server) Close() error {
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

// watchBreakpoints listens for breakpoint hits and sends stopped events to the IDE.
func (s *Server) watchBreakpoints() {
	for {
		select {
		case info := <-s.session.pauseCh:
			line := StageLines[string(info.stage)]
			text := string(info.stage)
			if info.name != "" {
				text += " (" + info.name + ")"
			}
			s.sendEvent("stopped", &StoppedEventBody{
				Reason:   "breakpoint",
				ThreadID: 1,
				Text:     text,
			})
			_ = line
		case <-s.session.doneCh:
			return
		}
	}
}

// processMessages reads and handles DAP messages from the connection.
func (s *Server) processMessages(conn net.Conn) error {
	reader := bufio.NewReader(conn)

	for {
		msg, err := readMessage(reader)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("read error: %w", err)
		}

		var req Request
		if err := json.Unmarshal(msg, &req); err != nil {
			s.logger.Warn("invalid DAP message", "error", err)
			continue
		}

		if req.Type != "request" {
			continue
		}

		if err := s.handleRequest(&req); err != nil {
			if err == errDisconnected {
				return nil
			}
			s.logger.Warn("handler error", "command", req.Command, "error", err)
		}
	}
}

// handleRequest dispatches a DAP request to the appropriate handler.
func (s *Server) handleRequest(req *Request) error {
	switch req.Command {
	case "initialize":
		s.sendResponse(req, true, &Capabilities{
			SupportsConfigurationDoneRequest: true,
		})
		// Send initialized event
		s.sendEvent("initialized", nil)

	case "configurationDone":
		s.sendResponse(req, true, nil)

	case "launch":
		var args LaunchArguments
		if req.Arguments != nil {
			if err := json.Unmarshal(req.Arguments, &args); err != nil {
				s.sendErrorResponse(req, "invalid launch arguments: "+err.Error())
				return nil
			}
		}
		s.session.mu.Lock()
		s.session.flowName = args.Flow
		s.session.input = args.Input
		s.session.dryRun = args.DryRun
		s.session.launched = true
		s.session.mu.Unlock()

		s.sendResponse(req, true, nil)

		// Start flow execution asynchronously
		if s.onLaunch != nil {
			go func() {
				if err := s.onLaunch(args); err != nil {
					s.sendEvent("output", &OutputEventBody{
						Category: "stderr",
						Output:   "Flow execution error: " + err.Error() + "\n",
					})
				}
			}()
		}

	case "setBreakpoints":
		var args SetBreakpointsArguments
		if req.Arguments != nil {
			json.Unmarshal(req.Arguments, &args)
		}
		s.handleSetBreakpoints(req, &args)

	case "threads":
		s.sendResponse(req, true, &ThreadsResponseBody{
			Threads: []Thread{{ID: 1, Name: s.session.flowName}},
		})

	case "stackTrace":
		s.handleStackTrace(req)

	case "scopes":
		var args ScopesArguments
		if req.Arguments != nil {
			json.Unmarshal(req.Arguments, &args)
		}
		s.handleScopes(req, &args)

	case "variables":
		var args VariablesArguments
		if req.Arguments != nil {
			json.Unmarshal(req.Arguments, &args)
		}
		s.handleVariables(req, &args)

	case "continue":
		s.session.resumeCh <- actionContinue
		s.sendResponse(req, true, map[string]bool{"allThreadsContinued": true})

	case "next":
		s.session.resumeCh <- actionNext
		s.sendResponse(req, true, nil)

	case "disconnect":
		// Abort any paused execution
		select {
		case s.session.resumeCh <- actionAbort:
		default:
		}
		s.sendResponse(req, true, nil)
		// Close the connection to unblock any pending reads
		s.connMu.Lock()
		if s.conn != nil {
			s.conn.Close()
		}
		s.connMu.Unlock()
		return errDisconnected

	default:
		s.sendErrorResponse(req, fmt.Sprintf("unsupported command: %s", req.Command))
	}

	return nil
}

// handleSetBreakpoints processes breakpoint requests.
// Lines map to pipeline stages (see StageLines).
func (s *Server) handleSetBreakpoints(req *Request, args *SetBreakpointsArguments) {
	var stages []trace.Stage
	var bps []ResponseBreakpoint

	for i, bp := range args.Breakpoints {
		stageName, ok := LineToStage[bp.Line]
		if ok {
			stages = append(stages, trace.Stage(stageName))
			bps = append(bps, ResponseBreakpoint{
				ID:       i + 1,
				Verified: true,
				Line:     bp.Line,
			})
		} else {
			bps = append(bps, ResponseBreakpoint{
				ID:       i + 1,
				Verified: false,
				Message:  fmt.Sprintf("no pipeline stage at line %d", bp.Line),
			})
		}
	}

	s.session.SetBreakpoints(stages)
	s.sendResponse(req, true, &SetBreakpointsResponseBody{Breakpoints: bps})
}

// handleStackTrace returns the pipeline stages as a call stack.
func (s *Server) handleStackTrace(req *Request) {
	history := s.session.StageHistory()
	stage, name, _ := s.session.CurrentState()

	frames := make([]StackFrame, 0, len(history)+1)

	// Current frame is the top of the stack
	if stage != "" {
		frameName := strings.ToUpper(string(stage))
		if name != "" {
			frameName += " (" + name + ")"
		}
		frames = append(frames, StackFrame{
			ID:   len(history),
			Name: frameName,
			Source: Source{
				Name: s.session.flowName,
			},
			Line:   StageLines[string(stage)],
			Column: 1,
		})
	}

	// Previous stages as parent frames
	for i := len(history) - 1; i >= 0; i-- {
		entry := history[i]
		if entry.stage == stage {
			continue // skip current (already added)
		}
		frameName := strings.ToUpper(string(entry.stage))
		if entry.name != "" {
			frameName += " (" + entry.name + ")"
		}
		frames = append(frames, StackFrame{
			ID:   i,
			Name: frameName,
			Source: Source{
				Name: s.session.flowName,
			},
			Line:   StageLines[string(entry.stage)],
			Column: 1,
		})
	}

	s.sendResponse(req, true, &StackTraceResponseBody{
		StackFrames: frames,
		TotalFrames: len(frames),
	})
}

// handleScopes returns the available variable scopes for a stack frame.
func (s *Server) handleScopes(req *Request, args *ScopesArguments) {
	// Scope reference encoding: frameID * 100 + scopeType
	// scopeType: 1 = data
	ref := args.FrameID*100 + 1

	s.sendResponse(req, true, &ScopesResponseBody{
		Scopes: []Scope{
			{Name: "Data", VariablesReference: ref, Expensive: false},
		},
	})
}

// handleVariables returns variables for a given scope.
func (s *Server) handleVariables(req *Request, args *VariablesArguments) {
	frameID := args.VariablesReference / 100

	// Find data for this frame
	var data interface{}
	history := s.session.StageHistory()
	if frameID >= 0 && frameID < len(history) {
		data = history[frameID].data
	} else {
		// Current data
		_, _, data = s.session.CurrentState()
	}

	vars := VariablesForData(data)
	s.sendResponse(req, true, &VariablesResponseBody{Variables: vars})
}

// sendResponse sends a DAP response to the client.
func (s *Server) sendResponse(req *Request, success bool, body interface{}) {
	resp := Response{
		Message: Message{
			Seq:  int(s.seq.Add(1)),
			Type: "response",
		},
		RequestSeq: req.Seq,
		Success:    success,
		Command:    req.Command,
		Body:       body,
	}
	s.writeMessage(resp)
}

// sendErrorResponse sends an error response.
func (s *Server) sendErrorResponse(req *Request, msg string) {
	resp := Response{
		Message: Message{
			Seq:  int(s.seq.Add(1)),
			Type: "response",
		},
		RequestSeq: req.Seq,
		Success:    false,
		Command:    req.Command,
		ErrorMsg:   msg,
	}
	s.writeMessage(resp)
}

// sendEvent sends a DAP event to the client.
func (s *Server) sendEvent(event string, body interface{}) {
	evt := Event{
		Message: Message{
			Seq:  int(s.seq.Add(1)),
			Type: "event",
		},
		Event: event,
		Body:  body,
	}
	s.writeMessage(evt)
}

// writeMessage serializes and sends a DAP message with Content-Length framing.
func (s *Server) writeMessage(msg interface{}) {
	data, err := json.Marshal(msg)
	if err != nil {
		s.logger.Warn("failed to marshal DAP message", "error", err)
		return
	}

	s.connMu.Lock()
	defer s.connMu.Unlock()

	if s.conn == nil {
		return
	}

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	s.conn.Write([]byte(header))
	s.conn.Write(data)
}

// readMessage reads a single DAP message (Content-Length framed).
func readMessage(reader *bufio.Reader) ([]byte, error) {
	// Read headers
	contentLength := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break // End of headers
		}
		if strings.HasPrefix(line, "Content-Length:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
			contentLength, _ = strconv.Atoi(val)
		}
	}

	if contentLength == 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}

	// Read body
	body := make([]byte, contentLength)
	_, err := io.ReadFull(reader, body)
	if err != nil {
		return nil, err
	}

	return body, nil
}
