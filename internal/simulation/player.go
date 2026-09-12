package simulation

import (
	"context"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kayushkin/education-demo/internal/model"
	"github.com/kayushkin/education-demo/internal/store"
)

// Player drips each team's scripted transcript into its room in real time.
//
// One goroutine per team, so teams talk concurrently the way real breakout
// rooms do. A human typing in a room is simply another writer to the same
// message log; the player neither knows nor cares.
type Player struct {
	Store     *store.Store
	SessionID string
	// Speed multiplies the scripted gaps. 1.0 is realistic pacing; a demo
	// running short can raise it to compress a 20-minute session.
	Speed float64
	// OnMessage is called after each line lands, so the SSE hub can push it.
	OnMessage func(model.Message)
	// Paused is consulted before every line, so the teacher can hold the room.
	Paused func() bool

	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.Mutex
	done   bool
}

// Start launches one player goroutine per team. It returns immediately.
func (p *Player) Start(ctx context.Context, teams []model.Team) {
	ctx, cancel := context.WithCancel(ctx)
	p.mu.Lock()
	p.cancel = cancel
	p.mu.Unlock()

	for _, t := range teams {
		p.wg.Add(1)
		go func(team model.Team) {
			defer p.wg.Done()
			p.runTeam(ctx, team)
		}(t)
	}
}

// Stop cancels playback and waits for the goroutines to finish.
func (p *Player) Stop() {
	p.mu.Lock()
	if p.cancel != nil {
		p.cancel()
	}
	p.mu.Unlock()
	p.wg.Wait()
}

// Done reports whether every team has run out of script.
func (p *Player) Done() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.done
}

func (p *Player) runTeam(ctx context.Context, team model.Team) {
	speed := p.Speed
	if speed <= 0 {
		speed = 1
	}
	// Stagger team openings so ten rooms do not all speak on the same
	// millisecond, which looks synthetic on the dashboard.
	rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(team.Ordinal)))
	if !sleepCtx(ctx, time.Duration(rng.Intn(4000))*time.Millisecond) {
		return
	}

	for {
		if ctx.Err() != nil {
			return
		}
		if p.Paused != nil && p.Paused() {
			if !sleepCtx(ctx, time.Second) {
				return
			}
			continue
		}

		line, err := p.Store.NextScriptedLine(team.ID)
		if err != nil {
			log.Printf("player %s: read next line: %v", team.Name, err)
			if !sleepCtx(ctx, 2*time.Second) {
				return
			}
			continue
		}
		if line == nil {
			// Script spent. The room stays open: a human in it can still talk
			// and still be monitored.
			p.markDone()
			return
		}

		gap := time.Duration(float64(line.GapMs)/speed) * time.Millisecond
		if !sleepCtx(ctx, gap) {
			return
		}
		if p.Paused != nil && p.Paused() {
			continue
		}

		msg := &model.Message{
			ID: uuid.NewString(), SessionID: line.SessionID, TeamID: line.TeamID,
			StudentID: line.StudentID, Body: line.Body, At: time.Now().UTC(),
		}
		if err := p.Store.AppendMessage(msg); err != nil {
			log.Printf("player %s: append message: %v", team.Name, err)
			continue
		}
		if err := p.Store.MarkLinePlayed(line.ID); err != nil {
			// Loud, because the consequence is a line replayed forever.
			log.Printf("player %s: mark line %s played: %v", team.Name, line.ID, err)
		}
		// The transcript decides when the lesson has moved on, not a timer.
		// Playing a line from a later act advances the session into it, which
		// is what the accuracy panel then scores the agent against. The store
		// never moves a session backwards, so teams reaching act 2 at
		// different moments is fine.
		if err := p.Store.AdvanceSessionPhase(line.SessionID, line.Phase); err != nil {
			log.Printf("player %s: advance to phase %d: %v", team.Name, line.Phase, err)
		}
		if p.OnMessage != nil {
			p.OnMessage(*msg)
		}
	}
}

func (p *Player) markDone() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.done = true
}

// sleepCtx waits for d, reporting false if the context ended first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
