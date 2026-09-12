package server

import (
	"encoding/json"
	"sync"
)

// Event is one thing that happened, pushed to every dashboard watching a
// session.
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// Event types. The client switches on these, so they are a contract.
const (
	EventMessage     = "message"
	EventAlert       = "alert"
	EventAssessments = "assessments"
	EventSession     = "session"
	EventMonitor     = "monitor"
)

// hub fans events out to the SSE subscribers of one session.
//
// Each subscriber gets a buffered channel and a slow reader is DROPPED rather
// than allowed to block the publisher: a stalled browser tab must never stop
// the simulation or the monitor. The dropped client's EventSource reconnects
// on its own and refetches state.
type hub struct {
	mu   sync.RWMutex
	subs map[string]map[chan []byte]struct{}
}

func newHub() *hub {
	return &hub{subs: map[string]map[chan []byte]struct{}{}}
}

func (h *hub) subscribe(sessionID string) chan []byte {
	ch := make(chan []byte, 64)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs[sessionID] == nil {
		h.subs[sessionID] = map[chan []byte]struct{}{}
	}
	h.subs[sessionID][ch] = struct{}{}
	return ch
}

func (h *hub) unsubscribe(sessionID string, ch chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set, ok := h.subs[sessionID]; ok {
		if _, present := set[ch]; present {
			delete(set, ch)
			close(ch)
		}
		if len(set) == 0 {
			delete(h.subs, sessionID)
		}
	}
}

func (h *hub) publish(sessionID string, ev Event) {
	payload, err := json.Marshal(ev)
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subs[sessionID] {
		select {
		case ch <- payload:
		default:
			// Subscriber is not keeping up. Skip it.
		}
	}
}

func (h *hub) subscriberCount(sessionID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs[sessionID])
}
