package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/kayushkin/education-demo/internal/model"
)

// oldSchema is the schema as it shipped BEFORE lessons ran in phases. It is
// pinned here verbatim rather than derived, because the whole point is to
// migrate a database that was created by code we no longer have.
const oldSchema = `
CREATE TABLE sessions (
  id TEXT PRIMARY KEY, title TEXT NOT NULL, subject TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL, created_at INTEGER NOT NULL,
  started_at INTEGER, ended_at INTEGER, seq INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE goals (
  id TEXT PRIMARY KEY, session_id TEXT NOT NULL, ordinal INTEGER NOT NULL,
  text TEXT NOT NULL, short_label TEXT NOT NULL DEFAULT ''
);
CREATE TABLE teams (
  id TEXT PRIMARY KEY, session_id TEXT NOT NULL, ordinal INTEGER NOT NULL, name TEXT NOT NULL
);
CREATE TABLE students (
  id TEXT PRIMARY KEY, session_id TEXT NOT NULL, team_id TEXT NOT NULL, name TEXT NOT NULL,
  is_human INTEGER NOT NULL DEFAULT 0, join_token TEXT NOT NULL DEFAULT '', joined_at INTEGER
);
CREATE TABLE truths (
  student_id TEXT NOT NULL, goal_id TEXT NOT NULL, state TEXT NOT NULL,
  PRIMARY KEY (student_id, goal_id)
);
CREATE TABLE messages (
  id TEXT PRIMARY KEY, session_id TEXT NOT NULL, team_id TEXT NOT NULL,
  student_id TEXT NOT NULL, seq INTEGER NOT NULL, body TEXT NOT NULL, at INTEGER NOT NULL
);
CREATE TABLE scripted_lines (
  id TEXT PRIMARY KEY, session_id TEXT NOT NULL, team_id TEXT NOT NULL,
  student_id TEXT NOT NULL, ordinal INTEGER NOT NULL, body TEXT NOT NULL,
  gap_ms INTEGER NOT NULL DEFAULT 4000, played_at INTEGER
);
CREATE TABLE assessments (
  session_id TEXT NOT NULL, student_id TEXT NOT NULL, goal_id TEXT NOT NULL,
  state TEXT NOT NULL, confidence REAL NOT NULL DEFAULT 0,
  evidence TEXT NOT NULL DEFAULT '', updated_at INTEGER NOT NULL,
  PRIMARY KEY (student_id, goal_id)
);
CREATE TABLE alerts (
  id TEXT PRIMARY KEY, session_id TEXT NOT NULL, team_id TEXT NOT NULL,
  student_id TEXT NOT NULL DEFAULT '', goal_id TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL, severity TEXT NOT NULL, title TEXT NOT NULL,
  detail TEXT NOT NULL DEFAULT '', quote TEXT NOT NULL DEFAULT '',
  at INTEGER NOT NULL, resolved INTEGER NOT NULL DEFAULT 0,
  dedupe_key TEXT NOT NULL DEFAULT ''
);
CREATE TABLE misconceptions (
  id TEXT PRIMARY KEY, session_id TEXT NOT NULL, goal_id TEXT NOT NULL,
  text TEXT NOT NULL, norm_key TEXT NOT NULL, count INTEGER NOT NULL DEFAULT 1,
  first_seen INTEGER NOT NULL, last_seen INTEGER NOT NULL
);
CREATE TABLE misconception_teams (
  misconception_id TEXT NOT NULL, team_id TEXT NOT NULL,
  PRIMARY KEY (misconception_id, team_id)
);
CREATE TABLE monitor_cursors (
  team_id TEXT PRIMARY KEY, last_seq INTEGER NOT NULL DEFAULT 0,
  ran_at INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT ''
);
`

// TestOpeningAPrePhaseDatabaseMigratesIt is the test that would have caught the
// deploy failure.
//
// Every test before it built a FRESH database, where schema.sql creates
// everything and migrations are a no-op — so the entire migration path was
// untested until production, where opening the existing database died with
// "no such column: phase". This builds the old schema deliberately.
func TestOpeningAPrePhaseDatabaseMigratesIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")

	// Stand up a database exactly as the previous version left it, with a row
	// of ground truth in it.
	raw, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	if _, err := raw.Exec(oldSchema); err != nil {
		t.Fatalf("apply old schema: %v", err)
	}
	if _, err := raw.Exec(
		`INSERT INTO sessions (id, title, status, created_at) VALUES ('s1','Old lesson','running',1)`); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO goals (id, session_id, ordinal, text) VALUES ('g1','s1',1,'goal')`); err != nil {
		t.Fatalf("seed goal: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO teams (id, session_id, ordinal, name) VALUES ('t1','s1',1,'Team 1')`); err != nil {
		t.Fatalf("seed team: %v", err)
	}
	if _, err := raw.Exec(
		`INSERT INTO students (id, session_id, team_id, name) VALUES ('st1','s1','t1','Amara')`); err != nil {
		t.Fatalf("seed student: %v", err)
	}
	if _, err := raw.Exec(
		`INSERT INTO truths (student_id, goal_id, state) VALUES ('st1','g1','understands')`); err != nil {
		t.Fatalf("seed truth: %v", err)
	}
	if _, err := raw.Exec(
		`INSERT INTO scripted_lines (id, session_id, team_id, student_id, ordinal, body)
		 VALUES ('l1','s1','t1','st1',1,'hello')`); err != nil {
		t.Fatalf("seed line: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}

	// Opening it with the current code must migrate rather than fail.
	st, err := Open(path)
	if err != nil {
		t.Fatalf("opening a pre-phase database failed: %v", err)
	}
	defer st.Close()

	// The session gained its phase columns with sane defaults.
	sess, err := st.GetSession("s1")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if sess.CurrentPhase != 1 {
		t.Errorf("current_phase = %d, want 1", sess.CurrentPhase)
	}
	if sess.PhaseCount < 1 {
		t.Errorf("phase_count = %d, want at least 1", sess.PhaseCount)
	}

	// The existing ground truth survived, carried over as phase 1 — it was a
	// single frozen snapshot, which is what phase 1 means.
	truths, err := st.ListTruthsAtPhase("s1", 1)
	if err != nil {
		t.Fatalf("list truths: %v", err)
	}
	if len(truths) != 1 {
		t.Fatalf("got %d truth rows after migration, want the 1 that was there", len(truths))
	}
	if truths[0].State != model.StateUnderstands || truths[0].Phase != 1 {
		t.Errorf("migrated truth = %+v, want state=understands phase=1", truths[0])
	}

	// The widened primary key actually took: the same pair may now be stored
	// again for a later phase, which the old key forbade.
	if err := st.SetTruth(model.Truth{
		StudentID: "st1", GoalID: "g1", Phase: 2, State: model.StatePartial,
	}); err != nil {
		t.Fatalf("write phase 2 truth: %v", err)
	}
	phase2, err := st.ListTruthsAtPhase("s1", 2)
	if err != nil {
		t.Fatalf("list phase 2: %v", err)
	}
	if len(phase2) != 1 || phase2[0].State != model.StatePartial {
		t.Errorf("phase 2 truth = %+v, want one row in state partial", phase2)
	}

	// Scripted lines kept their content and defaulted into act 1.
	line, err := st.NextScriptedLine("t1")
	if err != nil {
		t.Fatalf("next line: %v", err)
	}
	if line == nil {
		t.Fatal("the existing scripted line was lost in migration")
	}
	if line.Body != "hello" || line.Phase != 1 {
		t.Errorf("migrated line = %+v, want body=hello phase=1", line)
	}

	// And the monitor cursor's new column is readable.
	if _, _, err := st.MonitorCursor("t1"); err != nil {
		t.Errorf("read migrated monitor cursor: %v", err)
	}
}

// TestMigrationIsIdempotent pins that opening an already-migrated database
// twice is safe — a service restart must not rebuild tables or duplicate rows.
func TestMigrationIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "twice.db")
	for i := 0; i < 3; i++ {
		st, err := Open(path)
		if err != nil {
			t.Fatalf("open %d: %v", i+1, err)
		}
		if err := st.Close(); err != nil {
			t.Fatalf("close %d: %v", i+1, err)
		}
	}
}
