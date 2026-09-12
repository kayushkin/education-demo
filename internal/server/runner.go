package server

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/kayushkin/education-demo/internal/model"
	"github.com/kayushkin/education-demo/internal/monitor"
	"github.com/kayushkin/education-demo/internal/simulation"
	"github.com/kayushkin/education-demo/internal/store"
)

// runner owns one live session: the transcript playback and the monitoring
// loop, and the pause flag both consult.
type runner struct {
	sessionID string
	player    *simulation.Player
	mon       *monitor.Monitor
	hub       *hub
	st        *store.Store
	cancel    context.CancelFunc
	wg        sync.WaitGroup

	mu        sync.RWMutex
	paused    bool
	lastRound time.Time
	roundNum  int
	lastErr   string
}

func (r *runner) isPaused() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.paused
}

func (r *runner) setPaused(v bool) {
	r.mu.Lock()
	r.paused = v
	r.mu.Unlock()
}

// status is what the dashboard shows about the engine itself.
type runnerStatus struct {
	Running     bool      `json:"running"`
	Paused      bool      `json:"paused"`
	Rounds      int       `json:"rounds"`
	LastRoundAt time.Time `json:"last_round_at,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
	ScriptLeft  int       `json:"script_left"`
}

func (r *runner) status() runnerStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	left, err := r.st.CountUnplayedLines(r.sessionID)
	if err != nil {
		log.Printf("runner %s: count unplayed lines: %v", r.sessionID, err)
	}
	return runnerStatus{
		Running: true, Paused: r.paused, Rounds: r.roundNum,
		LastRoundAt: r.lastRound, LastError: r.lastErr, ScriptLeft: left,
	}
}

// start launches playback and the monitoring loop.
func (r *runner) start(ctx context.Context, teams []model.Team, interval time.Duration) {
	ctx, cancel := context.WithCancel(ctx)
	r.cancel = cancel

	r.player.Paused = r.isPaused
	r.player.OnMessage = func(m model.Message) {
		r.hub.publish(r.sessionID, Event{Type: EventMessage, Data: m})
	}
	r.mon.OnRound = func(teamID string, alerts []model.Alert) {
		for _, a := range alerts {
			r.hub.publish(r.sessionID, Event{Type: EventAlert, Data: a})
		}
	}
	r.player.Start(ctx, teams)

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.monitorLoop(ctx, interval)
	}()
}

// monitorLoop runs an assessment round on a fixed cadence.
//
// The interval is wall-clock between round STARTS, but a round that overruns
// is not stacked on the next tick: each round is awaited before the timer is
// reset. Ten teams times an eight-second call is a real duration and letting
// rounds overlap would spend the subscription twice for the same transcript.
func (r *runner) monitorLoop(ctx context.Context, interval time.Duration) {
	// Give the rooms something to talk about before the first call; assessing
	// an empty transcript wastes a round and reports nothing.
	if !sleep(ctx, 12*time.Second) {
		return
	}
	for {
		if ctx.Err() != nil {
			return
		}
		if !r.isPaused() {
			started := time.Now()
			checked, err := r.mon.RunRound(ctx, r.sessionID)
			r.mu.Lock()
			r.lastRound = time.Now().UTC()
			r.roundNum++
			r.lastErr = ""
			if err != nil {
				r.lastErr = err.Error()
			}
			round, lastErr := r.roundNum, r.lastErr
			r.mu.Unlock()

			r.hub.publish(r.sessionID, Event{Type: EventMonitor, Data: map[string]any{
				"round":         round,
				"teams_checked": checked,
				"duration_ms":   time.Since(started).Milliseconds(),
				"error":         lastErr,
			}})
			// Assessments changed, so tell the dashboard to refetch the grid
			// rather than pushing the whole matrix down the wire each round.
			if checked > 0 {
				r.hub.publish(r.sessionID, Event{Type: EventAssessments, Data: map[string]any{"round": round}})
			}
		}
		if !sleep(ctx, interval) {
			return
		}
	}
}

func (r *runner) stop() {
	if r.cancel != nil {
		r.cancel()
	}
	r.player.Stop()
	r.wg.Wait()
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
