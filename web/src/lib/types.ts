// Shapes from CONTRACT.md. The server is authoritative; nothing here invents a
// field and nothing here narrows an enum — states, alert kinds and severities
// arrive at runtime from GET /api/vocabularies and are carried as plain strings.

export type UnderstandingState = string
export type AlertKind = string
export type Severity = string
export type SessionStatus = string

export interface Vocabularies {
  understanding_states: UnderstandingState[]
  alert_kinds: AlertKind[]
  severities: Severity[]
  session_statuses: SessionStatus[]
}

export interface Health {
  status: string
  agent_ready: boolean
  agent_error: string
}

export interface Preset {
  id: string
  title: string
  subject: string
  goals: string[]
  labels: string[]
}

export interface Session {
  id: string
  title: string
  subject: string
  status: SessionStatus
  created_at: string
  started_at?: string
  ended_at?: string
}

export interface Goal {
  id: string
  session_id: string
  ordinal: number
  text: string
  short_label: string
}

export interface Team {
  id: string
  session_id: string
  ordinal: number
  name: string
}

export interface Student {
  id: string
  session_id: string
  team_id: string
  name: string
  is_human: boolean
  joined_at?: string
}

export interface Assessment {
  student_id: string
  goal_id: string
  state: UnderstandingState
  confidence: number
  evidence: string
  updated_at: string
}

export interface Alert {
  id: string
  session_id: string
  team_id: string
  student_id?: string
  goal_id?: string
  kind: AlertKind
  severity: Severity
  title: string
  detail: string
  quote?: string
  at: string
  resolved: boolean
}

export interface Misconception {
  id: string
  session_id: string
  goal_id: string
  text: string
  team_ids: string[]
  count: number
  first_seen: string
  last_seen: string
}

export interface GoalRollup {
  goal_id: string
  ordinal: number
  short_label: string
  counts: Record<UnderstandingState, number>
  assessed: number
  roster: number
  /** -1 means "not yet assessed". It is NOT zero. */
  understood_pct: number
  teams_with_no_understanding: string[]
}

export interface TeamGoalCell {
  team_id: string
  goal_id: string
  counts: Record<UnderstandingState, number>
  assessed: number
  roster: number
  any_understands: boolean
}

export interface RunnerStatus {
  running: boolean
  paused: boolean
  rounds: number
  last_round_at?: string
  last_error?: string
  script_left: number
}

export interface SessionState {
  session: Session
  goals: Goal[]
  teams: Team[]
  students: Student[]
  assessments: Assessment[]
  alerts: Alert[]
  misconceptions: Misconception[]
  goal_rollups: GoalRollup[]
  team_goal_cells: TeamGoalCell[]
  runner: RunnerStatus | null
  agent_ready: boolean
  agent_error?: string
}

export interface Message {
  id: string
  session_id: string
  team_id: string
  student_id: string
  seq: number
  body: string
  at: string
}

export interface PerStateScore {
  truth_count: number
  caught: number
  recall: number
  predict_count: number
  precision: number
}

export interface Accuracy {
  scored: number
  truth_pairs: number
  correct: number
  exact_pct: number
  adjacent_pct: number
  confusion: Record<UnderstandingState, Record<UnderstandingState, number>>
  per_state: Record<UnderstandingState, PerStateScore>
}

export interface TruthCell {
  student_id: string
  goal_id: string
  state: UnderstandingState
}

/** A joinable breakout room. Joining ADDS a student; it never takes a seat over. */
export interface Room {
  team_id: string
  team_name: string
  members: string[]
  humans: number
}

export interface JoinResult {
  student: Student
  token: string
}

export interface CreateSessionResult {
  session: Session
  goals: Goal[]
  teams: Team[]
  students: Student[]
  planted: string[]
}

export type StreamEvent =
  | { type: 'message'; data: Message }
  | { type: 'alert'; data: Alert }
  | { type: 'assessments'; data: { round: number } }
  | { type: 'monitor'; data: { round: number; teams_checked: number; duration_ms: number; error?: string } }
  | { type: 'session'; data: { status: SessionStatus; error?: string } }
