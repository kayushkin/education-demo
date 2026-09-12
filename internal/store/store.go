// Package store is the SQLite persistence layer.
//
// It holds no policy: it writes what it is given and returns what it holds.
// Deciding what a state means, when to raise an alert, or how to score the
// agent belongs above this layer.
package store

import (
	"database/sql"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/kayushkin/education-demo/internal/model"
	_ "github.com/mattn/go-sqlite3"
)

//go:embed schema.sql
var schemaSQL string

type Store struct{ db *sql.DB }

// Open opens the database at path and applies the schema.
//
// _txlock=immediate is load-bearing: appendMessage bumps the session's seq
// counter and inserts a row reading it, and under deferred locking two
// concurrent senders can both read the old value and be handed the same seq.
func Open(path string) (*Store, error) {
	dsn := path + "?_journal_mode=WAL&_foreign_keys=on&_txlock=immediate&_busy_timeout=5000"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	// SQLite takes one writer at a time; letting database/sql open many
	// connections buys nothing and turns lock contention into SQLITE_BUSY.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func ms(t time.Time) int64     { return t.UnixMilli() }
func fromMs(v int64) time.Time { return time.UnixMilli(v).UTC() }
func msPtr(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := fromMs(v.Int64)
	return &t
}

// ---------- sessions ----------

func (s *Store) CreateSession(sess *model.Session) error {
	phaseCount := sess.PhaseCount
	if phaseCount <= 0 {
		phaseCount = 1
	}
	_, err := s.db.Exec(
		`INSERT INTO sessions (id, title, subject, status, created_at, current_phase, phase_count)
		 VALUES (?,?,?,?,?,1,?)`,
		sess.ID, sess.Title, sess.Subject, string(sess.Status), ms(sess.CreatedAt), phaseCount)
	return err
}

func (s *Store) GetSession(id string) (*model.Session, error) {
	row := s.db.QueryRow(
		`SELECT id, title, subject, status, created_at, started_at, ended_at, current_phase, phase_count
		 FROM sessions WHERE id = ?`, id)
	var out model.Session
	var created int64
	var started, ended sql.NullInt64
	var status string
	if err := row.Scan(&out.ID, &out.Title, &out.Subject, &status, &created, &started, &ended,
		&out.CurrentPhase, &out.PhaseCount); err != nil {
		return nil, err
	}
	out.Status = model.SessionStatus(status)
	out.CreatedAt = fromMs(created)
	out.StartedAt, out.EndedAt = msPtr(started), msPtr(ended)
	return &out, nil
}

func (s *Store) ListSessions() ([]model.Session, error) {
	rows, err := s.db.Query(
		`SELECT id, title, subject, status, created_at, started_at, ended_at, current_phase, phase_count
		 FROM sessions ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Session{}
	for rows.Next() {
		var v model.Session
		var created int64
		var started, ended sql.NullInt64
		var status string
		if err := rows.Scan(&v.ID, &v.Title, &v.Subject, &status, &created, &started, &ended,
			&v.CurrentPhase, &v.PhaseCount); err != nil {
			return nil, err
		}
		v.Status = model.SessionStatus(status)
		v.CreatedAt = fromMs(created)
		v.StartedAt, v.EndedAt = msPtr(started), msPtr(ended)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) SetSessionStatus(id string, st model.SessionStatus) error {
	var col string
	switch st {
	case model.SessionRunning:
		col = `, started_at = COALESCE(started_at, ?)`
	case model.SessionEnded:
		col = `, ended_at = ?`
	}
	if col == "" {
		_, err := s.db.Exec(`UPDATE sessions SET status = ? WHERE id = ?`, string(st), id)
		return err
	}
	_, err := s.db.Exec(
		`UPDATE sessions SET status = ?`+col+` WHERE id = ?`, string(st), ms(time.Now()), id)
	return err
}

// ---------- goals, teams, students ----------

func (s *Store) CreateGoal(g *model.Goal) error {
	_, err := s.db.Exec(
		`INSERT INTO goals (id, session_id, ordinal, text, short_label) VALUES (?,?,?,?,?)`,
		g.ID, g.SessionID, g.Ordinal, g.Text, g.ShortLabel)
	return err
}

func (s *Store) ListGoals(sessionID string) ([]model.Goal, error) {
	rows, err := s.db.Query(
		`SELECT id, session_id, ordinal, text, short_label FROM goals
		 WHERE session_id = ? ORDER BY ordinal`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Goal{}
	for rows.Next() {
		var g model.Goal
		if err := rows.Scan(&g.ID, &g.SessionID, &g.Ordinal, &g.Text, &g.ShortLabel); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) CreateTeam(t *model.Team) error {
	_, err := s.db.Exec(
		`INSERT INTO teams (id, session_id, ordinal, name) VALUES (?,?,?,?)`,
		t.ID, t.SessionID, t.Ordinal, t.Name)
	return err
}

func (s *Store) ListTeams(sessionID string) ([]model.Team, error) {
	rows, err := s.db.Query(
		`SELECT id, session_id, ordinal, name FROM teams WHERE session_id = ? ORDER BY ordinal`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Team{}
	for rows.Next() {
		var t model.Team
		if err := rows.Scan(&t.ID, &t.SessionID, &t.Ordinal, &t.Name); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) CreateStudent(st *model.Student) error {
	_, err := s.db.Exec(
		`INSERT INTO students (id, session_id, team_id, name, is_human, join_token)
		 VALUES (?,?,?,?,?,?)`,
		st.ID, st.SessionID, st.TeamID, st.Name, st.IsHuman, st.JoinToken)
	return err
}

func scanStudents(rows *sql.Rows) ([]model.Student, error) {
	defer rows.Close()
	out := []model.Student{}
	for rows.Next() {
		var v model.Student
		var human int
		var joined sql.NullInt64
		if err := rows.Scan(&v.ID, &v.SessionID, &v.TeamID, &v.Name, &human, &v.JoinToken, &joined); err != nil {
			return nil, err
		}
		v.IsHuman = human != 0
		v.JoinedAt = msPtr(joined)
		out = append(out, v)
	}
	return out, rows.Err()
}

const studentCols = `id, session_id, team_id, name, is_human, join_token, joined_at`

func (s *Store) ListStudents(sessionID string) ([]model.Student, error) {
	rows, err := s.db.Query(
		`SELECT `+studentCols+` FROM students WHERE session_id = ? ORDER BY team_id, name`, sessionID)
	if err != nil {
		return nil, err
	}
	return scanStudents(rows)
}

func (s *Store) ListTeamStudents(teamID string) ([]model.Student, error) {
	rows, err := s.db.Query(
		`SELECT `+studentCols+` FROM students WHERE team_id = ? ORDER BY name`, teamID)
	if err != nil {
		return nil, err
	}
	return scanStudents(rows)
}

func (s *Store) GetStudent(id string) (*model.Student, error) {
	rows, err := s.db.Query(`SELECT `+studentCols+` FROM students WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	list, err := scanStudents(rows)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, sql.ErrNoRows
	}
	return &list[0], nil
}

// GetStudentByToken resolves a human's join link to the seat it claims.
func (s *Store) GetStudentByToken(token string) (*model.Student, error) {
	if token == "" {
		return nil, sql.ErrNoRows
	}
	rows, err := s.db.Query(`SELECT `+studentCols+` FROM students WHERE join_token = ?`, token)
	if err != nil {
		return nil, err
	}
	list, err := scanStudents(rows)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, sql.ErrNoRows
	}
	return &list[0], nil
}

// AddHumanStudent seats a real person in a team as a NEW student.
//
// It deliberately does NOT let a person take over a simulated student's seat.
// A seat that has already spoken carries a transcript history, and renaming it
// re-attributes every one of those lines to whoever just sat down — measured:
// a human who joined mid-session was assessed on three goals using the
// previous occupant's words. A new row has no history to misattribute, no
// scripted lines to cancel, and no ground truth to invalidate.
//
// The team grows by one, which is what actually happened.
func (s *Store) AddHumanStudent(st *model.Student) error {
	_, err := s.db.Exec(
		`INSERT INTO students (id, session_id, team_id, name, is_human, join_token, joined_at)
		 VALUES (?,?,?,?,1,?,?)`,
		st.ID, st.SessionID, st.TeamID, st.Name, st.JoinToken, ms(time.Now()))
	return err
}

// ---------- truth ----------

func (s *Store) SetTruth(t model.Truth) error {
	phase := t.Phase
	if phase <= 0 {
		phase = 1
	}
	_, err := s.db.Exec(
		`INSERT INTO truths (student_id, goal_id, phase, state) VALUES (?,?,?,?)
		 ON CONFLICT(student_id, goal_id, phase) DO UPDATE SET state = excluded.state`,
		t.StudentID, t.GoalID, phase, string(t.State))
	return err
}

// InsertPhaseTruths writes a whole phase in one transaction.
func (s *Store) InsertPhaseTruths(truths []model.Truth) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(
		`INSERT INTO truths (student_id, goal_id, phase, state) VALUES (?,?,?,?)
		 ON CONFLICT(student_id, goal_id, phase) DO UPDATE SET state = excluded.state`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, t := range truths {
		if _, err := stmt.Exec(t.StudentID, t.GoalID, t.Phase, string(t.State)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListTruthsAtPhase returns the class's ground truth during one phase.
func (s *Store) ListTruthsAtPhase(sessionID string, phase int) ([]model.Truth, error) {
	rows, err := s.db.Query(
		`SELECT t.student_id, t.goal_id, t.phase, t.state FROM truths t
		 JOIN students st ON st.id = t.student_id
		 WHERE st.session_id = ? AND t.phase = ?`, sessionID, phase)
	if err != nil {
		return nil, err
	}
	return scanTruths(rows)
}

// MaxTruthPhase is the last phase ground truth was written for.
func (s *Store) MaxTruthPhase(sessionID string) (int, error) {
	var n sql.NullInt64
	err := s.db.QueryRow(
		`SELECT MAX(t.phase) FROM truths t JOIN students st ON st.id = t.student_id
		 WHERE st.session_id = ?`, sessionID).Scan(&n)
	if err != nil || !n.Valid {
		return 0, err
	}
	return int(n.Int64), nil
}

func scanTruths(rows *sql.Rows) ([]model.Truth, error) {
	defer rows.Close()
	out := []model.Truth{}
	for rows.Next() {
		var v model.Truth
		var state string
		if err := rows.Scan(&v.StudentID, &v.GoalID, &v.Phase, &state); err != nil {
			return nil, err
		}
		v.State = model.UnderstandingState(state)
		out = append(out, v)
	}
	return out, rows.Err()
}

// ListTruths returns ground truth for the phase the session is CURRENTLY in.
//
// That is the right thing to score the agent's latest opinion against: judging
// a fresh assessment against phase 1 would mark the agent wrong for correctly
// noticing that a student has since learned something.
func (s *Store) ListTruths(sessionID string) ([]model.Truth, error) {
	rows, err := s.db.Query(
		`SELECT t.student_id, t.goal_id, t.phase, t.state FROM truths t
		 JOIN students st ON st.id = t.student_id
		 WHERE st.session_id = ?
		   AND t.phase = (SELECT current_phase FROM sessions WHERE id = ?)`,
		sessionID, sessionID)
	if err != nil {
		return nil, err
	}
	return scanTruths(rows)
}

// ---------- messages ----------

// AppendMessage assigns the next session sequence number and stores the
// message under it, both inside one immediate transaction.
func (s *Store) AppendMessage(m *model.Message) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE sessions SET seq = seq + 1 WHERE id = ?`, m.SessionID); err != nil {
		return err
	}
	if err := tx.QueryRow(`SELECT seq FROM sessions WHERE id = ?`, m.SessionID).Scan(&m.Seq); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`INSERT INTO messages (id, session_id, team_id, student_id, seq, body, at) VALUES (?,?,?,?,?,?,?)`,
		m.ID, m.SessionID, m.TeamID, m.StudentID, m.Seq, m.Body, ms(m.At)); err != nil {
		return err
	}
	return tx.Commit()
}

func scanMessages(rows *sql.Rows) ([]model.Message, error) {
	defer rows.Close()
	out := []model.Message{}
	for rows.Next() {
		var v model.Message
		var at int64
		if err := rows.Scan(&v.ID, &v.SessionID, &v.TeamID, &v.StudentID, &v.Seq, &v.Body, &at); err != nil {
			return nil, err
		}
		v.At = fromMs(at)
		out = append(out, v)
	}
	return out, rows.Err()
}

const messageCols = `id, session_id, team_id, student_id, seq, body, at`

// ListTeamMessagesSince returns a team's messages after seq, oldest first.
func (s *Store) ListTeamMessagesSince(teamID string, seq int64, limit int) ([]model.Message, error) {
	rows, err := s.db.Query(
		`SELECT `+messageCols+` FROM messages WHERE team_id = ? AND seq > ?
		 ORDER BY seq LIMIT ?`, teamID, seq, limit)
	if err != nil {
		return nil, err
	}
	return scanMessages(rows)
}

// ListTeamMessagesTail returns the newest n messages for a team, oldest first,
// which is the window the monitor reads and the room renders.
func (s *Store) ListTeamMessagesTail(teamID string, n int) ([]model.Message, error) {
	rows, err := s.db.Query(
		`SELECT `+messageCols+` FROM (
		   SELECT `+messageCols+` FROM messages WHERE team_id = ? ORDER BY seq DESC LIMIT ?
		 ) ORDER BY seq`, teamID, n)
	if err != nil {
		return nil, err
	}
	return scanMessages(rows)
}

func (s *Store) ListSessionMessagesSince(sessionID string, seq int64, limit int) ([]model.Message, error) {
	rows, err := s.db.Query(
		`SELECT `+messageCols+` FROM messages WHERE session_id = ? AND seq > ?
		 ORDER BY seq LIMIT ?`, sessionID, seq, limit)
	if err != nil {
		return nil, err
	}
	return scanMessages(rows)
}

// ---------- scripted lines ----------

type ScriptedLine struct {
	ID        string
	SessionID string
	TeamID    string
	StudentID string
	Ordinal   int
	Body      string
	GapMs     int
	// Phase is the segment of the lesson this line belongs to. Playing it
	// advances the session into that phase.
	Phase int
}

func (s *Store) InsertScriptedLines(lines []ScriptedLine) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(
		`INSERT INTO scripted_lines (id, session_id, team_id, student_id, ordinal, body, gap_ms, phase)
		 VALUES (?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, l := range lines {
		phase := l.Phase
		if phase <= 0 {
			phase = 1
		}
		if _, err := stmt.Exec(l.ID, l.SessionID, l.TeamID, l.StudentID, l.Ordinal,
			l.Body, l.GapMs, phase); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// NextScriptedLine returns a team's next unplayed line, or nil when the script
// is spent.
func (s *Store) NextScriptedLine(teamID string) (*ScriptedLine, error) {
	row := s.db.QueryRow(
		`SELECT id, session_id, team_id, student_id, ordinal, body, gap_ms, phase FROM scripted_lines
		 WHERE team_id = ? AND played_at IS NULL ORDER BY ordinal LIMIT 1`, teamID)
	var l ScriptedLine
	if err := row.Scan(&l.ID, &l.SessionID, &l.TeamID, &l.StudentID, &l.Ordinal,
		&l.Body, &l.GapMs, &l.Phase); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &l, nil
}

func (s *Store) MarkLinePlayed(id string) error {
	_, err := s.db.Exec(`UPDATE scripted_lines SET played_at = ? WHERE id = ?`, ms(time.Now()), id)
	return err
}

// CountScriptedLines counts every scripted line, played or not.
//
// This is the question "has a transcript ever been written for this session?",
// which is NOT the same as "is there anything left to play". A session whose
// script has fully played has zero unplayed lines and must not be handed a
// second transcript.
func (s *Store) CountScriptedLines(sessionID string) (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM scripted_lines WHERE session_id = ?`, sessionID).Scan(&n)
	return n, err
}

func (s *Store) CountUnplayedLines(sessionID string) (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM scripted_lines WHERE session_id = ? AND played_at IS NULL`, sessionID).Scan(&n)
	return n, err
}

// ---------- assessments ----------

// UpsertAssessment stores the agent's current opinion and appends to the
// history whenever that opinion is NEW or DIFFERENT.
//
// Appending only on change is deliberate: a round that re-confirms what the
// agent already thought is not a change of mind, and recording it would bury
// the handful of real transitions -- the moment a student stopped being lost --
// under hundreds of identical rows.
func (s *Store) UpsertAssessment(a model.Assessment, phase int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var prior string
	err = tx.QueryRow(
		`SELECT state FROM assessments WHERE student_id = ? AND goal_id = ?`,
		a.StudentID, a.GoalID).Scan(&prior)
	changed := err == sql.ErrNoRows || (err == nil && prior != string(a.State))
	if err != nil && err != sql.ErrNoRows {
		return err
	}

	if _, err := tx.Exec(
		`INSERT INTO assessments (session_id, student_id, goal_id, state, confidence, evidence, updated_at)
		 VALUES (?,?,?,?,?,?,?)
		 ON CONFLICT(student_id, goal_id) DO UPDATE SET
		   state = excluded.state, confidence = excluded.confidence,
		   evidence = excluded.evidence, updated_at = excluded.updated_at`,
		a.SessionID, a.StudentID, a.GoalID, string(a.State), a.Confidence, a.Evidence,
		ms(a.UpdatedAt)); err != nil {
		return err
	}
	if changed {
		if _, err := tx.Exec(
			`INSERT INTO assessment_history (session_id, student_id, goal_id, state, confidence, phase, at)
			 VALUES (?,?,?,?,?,?,?)`,
			a.SessionID, a.StudentID, a.GoalID, string(a.State), a.Confidence, phase,
			ms(a.UpdatedAt)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// FirstAndLatestAssessments returns, per (student, goal), the agent's earliest
// recorded opinion and its current one. This is the agent's OWN view of what
// changed over the lesson, which is the honest one to show a teacher: it is
// what the tool would report in a real classroom, where no ground truth exists.
func (s *Store) FirstAndLatestAssessments(sessionID string) (first, latest []model.Assessment, err error) {
	rows, err := s.db.Query(
		`SELECT h.student_id, h.goal_id, h.state, h.confidence, h.phase, h.at, h.id
		 FROM assessment_history h WHERE h.session_id = ? ORDER BY h.id`, sessionID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	type key struct{ student, goal string }
	firstOf := map[key]model.Assessment{}
	latestOf := map[key]model.Assessment{}
	order := []key{}
	for rows.Next() {
		var a model.Assessment
		var state string
		var at int64
		var phase, id int
		if err := rows.Scan(&a.StudentID, &a.GoalID, &state, &a.Confidence, &phase, &at, &id); err != nil {
			return nil, nil, err
		}
		a.SessionID = sessionID
		a.State = model.UnderstandingState(state)
		a.UpdatedAt = fromMs(at)
		k := key{a.StudentID, a.GoalID}
		if _, seen := firstOf[k]; !seen {
			firstOf[k] = a
			order = append(order, k)
		}
		latestOf[k] = a
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	for _, k := range order {
		first = append(first, firstOf[k])
		latest = append(latest, latestOf[k])
	}
	return first, latest, nil
}

// AdvanceSessionPhase moves the session into phase, never backwards.
func (s *Store) AdvanceSessionPhase(sessionID string, phase int) error {
	_, err := s.db.Exec(
		`UPDATE sessions SET current_phase = ? WHERE id = ? AND current_phase < ?`,
		phase, sessionID, phase)
	return err
}

func (s *Store) ListAssessments(sessionID string) ([]model.Assessment, error) {
	rows, err := s.db.Query(
		`SELECT session_id, student_id, goal_id, state, confidence, evidence, updated_at
		 FROM assessments WHERE session_id = ?`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Assessment{}
	for rows.Next() {
		var v model.Assessment
		var state string
		var updated int64
		if err := rows.Scan(&v.SessionID, &v.StudentID, &v.GoalID, &state, &v.Confidence, &v.Evidence, &updated); err != nil {
			return nil, err
		}
		v.State = model.UnderstandingState(state)
		v.UpdatedAt = fromMs(updated)
		out = append(out, v)
	}
	return out, rows.Err()
}

// ---------- alerts ----------

// InsertAlert stores an alert, or reports that an unresolved one with the same
// dedupe key is already standing. inserted=false is an ordinary outcome, not a
// failure: it means the teacher has already been told.
func (s *Store) InsertAlert(a *model.Alert, dedupeKey string) (inserted bool, err error) {
	res, err := s.db.Exec(
		`INSERT INTO alerts (id, session_id, team_id, student_id, goal_id, kind, severity,
		                     title, detail, quote, at, resolved, dedupe_key)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,0,?)
		 ON CONFLICT DO NOTHING`,
		a.ID, a.SessionID, a.TeamID, a.StudentID, a.GoalID, string(a.Kind), string(a.Severity),
		a.Title, a.Detail, a.Quote, ms(a.At), dedupeKey)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (s *Store) ListAlerts(sessionID string, includeResolved bool) ([]model.Alert, error) {
	q := `SELECT id, session_id, team_id, student_id, goal_id, kind, severity, title, detail,
	             quote, at, resolved FROM alerts WHERE session_id = ?`
	if !includeResolved {
		q += ` AND resolved = 0`
	}
	q += ` ORDER BY at DESC LIMIT 300`
	rows, err := s.db.Query(q, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Alert{}
	for rows.Next() {
		var v model.Alert
		var kind, sev string
		var at int64
		var resolved int
		if err := rows.Scan(&v.ID, &v.SessionID, &v.TeamID, &v.StudentID, &v.GoalID,
			&kind, &sev, &v.Title, &v.Detail, &v.Quote, &at, &resolved); err != nil {
			return nil, err
		}
		v.Kind, v.Severity = model.AlertKind(kind), model.Severity(sev)
		v.At, v.Resolved = fromMs(at), resolved != 0
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) ResolveAlert(id string) error {
	_, err := s.db.Exec(`UPDATE alerts SET resolved = 1 WHERE id = ?`, id)
	return err
}

// ---------- misconceptions ----------

// RecordMisconception folds a wrong idea into the collection, counting a
// repeat rather than storing it twice. normKey is the caller's normalization
// of the text; this layer does not invent one.
func (s *Store) RecordMisconception(m *model.Misconception, normKey, teamID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRow(
		`SELECT id FROM misconceptions WHERE session_id = ? AND goal_id = ? AND norm_key = ?`,
		m.SessionID, m.GoalID, normKey).Scan(&id)
	switch {
	case err == sql.ErrNoRows:
		id = m.ID
		if _, err := tx.Exec(
			`INSERT INTO misconceptions (id, session_id, goal_id, text, norm_key, count, first_seen, last_seen)
			 VALUES (?,?,?,?,?,1,?,?)`,
			id, m.SessionID, m.GoalID, m.Text, normKey, ms(m.FirstSeen), ms(m.LastSeen)); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		if _, err := tx.Exec(
			`UPDATE misconceptions SET count = count + 1, last_seen = ? WHERE id = ?`,
			ms(m.LastSeen), id); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(
		`INSERT INTO misconception_teams (misconception_id, team_id) VALUES (?,?) ON CONFLICT DO NOTHING`,
		id, teamID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ListMisconceptions(sessionID string) ([]model.Misconception, error) {
	rows, err := s.db.Query(
		`SELECT m.id, m.session_id, m.goal_id, m.text, m.count, m.first_seen, m.last_seen,
		        COALESCE(GROUP_CONCAT(mt.team_id), '')
		 FROM misconceptions m
		 LEFT JOIN misconception_teams mt ON mt.misconception_id = m.id
		 WHERE m.session_id = ?
		 GROUP BY m.id ORDER BY m.count DESC, m.last_seen DESC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Misconception{}
	for rows.Next() {
		var v model.Misconception
		var first, last int64
		var teams string
		if err := rows.Scan(&v.ID, &v.SessionID, &v.GoalID, &v.Text, &v.Count, &first, &last, &teams); err != nil {
			return nil, err
		}
		v.FirstSeen, v.LastSeen = fromMs(first), fromMs(last)
		v.TeamIDs = []string{}
		if teams != "" {
			v.TeamIDs = strings.Split(teams, ",")
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ---------- monitor cursors ----------

// MonitorCursor reports how far the monitor has read in a team's room and
// which act it last assessed it in.
func (s *Store) MonitorCursor(teamID string) (lastSeq int64, lastPhase int, err error) {
	err = s.db.QueryRow(
		`SELECT last_seq, last_phase FROM monitor_cursors WHERE team_id = ?`,
		teamID).Scan(&lastSeq, &lastPhase)
	if err == sql.ErrNoRows {
		return 0, 0, nil
	}
	return lastSeq, lastPhase, err
}

func (s *Store) SetMonitorCursor(teamID string, lastSeq int64, phase int, lastErr string) error {
	_, err := s.db.Exec(
		`INSERT INTO monitor_cursors (team_id, last_seq, ran_at, last_error, last_phase)
		 VALUES (?,?,?,?,?)
		 ON CONFLICT(team_id) DO UPDATE SET
		   last_seq = excluded.last_seq, ran_at = excluded.ran_at,
		   last_error = excluded.last_error, last_phase = excluded.last_phase`,
		teamID, lastSeq, ms(time.Now()), lastErr, phase)
	return err
}
