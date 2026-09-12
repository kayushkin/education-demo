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

// TestHumanJoinsAsNewStudent pins that a real person is added rather than
// substituted into a simulated seat.
//
// The bug this guards against was measured: a human who took over a seat was
// assessed on three goals using the previous occupant's words, because the
// seat's transcript history followed the rename. A new row cannot do that.
func TestHumanJoinsAsNewStudent(t *testing.T) {
	st := newTestStore(t)
	sessionID, teamID, simID, goalID := seedSession(t, st)

	if err := st.SetTruth(model.Truth{
		StudentID: simID, GoalID: goalID, State: model.StateUnderstands,
	}); err != nil {
		t.Fatalf("set truth: %v", err)
	}
	if err := st.InsertScriptedLines([]ScriptedLine{{
		ID: uuid.NewString(), SessionID: sessionID, TeamID: teamID,
		StudentID: simID, Ordinal: 1, Body: "scripted", GapMs: 1000,
	}}); err != nil {
		t.Fatalf("insert script: %v", err)
	}

	human := &model.Student{
		ID: uuid.NewString(), SessionID: sessionID, TeamID: teamID,
		Name: "Judge Alice", JoinToken: "tok-human",
	}
	if err := st.AddHumanStudent(human); err != nil {
		t.Fatalf("add human: %v", err)
	}

	// The simulated student is untouched: same name, still simulated, script
	// and ground truth intact.
	sim, err := st.GetStudent(simID)
	if err != nil {
		t.Fatalf("get simulated student: %v", err)
	}
	if sim.IsHuman || sim.Name != "Amara" {
		t.Errorf("simulated seat was altered: is_human=%v name=%q", sim.IsHuman, sim.Name)
	}
	line, err := st.NextScriptedLine(teamID)
	if err != nil {
		t.Fatalf("next line: %v", err)
	}
	if line == nil {
		t.Error("the simulated student's script was cancelled by someone else joining")
	}
	truths, err := st.ListTruths(sessionID)
	if err != nil {
		t.Fatalf("list truths: %v", err)
	}
	if len(truths) != 1 {
		t.Errorf("ground truth rows = %d, want 1 (the human adds none)", len(truths))
	}

	// The human is present, marked, and has no ground truth of their own —
	// nobody knows what a real person understands.
	joined, err := st.GetStudent(human.ID)
	if err != nil {
		t.Fatalf("get human: %v", err)
	}
	if !joined.IsHuman || joined.Name != "Judge Alice" {
		t.Errorf("human not seated correctly: is_human=%v name=%q", joined.IsHuman, joined.Name)
	}
	if joined.JoinedAt == nil {
		t.Error("joined_at was not stamped")
	}
	for _, tr := range truths {
		if tr.StudentID == human.ID {
			t.Error("a human was given ground truth")
		}
	}

	roster, err := st.ListTeamStudents(teamID)
	if err != nil {
		t.Fatalf("list team: %v", err)
	}
	if len(roster) != 2 {
		t.Errorf("team size = %d, want 2 (the team grew by one)", len(roster))
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

// TestTranscriptTailIsOldestFirst pins the ordering the monitor depends on.
//
// ListTeamMessagesTail takes the NEWEST n rows and must hand them back
// oldest-first. Get this backwards and the model reads every conversation in
// reverse — an answer before its question — and silently misjudges who
// corrected whom. It cannot fail loudly, so it is pinned here.
func TestTranscriptTailIsOldestFirst(t *testing.T) {
	st := newTestStore(t)
	sessionID, teamID, studentID, _ := seedSession(t, st)

	for i := 0; i < 10; i++ {
		if err := st.AppendMessage(&model.Message{
			ID: uuid.NewString(), SessionID: sessionID, TeamID: teamID,
			StudentID: studentID, Body: string(rune('0' + i)), At: time.Now(),
		}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	// Ask for the last four of ten.
	tail, err := st.ListTeamMessagesTail(teamID, 4)
	if err != nil {
		t.Fatalf("tail: %v", err)
	}
	if len(tail) != 4 {
		t.Fatalf("got %d messages, want 4", len(tail))
	}
	if got, want := tail[0].Body, "6"; got != want {
		t.Errorf("first of tail = %q, want %q (the oldest of the newest four)", got, want)
	}
	if got, want := tail[3].Body, "9"; got != want {
		t.Errorf("last of tail = %q, want %q (the newest)", got, want)
	}
	for i := 1; i < len(tail); i++ {
		if tail[i].Seq <= tail[i-1].Seq {
			t.Fatalf("tail is not ascending by seq: %d then %d", tail[i-1].Seq, tail[i].Seq)
		}
	}
}

// TestScriptedLineCountsAnswerDifferentQuestions pins the distinction that a
// resumed session turns on.
//
// "Has a transcript ever been written?" and "is there any left to play?" are
// different questions, and a fully-played session answers 0 to the second
// while answering yes to the first. Conflating them made a restart hand a
// finished lesson a whole second transcript — measured on the live service.
func TestScriptedLineCountsAnswerDifferentQuestions(t *testing.T) {
	st := newTestStore(t)
	sessionID, teamID, studentID, _ := seedSession(t, st)

	// Nothing written yet: both counts agree there is no script.
	written, err := st.CountScriptedLines(sessionID)
	if err != nil {
		t.Fatalf("count written: %v", err)
	}
	unplayed, err := st.CountUnplayedLines(sessionID)
	if err != nil {
		t.Fatalf("count unplayed: %v", err)
	}
	if written != 0 || unplayed != 0 {
		t.Fatalf("fresh session: written=%d unplayed=%d, want 0 and 0", written, unplayed)
	}

	lineID := uuid.NewString()
	if err := st.InsertScriptedLines([]ScriptedLine{{
		ID: lineID, SessionID: sessionID, TeamID: teamID,
		StudentID: studentID, Ordinal: 1, Body: "line", GapMs: 1000,
	}}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := st.MarkLinePlayed(lineID); err != nil {
		t.Fatalf("mark played: %v", err)
	}

	// Fully played: nothing left to play, but a transcript certainly exists.
	written, err = st.CountScriptedLines(sessionID)
	if err != nil {
		t.Fatalf("count written: %v", err)
	}
	unplayed, err = st.CountUnplayedLines(sessionID)
	if err != nil {
		t.Fatalf("count unplayed: %v", err)
	}
	if unplayed != 0 {
		t.Errorf("unplayed = %d after the only line played, want 0", unplayed)
	}
	if written != 1 {
		t.Errorf("written = %d, want 1 — a played line is still a line that was written", written)
	}
}

// TestDeleteSessionRemovesEverythingItOwns pins that deleting a session really
// empties it rather than orphaning its rows.
//
// The delete is a single statement relying entirely on ON DELETE CASCADE, and
// SQLite enforces that only when the foreign_keys pragma is on. With it off
// the statement still succeeds and leaves every goal, student, message and
// assessment behind — a database that looks emptied and is not. This checks
// the rows are actually gone, table by table.
func TestDeleteSessionRemovesEverythingItOwns(t *testing.T) {
	st := newTestStore(t)
	sessionID, teamID, studentID, goalID := seedSession(t, st)

	if err := st.SetTruth(model.Truth{
		StudentID: studentID, GoalID: goalID, Phase: 1, State: model.StateUnderstands,
	}); err != nil {
		t.Fatalf("seed truth: %v", err)
	}
	if err := st.AppendMessage(&model.Message{
		ID: uuid.NewString(), SessionID: sessionID, TeamID: teamID,
		StudentID: studentID, Body: "hello", At: time.Now(),
	}); err != nil {
		t.Fatalf("seed message: %v", err)
	}
	if err := st.InsertScriptedLines([]ScriptedLine{{
		ID: uuid.NewString(), SessionID: sessionID, TeamID: teamID,
		StudentID: studentID, Ordinal: 1, Body: "scripted", GapMs: 1000, Phase: 1,
	}}); err != nil {
		t.Fatalf("seed script: %v", err)
	}
	if err := st.UpsertAssessment(model.Assessment{
		SessionID: sessionID, StudentID: studentID, GoalID: goalID,
		State: model.StatePartial, UpdatedAt: time.Now(),
	}, 1); err != nil {
		t.Fatalf("seed assessment: %v", err)
	}
	if _, err := st.InsertAlert(&model.Alert{
		ID: uuid.NewString(), SessionID: sessionID, TeamID: teamID,
		Kind: model.AlertDisengaged, Severity: model.SeverityInfo,
		Title: "quiet", At: time.Now(),
	}, "k"); err != nil {
		t.Fatalf("seed alert: %v", err)
	}
	if err := st.RecordMisconception(&model.Misconception{
		ID: uuid.NewString(), SessionID: sessionID, GoalID: goalID,
		Text: "wrong idea", FirstSeen: time.Now(), LastSeen: time.Now(),
	}, "wrong idea", teamID); err != nil {
		t.Fatalf("seed misconception: %v", err)
	}

	removed, err := st.DeleteSession(sessionID)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !removed {
		t.Fatal("delete reported nothing removed")
	}

	// Every table that hangs off a session must be empty, checked directly
	// rather than through the read paths, which filter by session anyway and
	// would hide an orphan.
	for _, table := range []string{
		"sessions", "goals", "teams", "students", "truths", "messages",
		"scripted_lines", "assessments", "assessment_history", "alerts",
		"misconceptions", "misconception_teams", "monitor_cursors",
	} {
		var n int
		if err := st.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s still holds %d row(s) after the session was deleted", table, n)
		}
	}

	// Deleting it again is a clean "nothing there", not an error.
	removed, err = st.DeleteSession(sessionID)
	if err != nil {
		t.Fatalf("second delete: %v", err)
	}
	if removed {
		t.Error("second delete claimed to remove a session that was already gone")
	}
}
