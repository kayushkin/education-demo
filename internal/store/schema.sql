PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS sessions (
  id          TEXT PRIMARY KEY,
  title       TEXT NOT NULL,
  subject     TEXT NOT NULL DEFAULT '',
  status      TEXT NOT NULL,
  created_at  INTEGER NOT NULL,
  started_at  INTEGER,
  ended_at    INTEGER,
  -- seq is the session's monotonic message counter, bumped inside the same
  -- transaction that inserts a message so two concurrent senders cannot be
  -- handed the same number.
  seq         INTEGER NOT NULL DEFAULT 0,
  -- current_phase is the segment of the lesson being played right now, and
  -- phase_count how many there are. Understanding is advanced between phases,
  -- so these are what make the end of a session differ from its beginning.
  current_phase INTEGER NOT NULL DEFAULT 1,
  phase_count   INTEGER NOT NULL DEFAULT 3
);

CREATE TABLE IF NOT EXISTS goals (
  id          TEXT PRIMARY KEY,
  session_id  TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  ordinal     INTEGER NOT NULL,
  text        TEXT NOT NULL,
  short_label TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_goals_session ON goals(session_id, ordinal);

CREATE TABLE IF NOT EXISTS teams (
  id          TEXT PRIMARY KEY,
  session_id  TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  ordinal     INTEGER NOT NULL,
  name        TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_teams_session ON teams(session_id, ordinal);

CREATE TABLE IF NOT EXISTS students (
  id          TEXT PRIMARY KEY,
  session_id  TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  team_id     TEXT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  is_human    INTEGER NOT NULL DEFAULT 0,
  join_token  TEXT NOT NULL DEFAULT '',
  joined_at   INTEGER
);
CREATE INDEX IF NOT EXISTS idx_students_session ON students(session_id);
CREATE INDEX IF NOT EXISTS idx_students_team ON students(team_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_students_token
  ON students(join_token) WHERE join_token != '';

-- Ground truth for simulated students only, ONE ROW PER PHASE. A human seat has
-- no row here, and the accuracy panel scores exactly the rows this table holds.
--
-- Keeping every phase rather than overwriting is the whole basis of the
-- progress view: it is the only exact record of what each student understood
-- early and late, and the agent never sees any of it.
CREATE TABLE IF NOT EXISTS truths (
  student_id  TEXT NOT NULL REFERENCES students(id) ON DELETE CASCADE,
  goal_id     TEXT NOT NULL REFERENCES goals(id) ON DELETE CASCADE,
  phase       INTEGER NOT NULL,
  state       TEXT NOT NULL,
  PRIMARY KEY (student_id, goal_id, phase)
);
CREATE INDEX IF NOT EXISTS idx_truths_phase ON truths(phase);

CREATE TABLE IF NOT EXISTS messages (
  id          TEXT PRIMARY KEY,
  session_id  TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  team_id     TEXT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  student_id  TEXT NOT NULL REFERENCES students(id) ON DELETE CASCADE,
  seq         INTEGER NOT NULL,
  body        TEXT NOT NULL,
  at          INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_team_seq ON messages(team_id, seq);
CREATE INDEX IF NOT EXISTS idx_messages_session_seq ON messages(session_id, seq);

-- The scripted transcript the simulation drips into a team room. Rows are
-- consumed in ordinal order; played_at stamps the ones already sent so a
-- restart resumes rather than replaying the session from the top.
CREATE TABLE IF NOT EXISTS scripted_lines (
  id          TEXT PRIMARY KEY,
  session_id  TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  team_id     TEXT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  student_id  TEXT NOT NULL REFERENCES students(id) ON DELETE CASCADE,
  ordinal     INTEGER NOT NULL,
  body        TEXT NOT NULL,
  gap_ms      INTEGER NOT NULL DEFAULT 4000,
  -- Which segment of the lesson this line belongs to. Playing a line advances
  -- the session into its phase, which is how the transcript itself -- not a
  -- timer -- decides when the class has moved on.
  phase       INTEGER NOT NULL DEFAULT 1,
  played_at   INTEGER
);
CREATE INDEX IF NOT EXISTS idx_script_team ON scripted_lines(team_id, ordinal);

CREATE TABLE IF NOT EXISTS assessments (
  session_id  TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  student_id  TEXT NOT NULL REFERENCES students(id) ON DELETE CASCADE,
  goal_id     TEXT NOT NULL REFERENCES goals(id) ON DELETE CASCADE,
  state       TEXT NOT NULL,
  confidence  REAL NOT NULL DEFAULT 0,
  evidence    TEXT NOT NULL DEFAULT '',
  updated_at  INTEGER NOT NULL,
  PRIMARY KEY (student_id, goal_id)
);
CREATE INDEX IF NOT EXISTS idx_assessments_session ON assessments(session_id);

CREATE TABLE IF NOT EXISTS alerts (
  id          TEXT PRIMARY KEY,
  session_id  TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  team_id     TEXT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  student_id  TEXT NOT NULL DEFAULT '',
  goal_id     TEXT NOT NULL DEFAULT '',
  kind        TEXT NOT NULL,
  severity    TEXT NOT NULL,
  title       TEXT NOT NULL,
  detail      TEXT NOT NULL DEFAULT '',
  quote       TEXT NOT NULL DEFAULT '',
  at          INTEGER NOT NULL,
  resolved    INTEGER NOT NULL DEFAULT 0,
  -- dedupe_key collapses the same standing problem re-reported on every
  -- monitoring round into one row. Without it a team that misunderstands a
  -- goal for ten minutes buries the feed in thirty identical alerts.
  dedupe_key  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_alerts_session_at ON alerts(session_id, at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_alerts_dedupe
  ON alerts(session_id, dedupe_key) WHERE dedupe_key != '' AND resolved = 0;

CREATE TABLE IF NOT EXISTS misconceptions (
  id          TEXT PRIMARY KEY,
  session_id  TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  goal_id     TEXT NOT NULL REFERENCES goals(id) ON DELETE CASCADE,
  text        TEXT NOT NULL,
  norm_key    TEXT NOT NULL,
  count       INTEGER NOT NULL DEFAULT 1,
  first_seen  INTEGER NOT NULL,
  last_seen   INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_misconceptions_key
  ON misconceptions(session_id, goal_id, norm_key);

CREATE TABLE IF NOT EXISTS misconception_teams (
  misconception_id TEXT NOT NULL REFERENCES misconceptions(id) ON DELETE CASCADE,
  team_id          TEXT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  PRIMARY KEY (misconception_id, team_id)
);

-- Watermark of how far the monitoring agent has read in each team room, so a
-- round only pays for messages it has not seen.
CREATE TABLE IF NOT EXISTS monitor_cursors (
  team_id     TEXT PRIMARY KEY REFERENCES teams(id) ON DELETE CASCADE,
  last_seq    INTEGER NOT NULL DEFAULT 0,
  ran_at      INTEGER NOT NULL DEFAULT 0,
  last_error  TEXT NOT NULL DEFAULT ''
);

-- Every change the agent made to its mind, appended.
--
-- `assessments` is upsert-only and keeps just the latest opinion, which cannot
-- answer "what did the agent think at the start". This can: the earliest row
-- per (student, goal) is the agent's first read and the latest is its current
-- one, and the difference is the learning it observed.
CREATE TABLE IF NOT EXISTS assessment_history (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id  TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  student_id  TEXT NOT NULL REFERENCES students(id) ON DELETE CASCADE,
  goal_id     TEXT NOT NULL REFERENCES goals(id) ON DELETE CASCADE,
  state       TEXT NOT NULL,
  confidence  REAL NOT NULL DEFAULT 0,
  phase       INTEGER NOT NULL DEFAULT 1,
  at          INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_history_pair
  ON assessment_history(session_id, student_id, goal_id, id);
