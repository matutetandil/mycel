package debug

import (
	"sync"
	"time"

	"github.com/matutetandil/mycel/internal/trace"
)

// EventStream fans out pipeline events to all connected debug clients.
// Thread-safe for concurrent producers (flow handlers) and consumers (WebSocket sessions).
type EventStream struct {
	mu          sync.RWMutex
	subscribers map[string]chan *Notification // sessionID → channel
}

// NewEventStream creates a new event stream.
func NewEventStream() *EventStream {
	return &EventStream{
		subscribers: make(map[string]chan *Notification),
	}
}

// Subscribe registers a session to receive events.
// Returns a channel that receives notifications. Buffer size prevents slow
// clients from blocking producers.
func (es *EventStream) Subscribe(sessionID string) <-chan *Notification {
	es.mu.Lock()
	defer es.mu.Unlock()
	ch := make(chan *Notification, 256)
	es.subscribers[sessionID] = ch
	return ch
}

// Unsubscribe removes a session from the event stream.
func (es *EventStream) Unsubscribe(sessionID string) {
	es.mu.Lock()
	defer es.mu.Unlock()
	if ch, ok := es.subscribers[sessionID]; ok {
		close(ch)
		delete(es.subscribers, sessionID)
	}
}

// Broadcast sends a notification to all connected clients.
// Non-blocking: drops events for slow clients to prevent backpressure.
func (es *EventStream) Broadcast(n *Notification) {
	es.mu.RLock()
	defer es.mu.RUnlock()
	for _, ch := range es.subscribers {
		select {
		case ch <- n:
		default:
			// Drop event for slow client
		}
	}
}

// HasSubscribers returns true if any clients are connected.
func (es *EventStream) HasSubscribers() bool {
	es.mu.RLock()
	defer es.mu.RUnlock()
	return len(es.subscribers) > 0
}

// StudioCollector implements trace.Collector and broadcasts events to connected clients.
type StudioCollector struct {
	stream   *EventStream
	threadID string
	flowName string

	mu     sync.Mutex
	events []trace.Event
}

// NewStudioCollector creates a collector that broadcasts trace events via the event stream.
func NewStudioCollector(stream *EventStream, threadID, flowName string) *StudioCollector {
	return &StudioCollector{
		stream:   stream,
		threadID: threadID,
		flowName: flowName,
		events:   make([]trace.Event, 0, 16),
	}
}

// Record stores the event and broadcasts it to connected clients.
func (c *StudioCollector) Record(event trace.Event) {
	c.mu.Lock()
	c.events = append(c.events, event)
	c.mu.Unlock()

	// Broadcast stage enter/exit events
	if event.Duration > 0 || event.Error != nil || event.Output != nil {
		// Stage exit (has duration or output)
		errStr := ""
		if event.Error != nil {
			errStr = event.Error.Error()
		}
		c.stream.Broadcast(newNotification("event.stageExit", &StageExitEvent{
			ThreadID: c.threadID,
			FlowName: c.flowName,
			Stage:    event.Stage,
			Name:     event.Name,
			Output:   event.Output,
			Duration: event.Duration.Microseconds(),
			Error:    errStr,
		}))
	} else if event.Input != nil {
		// Stage enter (has input, no output yet)
		c.stream.Broadcast(newNotification("event.stageEnter", &StageEnterEvent{
			ThreadID: c.threadID,
			FlowName: c.flowName,
			Stage:    event.Stage,
			Name:     event.Name,
			Input:    event.Input,
		}))
	}
}

// Events returns all collected events.
func (c *StudioCollector) Events() []trace.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]trace.Event, len(c.events))
	copy(result, c.events)
	return result
}

// BroadcastFlowStart sends a flow start event.
func (c *StudioCollector) BroadcastFlowStart(input interface{}) {
	c.stream.Broadcast(newNotification("event.flowStart", &FlowStartEvent{
		ThreadID: c.threadID,
		FlowName: c.flowName,
		Input:    input,
	}))
}

// BroadcastFlowEnd sends a flow end event.
func (c *StudioCollector) BroadcastFlowEnd(output interface{}, duration time.Duration, err error) {
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	c.stream.Broadcast(newNotification("event.flowEnd", &FlowEndEvent{
		ThreadID: c.threadID,
		FlowName: c.flowName,
		Output:   output,
		Duration: duration.Microseconds(),
		Error:    errStr,
	}))
}

// BroadcastRuleEval sends a rule evaluation event.
func (c *StudioCollector) BroadcastRuleEval(stage trace.Stage, index int, target, expression string, result interface{}, err error) {
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	c.stream.Broadcast(newNotification("event.ruleEval", &RuleEvalEvent{
		ThreadID:   c.threadID,
		FlowName:   c.flowName,
		Stage:      stage,
		RuleIndex:  index,
		Target:     target,
		Expression: expression,
		Result:     result,
		Error:      errStr,
	}))
}
