// Package monitor is the agent that watches every breakout room.
//
// One LLM call per team per round, over that team's recent transcript. It
// produces three things: an understanding assessment per student per goal,
// alerts for the teacher, and misconceptions worth collecting across the
// class. It never sees ground truth — recovering it from dialogue alone is
// the job being measured.
package monitor

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kayushkin/education-demo/internal/agent"
	"github.com/kayushkin/education-demo/internal/model"
	"github.com/kayushkin/education-demo/internal/store"
)

const systemPrompt = `You observe one small group of students working on a set of learning goals, and you report to their teacher during the lesson.

You are given the group's recent transcript, the learning goals, and the roster. Judge ONLY from what was said.

For each student and each goal, classify their understanding:
- understands: they said something that shows correct grasp. Explaining it correctly to someone else is the strongest evidence.
- partial: broadly right but incomplete, hedged, or missing the mechanism.
- unknown: no evidence either way, or they said they do not know. Silence is unknown, NOT misunderstanding.
- misunderstands: they asserted something incorrect. This requires a WRONG claim, not merely a confused question.

Only include a (student, goal) pair when the transcript gives you evidence. Do not fill in the grid.

Raise an alert when, and only when, one of these is true:
- confidently_wrong: a student states something incorrect about a goal TO their teammates, especially if nobody corrects it. Severity critical when others appear to accept it.
- goal_unmastered: no member of this group shows understanding of a goal AND they have engaged with it. Not for a goal nobody has reached yet.
- conflict: interpersonal friction that is impeding the work. Disagreement about the material is NOT conflict; it is often healthy.
- disengaged: a named student has contributed nothing meaningful while others work.

Also collect misconceptions: specific wrong ideas stated, phrased as the wrong idea itself ("heavier objects fall faster"), not as a description of a student.

Be conservative. A teacher who gets a false alarm every round stops reading alerts. Quote the exact words you are relying on.`

// assessmentOut mirrors one row of the model's structured reply. Names are the
// wire contract with the prompt, which is why they are student and goal NAMES
// here — the model cannot be trusted with opaque uuids, so it works in names
// and this package resolves them back to ids against the roster it sent.
type assessmentOut struct {
	Student    string  `json:"student"`
	Goal       int     `json:"goal_number"`
	State      string  `json:"state"`
	Confidence float64 `json:"confidence"`
	Evidence   string  `json:"evidence"`
}

type alertOut struct {
	Kind     string `json:"kind"`
	Severity string `json:"severity"`
	Student  string `json:"student"`
	Goal     int    `json:"goal_number"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Quote    string `json:"quote"`
}

type misconceptionOut struct {
	Goal int    `json:"goal_number"`
	Text string `json:"text"`
}

type roundOut struct {
	Assessments    []assessmentOut    `json:"assessments"`
	Alerts         []alertOut         `json:"alerts"`
	Misconceptions []misconceptionOut `json:"misconceptions"`
}

func schema(states, kinds, severities []string) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"assessments": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"student":     map[string]any{"type": "string"},
						"goal_number": map[string]any{"type": "integer"},
						"state":       map[string]any{"type": "string", "enum": states},
						"confidence":  map[string]any{"type": "number", "minimum": 0, "maximum": 1},
						"evidence":    map[string]any{"type": "string", "description": "the words you judged from"},
					},
					"required": []string{"student", "goal_number", "state", "confidence", "evidence"},
				},
			},
			"alerts": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"kind":        map[string]any{"type": "string", "enum": kinds},
						"severity":    map[string]any{"type": "string", "enum": severities},
						"student":     map[string]any{"type": "string", "description": "empty if not about one student"},
						"goal_number": map[string]any{"type": "integer", "description": "0 if not about one goal"},
						"title":       map[string]any{"type": "string", "description": "one short line for the teacher"},
						"detail":      map[string]any{"type": "string"},
						"quote":       map[string]any{"type": "string"},
					},
					"required": []string{"kind", "severity", "title", "detail"},
				},
			},
			"misconceptions": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"goal_number": map[string]any{"type": "integer"},
						"text":        map[string]any{"type": "string", "description": "the wrong idea itself"},
					},
					"required": []string{"goal_number", "text"},
				},
			},
		},
		"required": []string{"assessments", "alerts", "misconceptions"},
	}
}

// Monitor runs assessment rounds over a session's teams.
type Monitor struct {
	Store   *store.Store
	Agent   *agent.Client
	Logf    func(string, ...any)
	OnRound func(teamID string, alerts []model.Alert)

	// MinNewMessages is how many unseen messages a room must have before a
	// round is worth its call. Below this the transcript has not moved enough
	// to change a judgement.
	MinNewMessages int
	// WindowSize is how many recent messages each call reads.
	WindowSize int
	// Concurrency caps simultaneous calls across teams.
	Concurrency int
}

func (m *Monitor) logf(format string, args ...any) {
	if m.Logf != nil {
		m.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// RunRound assesses every team that has enough new dialogue to justify a call.
// It returns the number of teams actually assessed.
func (m *Monitor) RunRound(ctx context.Context, sessionID string) (int, error) {
	goals, err := m.Store.ListGoals(sessionID)
	if err != nil {
		return 0, fmt.Errorf("list goals: %w", err)
	}
	teams, err := m.Store.ListTeams(sessionID)
	if err != nil {
		return 0, fmt.Errorf("list teams: %w", err)
	}

	minNew := m.MinNewMessages
	if minNew <= 0 {
		minNew = 4
	}
	conc := m.Concurrency
	if conc <= 0 {
		conc = 4
	}

	var (
		wg      sync.WaitGroup
		sem     = make(chan struct{}, conc)
		mu      sync.Mutex
		checked int
	)
	for _, t := range teams {
		wg.Add(1)
		go func(team model.Team) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			ran, err := m.assessTeam(ctx, sessionID, team, goals, minNew)
			if err != nil {
				m.logf("monitor %s: %v", team.Name, err)
				if setErr := m.Store.SetMonitorCursor(team.ID, -1, err.Error()); setErr != nil {
					m.logf("monitor %s: record error: %v", team.Name, setErr)
				}
			}
			if ran {
				mu.Lock()
				checked++
				mu.Unlock()
			}
		}(t)
	}
	wg.Wait()
	return checked, nil
}

// assessTeam runs one call for one team, if it has moved enough to need one.
func (m *Monitor) assessTeam(ctx context.Context, sessionID string, team model.Team,
	goals []model.Goal, minNew int) (bool, error) {

	cursor, err := m.Store.MonitorCursor(team.ID)
	if err != nil {
		return false, fmt.Errorf("read cursor: %w", err)
	}
	if cursor < 0 {
		cursor = 0
	}
	fresh, err := m.Store.ListTeamMessagesSince(team.ID, cursor, 500)
	if err != nil {
		return false, fmt.Errorf("read new messages: %w", err)
	}
	if len(fresh) < minNew {
		return false, nil
	}

	window := m.WindowSize
	if window <= 0 {
		window = 45
	}
	transcript, err := m.Store.ListTeamMessagesTail(team.ID, window)
	if err != nil {
		return false, fmt.Errorf("read transcript: %w", err)
	}
	students, err := m.Store.ListTeamStudents(team.ID)
	if err != nil {
		return false, fmt.Errorf("read roster: %w", err)
	}
	if len(students) == 0 {
		return false, nil
	}

	highest := fresh[len(fresh)-1].Seq

	if m.Agent == nil {
		// No model configured: do what can be computed exactly, and refuse to
		// invent the rest. Understanding is not derivable from message counts,
		// so no assessment is written — a fabricated grid would be worse than
		// an empty one.
		alerts := participationAlerts(sessionID, team, students, transcript)
		m.writeAlerts(sessionID, team, alerts)
		return true, m.Store.SetMonitorCursor(team.ID, highest, "no agent configured: participation only")
	}

	// Feed back the wrong ideas already collected for this session so the model
	// reuses an existing phrasing instead of inventing a near-duplicate. Without
	// this, one impetus misconception arrives as six differently-worded rows and
	// the teacher's list is unreadable.
	known, err := m.Store.ListMisconceptions(sessionID)
	if err != nil {
		return false, fmt.Errorf("read known misconceptions: %w", err)
	}

	var out roundOut
	err = m.Agent.CallJSON(ctx, systemPrompt,
		buildPrompt(team, goals, students, transcript, known), schema(
			stringsOf(model.UnderstandingStates), stringsOf(model.AlertKinds), stringsOf(model.Severities),
		), 8000, &out)
	if err != nil {
		return false, fmt.Errorf("assess: %w", err)
	}

	m.applyRound(sessionID, team, goals, students, out)
	if err := m.Store.SetMonitorCursor(team.ID, highest, ""); err != nil {
		return true, fmt.Errorf("advance cursor: %w", err)
	}
	return true, nil
}

func buildPrompt(team model.Team, goals []model.Goal, students []model.Student,
	msgs []model.Message, known []model.Misconception) string {
	nameByID := map[string]string{}
	for _, s := range students {
		nameByID[s.ID] = s.Name
	}
	var b strings.Builder
	fmt.Fprintf(&b, "GROUP: %s\n\nLEARNING GOALS:\n", team.Name)
	for _, g := range goals {
		fmt.Fprintf(&b, "  Goal %d: %s\n", g.Ordinal, g.Text)
	}
	b.WriteString("\nSTUDENTS IN THIS GROUP:\n")
	for _, s := range students {
		fmt.Fprintf(&b, "  %s\n", s.Name)
	}
	b.WriteString("\nTRANSCRIPT (oldest first):\n")
	for _, msg := range msgs {
		name := nameByID[msg.StudentID]
		if name == "" {
			name = "(unknown)"
		}
		fmt.Fprintf(&b, "  [%s] %s: %s\n", msg.At.Format("15:04:05"), name, msg.Body)
	}
	if len(known) > 0 {
		goalByID := map[string]model.Goal{}
		for _, g := range goals {
			goalByID[g.ID] = g
		}
		b.WriteString("\nMISCONCEPTIONS ALREADY COLLECTED IN THIS CLASS:\n")
		for _, k := range known {
			fmt.Fprintf(&b, "  (goal %d) %s\n", goalByID[k.GoalID].Ordinal, k.Text)
		}
		b.WriteString("If this group shows one of these SAME ideas, reuse that exact wording " +
			"so it counts as one misconception. Only write a new one for a genuinely different idea.\n")
	}

	b.WriteString("\nAssess this group now.")
	return b.String()
}

// applyRound resolves the model's names back to ids and persists everything.
// A name that matches no student on this team is dropped with a log line: the
// alternative is attributing a judgement to the wrong person.
func (m *Monitor) applyRound(sessionID string, team model.Team, goals []model.Goal,
	students []model.Student, out roundOut) {

	studentByName := map[string]model.Student{}
	for _, s := range students {
		studentByName[strings.ToLower(strings.TrimSpace(s.Name))] = s
	}
	goalByOrdinal := map[int]model.Goal{}
	for _, g := range goals {
		goalByOrdinal[g.Ordinal] = g
	}

	now := time.Now().UTC()
	for _, a := range out.Assessments {
		st, ok := studentByName[strings.ToLower(strings.TrimSpace(a.Student))]
		if !ok {
			m.logf("monitor %s: dropped assessment for unknown student %q", team.Name, a.Student)
			continue
		}
		g, ok := goalByOrdinal[a.Goal]
		if !ok {
			m.logf("monitor %s: dropped assessment for unknown goal %d", team.Name, a.Goal)
			continue
		}
		state := model.UnderstandingState(a.State)
		if !state.Valid() {
			m.logf("monitor %s: dropped assessment with invalid state %q", team.Name, a.State)
			continue
		}
		if err := m.Store.UpsertAssessment(model.Assessment{
			SessionID: sessionID, StudentID: st.ID, GoalID: g.ID, State: state,
			Confidence: a.Confidence, Evidence: a.Evidence, UpdatedAt: now,
		}); err != nil {
			m.logf("monitor %s: store assessment: %v", team.Name, err)
		}
	}

	var raised []model.Alert
	for _, al := range out.Alerts {
		kind := model.AlertKind(al.Kind)
		if !kind.Valid() {
			m.logf("monitor %s: dropped alert with invalid kind %q", team.Name, al.Kind)
			continue
		}
		sev := model.Severity(al.Severity)
		if !sev.Valid() {
			sev = model.SeverityWarn
		}
		alert := model.Alert{
			ID: uuid.NewString(), SessionID: sessionID, TeamID: team.ID,
			Kind: kind, Severity: sev, Title: al.Title, Detail: al.Detail,
			Quote: al.Quote, At: now,
		}
		if st, ok := studentByName[strings.ToLower(strings.TrimSpace(al.Student))]; ok {
			alert.StudentID = st.ID
		}
		if g, ok := goalByOrdinal[al.Goal]; ok {
			alert.GoalID = g.ID
		}
		inserted, err := m.Store.InsertAlert(&alert, dedupeKey(alert))
		if err != nil {
			m.logf("monitor %s: store alert: %v", team.Name, err)
			continue
		}
		if inserted {
			raised = append(raised, alert)
		}
	}

	for _, mc := range out.Misconceptions {
		g, ok := goalByOrdinal[mc.Goal]
		if !ok {
			continue
		}
		text := strings.TrimSpace(mc.Text)
		if text == "" {
			continue
		}
		if err := m.Store.RecordMisconception(&model.Misconception{
			ID: uuid.NewString(), SessionID: sessionID, GoalID: g.ID,
			Text: text, FirstSeen: now, LastSeen: now,
		}, normalizeMisconception(text), team.ID); err != nil {
			m.logf("monitor %s: record misconception: %v", team.Name, err)
		}
	}

	if len(raised) > 0 && m.OnRound != nil {
		m.OnRound(team.ID, raised)
	}
}

func (m *Monitor) writeAlerts(sessionID string, team model.Team, alerts []model.Alert) {
	var raised []model.Alert
	for i := range alerts {
		inserted, err := m.Store.InsertAlert(&alerts[i], dedupeKey(alerts[i]))
		if err != nil {
			m.logf("monitor %s: store alert: %v", team.Name, err)
			continue
		}
		if inserted {
			raised = append(raised, alerts[i])
		}
	}
	if len(raised) > 0 && m.OnRound != nil {
		m.OnRound(team.ID, raised)
	}
}

// dedupeKey collapses a standing problem into one alert. A team stuck on a
// goal for ten minutes is one thing the teacher should see once, not thirty
// identical rows burying everything else.
func dedupeKey(a model.Alert) string {
	return strings.Join([]string{string(a.Kind), a.TeamID, a.StudentID, a.GoalID}, "|")
}

// normalizeMisconception is the key two phrasings of the same wrong idea
// collapse on. Deliberately crude — lowercase, letters and digits only — so
// "Heavier objects fall faster!" and "heavier objects fall faster" merge.
// Genuinely different wordings of one idea still land apart; clustering those
// properly needs embeddings and is not worth it at this scale.
func normalizeMisconception(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func stringsOf[T ~string](in []T) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = string(v)
	}
	return out
}
