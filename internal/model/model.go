// Package model holds the domain types for the classroom monitoring demo and
// the vocabularies that constrain them.
//
// The vocabularies are values, not switch statements: the HTTP layer serves
// them at GET /api/vocabularies so the front end renders from what the server
// actually accepts instead of keeping its own copy that drifts.
package model

import "time"

// UnderstandingState is how well one student grasps one goal.
//
// Misunderstands is deliberately NOT a degree of Unknown. A student who knows
// they don't know asks a question; a student who misunderstands teaches the
// error to their team. Collapsing the two would hide the single most valuable
// alert this tool raises.
type UnderstandingState string

const (
	StateUnderstands    UnderstandingState = "understands"
	StatePartial        UnderstandingState = "partial"
	StateUnknown        UnderstandingState = "unknown"
	StateMisunderstands UnderstandingState = "misunderstands"
)

// UnderstandingStates is the ordered vocabulary, worst-to-best for display.
var UnderstandingStates = []UnderstandingState{
	StateMisunderstands, StateUnknown, StatePartial, StateUnderstands,
}

func (s UnderstandingState) Valid() bool {
	for _, v := range UnderstandingStates {
		if v == s {
			return true
		}
	}
	return false
}

// AlertKind is what the monitoring agent noticed.
type AlertKind string

const (
	// AlertGoalUnmastered: no member of a team understands a goal, so the team
	// cannot teach itself out of the hole and needs the teacher.
	AlertGoalUnmastered AlertKind = "goal_unmastered"
	// AlertConfidentlyWrong: a student is explaining a goal to teammates
	// incorrectly. The most urgent kind — the error is actively spreading.
	AlertConfidentlyWrong AlertKind = "confidently_wrong"
	// AlertConflict: interpersonal friction impeding the work.
	AlertConflict AlertKind = "conflict"
	// AlertDisengaged: a student has gone quiet or is contributing nothing.
	AlertDisengaged AlertKind = "disengaged"
	// AlertMisconception: a specific wrong idea worth collecting across teams.
	AlertMisconception AlertKind = "misconception"
)

var AlertKinds = []AlertKind{
	AlertConfidentlyWrong, AlertGoalUnmastered, AlertConflict,
	AlertDisengaged, AlertMisconception,
}

func (k AlertKind) Valid() bool {
	for _, v := range AlertKinds {
		if v == k {
			return true
		}
	}
	return false
}

// Severity ranks how loudly the teacher should be interrupted.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarn     Severity = "warn"
	SeverityCritical Severity = "critical"
)

var Severities = []Severity{SeverityInfo, SeverityWarn, SeverityCritical}

func (s Severity) Valid() bool {
	for _, v := range Severities {
		if v == s {
			return true
		}
	}
	return false
}

// SessionStatus is where a class session is in its life.
type SessionStatus string

const (
	SessionSetup   SessionStatus = "setup"
	SessionRunning SessionStatus = "running"
	SessionPaused  SessionStatus = "paused"
	SessionEnded   SessionStatus = "ended"
)

var SessionStatuses = []SessionStatus{
	SessionSetup, SessionRunning, SessionPaused, SessionEnded,
}

// Session is one class period with a set of goals and a set of teams.
type Session struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	Subject   string        `json:"subject"`
	Status    SessionStatus `json:"status"`
	CreatedAt time.Time     `json:"created_at"`
	StartedAt *time.Time    `json:"started_at,omitempty"`
	EndedAt   *time.Time    `json:"ended_at,omitempty"`
	// CurrentPhase is the segment of the lesson currently being played. It
	// advances as the transcript crosses a phase boundary, and it is what the
	// accuracy panel scores the agent's latest opinion against — judging a
	// fresh assessment against phase 1's truth would mark the agent wrong for
	// noticing that somebody has since learned something.
	CurrentPhase int `json:"current_phase"`
	PhaseCount   int `json:"phase_count"`
}

// Goal is one learning objective for a session.
type Goal struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	Ordinal   int    `json:"ordinal"`
	Text      string `json:"text"`
	// ShortLabel is for axis headers and chips, where the full text will not
	// fit. Presentation carries it; nothing joins on it.
	ShortLabel string `json:"short_label"`
}

// Team is one breakout group.
type Team struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	Ordinal   int    `json:"ordinal"`
	Name      string `json:"name"`
}

// Student is one member of one team.
//
// IsHuman separates a real person who joined through the browser from a
// simulated participant. It decides two things: simulated students get a
// scripted transcript and a ground-truth row, humans get neither, and the
// accuracy panel scores only the simulated ones because they are the only
// ones whose true state is knowable.
type Student struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	TeamID    string `json:"team_id"`
	Name      string `json:"name"`
	IsHuman   bool   `json:"is_human"`
	// JoinToken is the unguessable path segment a human uses to claim this
	// seat. Empty for simulated students.
	JoinToken string     `json:"-"`
	JoinedAt  *time.Time `json:"joined_at,omitempty"`
}

// Truth is a simulated student's actual understanding of a goal DURING ONE
// PHASE: the hidden variable the simulation draws their dialogue from and the
// monitoring agent is trying to recover. Humans have no Truth row.
//
// Phase is what makes a lesson a lesson rather than a snapshot. Understanding
// is advanced between phases by the peer-learning rule, so the same student and
// goal carry a different state early and late, and the difference is the
// learning the dashboard reports.
type Truth struct {
	StudentID string             `json:"student_id"`
	GoalID    string             `json:"goal_id"`
	Phase     int                `json:"phase"`
	State     UnderstandingState `json:"state"`
}

// PhaseTruth is the whole class's understanding at one point in the lesson.
type PhaseTruth struct {
	Phase  int     `json:"phase"`
	Truths []Truth `json:"truths"`
}

// Message is one utterance in a team room, from a simulated or a real student.
// Seq is a per-session monotonic counter so a client can resume a stream and
// the monitor can ask for "everything after N" without timestamp ties.
type Message struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	TeamID    string    `json:"team_id"`
	StudentID string    `json:"student_id"`
	Seq       int64     `json:"seq"`
	Body      string    `json:"body"`
	At        time.Time `json:"at"`
}

// Assessment is the monitoring agent's inference about one student and one
// goal. It is the agent's belief, never the truth — the two are compared only
// in the accuracy panel and never merged.
type Assessment struct {
	SessionID  string             `json:"session_id"`
	StudentID  string             `json:"student_id"`
	GoalID     string             `json:"goal_id"`
	State      UnderstandingState `json:"state"`
	Confidence float64            `json:"confidence"`
	Evidence   string             `json:"evidence"`
	UpdatedAt  time.Time          `json:"updated_at"`
}

// Alert is something the teacher should look at, raised by the monitoring
// agent against a team and optionally a specific student and goal.
type Alert struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	TeamID    string    `json:"team_id"`
	StudentID string    `json:"student_id,omitempty"`
	GoalID    string    `json:"goal_id,omitempty"`
	Kind      AlertKind `json:"kind"`
	Severity  Severity  `json:"severity"`
	Title     string    `json:"title"`
	Detail    string    `json:"detail"`
	Quote     string    `json:"quote,omitempty"`
	At        time.Time `json:"at"`
	Resolved  bool      `json:"resolved"`
}

// Misconception is a specific wrong idea, collected across every team that
// showed it. This is the artifact a teacher takes away and reteaches from.
type Misconception struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	GoalID    string    `json:"goal_id"`
	Text      string    `json:"text"`
	TeamIDs   []string  `json:"team_ids"`
	Count     int       `json:"count"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
}

// Truthlike is the one thing Progress needs from a row: which student, which
// goal, and what state.
//
// Both an Assessment (what the agent believes) and a Truth (what is actually
// so) satisfy it, which is what lets one comparison serve both halves of the
// progress view without either being converted into the other and losing what
// it is.
type Truthlike interface {
	Student() string
	Goal() string
	Understanding() UnderstandingState
}

func (t Truth) Student() string                   { return t.StudentID }
func (t Truth) Goal() string                      { return t.GoalID }
func (t Truth) Understanding() UnderstandingState { return t.State }

func (a Assessment) Student() string                   { return a.StudentID }
func (a Assessment) Goal() string                      { return a.GoalID }
func (a Assessment) Understanding() UnderstandingState { return a.State }
