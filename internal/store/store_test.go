package store

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kayushkin/education-demo/internal/model"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// seedSession creates the minimum graph a message needs: session, goal, team,
// student.
func seedSession(t *testing.T, st *Store) (sessionID, teamID, studentID, goalID string) {
	t.Helper()
	sessionID = uuid.NewString()
	if err := st.CreateSession(&model.Session{
		ID: sessionID, Title: "T", Status: model.SessionSetup, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	goalID = uuid.NewString()
	if err := st.CreateGoal(&model.Goal{ID: goalID, SessionID: sessionID, Ordinal: 1, Text: "g"}); err != nil {
		t.Fatalf("create goal: %v", err)
	}
	teamID = uuid.NewString()
	if err := st.CreateTeam(&model.Team{ID: teamID, SessionID: sessionID, Ordinal: 1, Name: "Team 1"}); err != nil {
		t.Fatalf("create team: %v", err)
	}
	studentID = uuid.NewString()
	if err := st.CreateStudent(&model.Student{
		ID: studentID, SessionID: sessionID, TeamID: teamID, Name: "Amara", JoinToken: "tok-1",
	}); err != nil {
		t.Fatalf("create student: %v", err)
	}
	return
}

// TestConcurrentAppendsGetDistinctSeq is why the DSN carries _txlock=immediate.
// Two writers racing on the session counter must not be handed the same
// sequence number: the monitor's "everything after N" cursor would silently
// skip a message.
func TestConcurrentAppendsGetDistinctSeq(t *testing.T) {
	st := newTestStore(t)
	sessionID, teamID, studentID, _ := seedSession(t, st)

	const n = 40
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := st.AppendMessage(&model.Message{
				ID: uuid.NewString(), SessionID: sessionID, TeamID: teamID,
				StudentID: studentID, Body: "hello", At: time.Now(),
			})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	msgs, err := st.ListTeamMessagesSince(teamID, 0, 1000)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(msgs) != n {
		t.Fatalf("got %d messages, want %d", len(msgs), n)
	}
	seen := map[int64]bool{}
	for _, m := range msgs {
		if seen[m.Seq] {
			t.Fatalf("sequence %d handed out twice", m.Seq)
		}
		seen[m.Seq] = true
	}
}

// TestAlertDedupeSuppressesStandingProblem pins that a team stuck on a goal
// for ten minutes produces one alert, not thirty.
func TestAlertDedupeSuppressesStandingProblem(t *testing.T) {
	st := newTestStore(t)
	sessionID, teamID, studentID, goalID := seedSession(t, st)

	mk := func() *model.Alert {
		return &model.Alert{
			ID: uuid.NewString(), SessionID: sessionID, TeamID: teamID,
			StudentID: studentID, GoalID: goalID, Kind: model.AlertConfidentlyWrong,
			Severity: model.SeverityCritical, Title: "wrong", At: time.Now(),
		}
	}
	key := "confidently_wrong|" + teamID + "|" + studentID + "|" + goalID

	first, err := st.InsertAlert(mk(), key)
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if !first {
		t.Fatal("first alert was not inserted")
	}
	second, err := st.InsertAlert(mk(), key)
	if err != nil {
		t.Fatalf("second insert: %v", err)
	}
	if second {
		t.Error("the same standing problem was inserted twice")
	}

	open, err := st.ListAlerts(sessionID, false)
	if err != nil {
		t.Fatalf("list alerts: %v", err)
	}
	if len(open) != 1 {
		t.Fatalf("got %d open alerts, want 1", len(open))
	}

	// Once the teacher resolves it, the same problem may be raised again —
	// otherwise a dismissed alert could never come back.
	if err := st.ResolveAlert(open[0].ID); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	third, err := st.InsertAlert(mk(), key)
	if err != nil {
		t.Fatalf("third insert: %v", err)
	}
	if !third {
		t.Error("alert could not be re-raised after being resolved")
	}
}

// TestClaimSeatDropsScriptAndTruth pins the two things that must happen when a
// real person takes a simulated seat: the script stops speaking through their
// name, and their ground truth goes, because nobody knows what a real person
// understands and scoring the agent against a leftover fiction would be wrong.
func TestClaimSeatDropsScriptAndTruth(t *testing.T) {
	st := newTestStore(t)
	sessionID, teamID, studentID, goalID := seedSession(t, st)

	if err := st.SetTruth(model.Truth{
		StudentID: studentID, GoalID: goalID, State: model.StateUnderstands,
	}); err != nil {
		t.Fatalf("set truth: %v", err)
	}
	if err := st.InsertScriptedLines([]ScriptedLine{{
		ID: uuid.NewString(), SessionID: sessionID, TeamID: teamID,
		StudentID: studentID, Ordinal: 1, Body: "scripted", GapMs: 1000,
	}}); err != nil {
		t.Fatalf("insert script: %v", err)
	}

	if err := st.ClaimSeat(studentID, "Real Person"); err != nil {
		t.Fatalf("claim seat: %v", err)
	}

	line, err := st.NextScriptedLine(teamID)
	if err != nil {
		t.Fatalf("next line: %v", err)
	}
	if line != nil {
		t.Errorf("scripted line survived the claim: %q", line.Body)
	}
	truths, err := st.ListTruths(sessionID)
	if err != nil {
		t.Fatalf("list truths: %v", err)
	}
	if len(truths) != 0 {
		t.Errorf("ground truth survived the claim: %+v", truths)
	}
	student, err := st.GetStudent(studentID)
	if err != nil {
		t.Fatalf("get student: %v", err)
	}
	if !student.IsHuman || student.Name != "Real Person" {
		t.Errorf("seat not claimed: is_human=%v name=%q", student.IsHuman, student.Name)
	}
}

// TestMisconceptionFoldsRepeats checks that the same wrong idea from two teams
// is one row with a count of two and both teams listed.
func TestMisconceptionFoldsRepeats(t *testing.T) {
	st := newTestStore(t)
	sessionID, teamID, _, goalID := seedSession(t, st)

	teamB := uuid.NewString()
	if err := st.CreateTeam(&model.Team{
		ID: teamB, SessionID: sessionID, Ordinal: 2, Name: "Team 2",
	}); err != nil {
		t.Fatalf("create team: %v", err)
	}

	now := time.Now()
	for _, team := range []string{teamID, teamB} {
		if err := st.RecordMisconception(&model.Misconception{
			ID: uuid.NewString(), SessionID: sessionID, GoalID: goalID,
			Text: "heavier objects fall faster", FirstSeen: now, LastSeen: now,
		}, "heavier objects fall faster", team); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	list, err := st.ListMisconceptions(sessionID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d misconceptions, want 1 folded row", len(list))
	}
	if list[0].Count != 2 {
		t.Errorf("count = %d, want 2", list[0].Count)
	}
	if len(list[0].TeamIDs) != 2 {
		t.Errorf("team_ids = %v, want both teams", list[0].TeamIDs)
	}
}

// TestTokenAuthenticatesTheRightSeat pins that a token resolves to exactly its
// own seat and an unknown token resolves to none.
func TestTokenAuthenticatesTheRightSeat(t *testing.T) {
	st := newTestStore(t)
	_, _, studentID, _ := seedSession(t, st)

	got, err := st.GetStudentByToken("tok-1")
	if err != nil {
		t.Fatalf("by token: %v", err)
	}
	if got.ID != studentID {
		t.Errorf("token resolved to %s, want %s", got.ID, studentID)
	}
	if _, err := st.GetStudentByToken("nope"); err == nil {
		t.Error("an unknown token resolved to a seat")
	}
	if _, err := st.GetStudentByToken(""); err == nil {
		t.Error("an empty token resolved to a seat")
	}
}
