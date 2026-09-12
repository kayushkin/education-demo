package simulation

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/kayushkin/education-demo/internal/agent"
	"github.com/kayushkin/education-demo/internal/model"
	"github.com/kayushkin/education-demo/internal/store"
)

const scriptSystemPrompt = `You write realistic transcripts of school students working in a small breakout group.

The transcript runs in ACTS. For each act you are given each student's TRUE understanding of each learning goal AT THAT POINT. Write dialogue that REVEALS that understanding through what they say, and never states it. Nobody announces "I partially understand goal 2".

THE UNDERSTANDING CHANGES BETWEEN ACTS, AND YOUR DIALOGUE MUST BE WHY.
This is the most important thing you do. If a student is "unknown" in act 1 and "partial" in act 2, then somewhere in act 1 or 2 a teammate explained it to them and they got part of it -- write that exchange. If someone is "misunderstands" in act 1 and "partial" in act 2, write the moment they were corrected and the penny half-dropped. Learning must happen ON THE PAGE, through students talking to each other. Never let a student silently arrive at a new state between acts.

How each true state sounds:
- understands: explains it correctly, often to someone else; uses the right causal language; can answer a follow-up.
- partial: gets the headline right but fumbles a mechanism, hedges, or stops one step short. Often correct-sounding but thin.
- unknown: asks questions, says they are lost, stays quiet, or changes the subject. Does NOT assert wrong facts.
- misunderstands: states something confidently and incorrectly, and keeps going. This is the important one. They may argue with a correct teammate, or teach the error to someone who does not know. They never hedge.

Rules:
- Use ONLY the exact student names given. Never invent a student.
- Short, natural, texting-register lines. Most under 25 words. Some very short ("yeah", "wait what").
- Real groups drift: a little off-topic chat is fine and makes the signal harder, which is the point.
- Spread the dialogue across ALL the goals, not just the first.
- Write 9 to 12 lines PER ACT.
- Every line carries the act number it belongs to.
- gap_seconds is the pause BEFORE the line: 2-12, larger after a question nobody wants to answer.

If a student's state is UNCHANGED across every act, they simply never get there -- do not invent progress the states do not show. A group where nobody understands a goal and one person is confidently wrong should get WORSE at it, with the wrong idea being copied down.`

// lineOut is one scripted utterance as the model returns it.
type lineOut struct {
	Speaker    string `json:"speaker"`
	Body       string `json:"body"`
	GapSeconds int    `json:"gap_seconds"`
	Act        int    `json:"act"`
}

type scriptOut struct {
	Lines []lineOut `json:"lines"`
}

var scriptSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"lines": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"speaker":     map[string]any{"type": "string", "description": "exact student name"},
					"body":        map[string]any{"type": "string"},
					"gap_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": 15},
					"act":         map[string]any{"type": "integer", "description": "which act this line is in, starting at 1"},
				},
				"required": []string{"speaker", "body", "gap_seconds", "act"},
			},
		},
	},
	"required": []string{"lines"},
}

// ScriptRequest is one team's worth of context for the writer.
type ScriptRequest struct {
	Team     model.Team
	Goals    []model.Goal
	Students []model.Student
	// PhaseTruth[i] maps student id -> goal id -> state during act i+1. The
	// model is shown all of them at once so it can write the transitions
	// between them as dialogue rather than having them appear by magic.
	PhaseTruth []map[string]map[string]model.UnderstandingState
	// Flavour is an optional extra instruction for this team, used to plant a
	// conflict or a disengaged student.
	Flavour string
}

func (r ScriptRequest) prompt() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Group: %s\n\nLEARNING GOALS:\n", r.Team.Name)
	for _, g := range r.Goals {
		fmt.Fprintf(&b, "  Goal %d: %s\n", g.Ordinal, g.Text)
	}

	for act, truth := range r.PhaseTruth {
		fmt.Fprintf(&b, "\n=== ACT %d — true understanding during this act ===\n", act+1)
		for _, st := range r.Students {
			fmt.Fprintf(&b, "  %s:\n", st.Name)
			for _, g := range r.Goals {
				state := truth[st.ID][g.ID]
				line := fmt.Sprintf("    Goal %d (%s): %s", g.Ordinal, g.ShortLabel, state)
				// Name the change explicitly rather than making the model
				// diff two tables in its head. The transitions are the point
				// of the whole transcript, so they are spelled out.
				if act > 0 {
					if was := r.PhaseTruth[act-1][st.ID][g.ID]; was != state {
						line += fmt.Sprintf("   <-- CHANGED from %s; your dialogue must show why", was)
					}
				}
				b.WriteString(line + "\n")
			}
		}
	}

	if r.Flavour != "" {
		fmt.Fprintf(&b, "\nADDITIONAL DIRECTION FOR THIS GROUP:\n%s\n", r.Flavour)
	}
	fmt.Fprintf(&b, "\nWrite all %d acts now, in order.", len(r.PhaseTruth))
	return b.String()
}

// Writer turns a seeded classroom into scripted dialogue.
type Writer struct {
	Agent *agent.Client
	// Concurrency caps how many teams are written at once. Each call is a
	// separate harness subprocess, so this is a real resource bound.
	Concurrency int
}

// WriteAll generates a transcript for every team, in parallel.
//
// A team whose call fails gets the deterministic fallback script instead of
// nothing. The error is returned alongside the lines, not swallowed: the
// caller logs it and the session still runs, which is the tradeoff a live demo
// needs. A silent fallback would let a wholly canned demo look like a working
// LLM integration, so the failures are named.
func (w *Writer) WriteAll(ctx context.Context, sessionID string, reqs []ScriptRequest) ([]store.ScriptedLine, []error) {
	conc := w.Concurrency
	if conc <= 0 {
		conc = 4
	}
	var (
		mu   sync.Mutex
		out  []store.ScriptedLine
		errs []error
		wg   sync.WaitGroup
		sem  = make(chan struct{}, conc)
	)
	for _, req := range reqs {
		wg.Add(1)
		go func(req ScriptRequest) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			lines, err := w.writeOne(ctx, sessionID, req)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", req.Team.Name, err))
				lines = fallbackScript(sessionID, req)
			}
			out = append(out, lines...)
		}(req)
	}
	wg.Wait()
	return out, errs
}

func (w *Writer) writeOne(ctx context.Context, sessionID string, req ScriptRequest) ([]store.ScriptedLine, error) {
	if w.Agent == nil {
		return nil, fmt.Errorf("no agent client configured")
	}
	var parsed scriptOut
	if err := w.Agent.CallJSON(ctx, scriptSystemPrompt, req.prompt(), scriptSchema, 8000, &parsed); err != nil {
		return nil, err
	}
	if len(parsed.Lines) == 0 {
		return nil, fmt.Errorf("model returned no lines")
	}

	// Resolve speakers by name against THIS TEAM's roster only. A name the
	// model invented is dropped rather than guessed at: attributing a line to
	// the wrong student would corrupt both the transcript and every
	// assessment drawn from it.
	byName := map[string]model.Student{}
	for _, st := range req.Students {
		byName[strings.ToLower(strings.TrimSpace(st.Name))] = st
	}
	// Lines are re-ordered by act before numbering. The model usually emits
	// them in order, but a single act label out of place would otherwise play
	// the lesson out of sequence and advance the session's phase backwards.
	sort.SliceStable(parsed.Lines, func(i, j int) bool {
		return parsed.Lines[i].Act < parsed.Lines[j].Act
	})

	out := []store.ScriptedLine{}
	ord := 0
	for _, l := range parsed.Lines {
		st, ok := byName[strings.ToLower(strings.TrimSpace(l.Speaker))]
		if !ok {
			continue
		}
		body := strings.TrimSpace(l.Body)
		if body == "" {
			continue
		}
		gap := l.GapSeconds
		if gap < 1 {
			gap = 3
		}
		if gap > 15 {
			gap = 15
		}
		act := l.Act
		if act < 1 {
			act = 1
		}
		if act > len(req.PhaseTruth) {
			act = len(req.PhaseTruth)
		}
		ord++
		out = append(out, store.ScriptedLine{
			ID: uuid.NewString(), SessionID: sessionID, TeamID: req.Team.ID,
			StudentID: st.ID, Ordinal: ord, Body: body, GapMs: gap * 1000, Phase: act,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("model returned %d lines but none named a student on this team", len(parsed.Lines))
	}
	return out, nil
}

// fallbackTemplates are the canned utterances used when the model cannot be
// reached. They are keyed by true state so the fallback transcript still
// carries real signal and the monitoring agent still has something to be right
// or wrong about.
var fallbackTemplates = map[model.UnderstandingState][]string{
	model.StateUnderstands: {
		"ok so for goal %d, the key thing is it follows from the definition — that's why it works.",
		"I think I've got goal %d. want me to walk through it?",
		"right, and that's exactly why goal %d holds even when you change the numbers.",
	},
	model.StatePartial: {
		"goal %d is the one about the main idea right? I get the first half at least.",
		"I can sort of do goal %d but I keep losing it at the last step.",
		"wait is goal %d the same as what we did yesterday? kind of?",
	},
	model.StateUnknown: {
		"honestly no idea on goal %d",
		"can someone explain goal %d again, I'm lost",
		"I don't even know where to start with goal %d",
	},
	model.StateMisunderstands: {
		"goal %d is easy — it's just the opposite of what the book says, trust me.",
		"no no, for goal %d you always flip it. that's the rule.",
		"goal %d literally just means the bigger one always wins. I'm sure.",
	},
}

// fallbackScript builds a deterministic transcript from ground truth with no
// model call at all, so a session still runs and still demonstrates the
// monitoring path when llm-bridge is unreachable.
func fallbackScript(sessionID string, req ScriptRequest) []store.ScriptedLine {
	rng := rand.New(rand.NewSource(int64(len(req.Team.ID)) + int64(req.Team.Ordinal)))
	out := []store.ScriptedLine{}
	ord := 0
	// One pass per act, reading that act's truth, so even the canned transcript
	// carries the session's learning arc instead of repeating its opening.
	for act, truth := range req.PhaseTruth {
		for _, st := range req.Students {
			g := req.Goals[rng.Intn(len(req.Goals))]
			tpl := fallbackTemplates[truth[st.ID][g.ID]]
			if len(tpl) == 0 {
				continue
			}
			ord++
			out = append(out, store.ScriptedLine{
				ID: uuid.NewString(), SessionID: sessionID, TeamID: req.Team.ID,
				StudentID: st.ID, Ordinal: ord,
				Body:  fmt.Sprintf(tpl[rng.Intn(len(tpl))], g.Ordinal),
				GapMs: (3 + rng.Intn(6)) * 1000, Phase: act + 1,
			})
		}
	}
	return out
}
