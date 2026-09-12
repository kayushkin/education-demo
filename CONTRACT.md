# education-demo — route table

Base path in production is `/education-demo`; every route below is relative to it.
In development the server runs at the root, so the same paths work with no prefix.

All responses are JSON. Errors carry `{"reason": "<machine_slug>", "error": "<human sentence>"}`.
Render `error` verbatim — the server says what went wrong and the UI must not reword it.

## Meta

| Method | Path | Notes |
|---|---|---|
| `GET` | `/api/health` | `{status, agent_ready, agent_error}`. `agent_ready=false` means no model path: sessions still run on fallback transcripts with participation-only monitoring. |
| `GET` | `/api/vocabularies` | `{understanding_states, alert_kinds, severities, session_statuses}`. **The UI must render from this, never from its own copy.** `understanding_states` is ordered worst-to-best. |
| `GET` | `/api/presets` | Ready-made lessons: `[{id, title, subject, goals[], labels[]}]`. |

## Session lifecycle

| Method | Path | Notes |
|---|---|---|
| `POST` | `/api/sessions` | Body `{preset_id?, title?, subject?, goals[]?, goal_labels[]?, team_count=10, class_size=30, seed?, plant_drama=true, phase_count=3}`. Either `preset_id` or `goals` + `title`. Returns `201 {session, goals, teams, students, planted[], phase_count}`. `planted` names the ONE scenario forced into the ground truth — a lone misinformer — so nobody reads the agent finding it as the agent finding something nobody put there. Show it, do not hide it. |
| `GET` | `/api/sessions` | List, newest first. |
| `GET` | `/api/sessions/{id}` | **The dashboard's single read.** See `sessionState` below. |
| `POST` | `/api/sessions/{id}/start` | Body `{speed?}` (1 = realistic pacing, 2 = twice as fast). Returns `202` immediately; transcript generation takes 30–90s in the background. Watch the SSE stream for `session:{status:"running"}`. |
| `POST` | `/api/sessions/{id}/pause` / `/resume` | `409 not_running` when there is no live runner. |
| `POST` | `/api/sessions/{id}/end` | Stops playback and monitoring for good. |
| `POST` | `/api/sessions/{id}/assess` | Forces a monitoring round now instead of waiting for the tick. Blocking, up to ~60s. |

### `sessionState` — `GET /api/sessions/{id}`

```
{
  session: {id, title, subject, status, created_at, started_at?, ended_at?},
  goals:   [{id, session_id, ordinal, text, short_label}],
  teams:   [{id, session_id, ordinal, name}],
  students:[{id, session_id, team_id, name, is_human, joined_at?}],
  assessments: [{student_id, goal_id, state, confidence, evidence, updated_at}],
  alerts:  [{id, team_id, student_id?, goal_id?, kind, severity, title, detail, quote?, at, resolved}],
  misconceptions: [{id, goal_id, text, team_ids[], count, first_seen, last_seen}],
  goal_rollups:   [{goal_id, ordinal, short_label, counts{state:n}, assessed, roster,
                    understood_pct, teams_with_no_understanding[]}],
  team_goal_cells:[{team_id, goal_id, counts{state:n}, assessed, roster, any_understands}],
  runner: {running, paused, rounds, last_round_at, last_error, script_left} | null,
  agent_ready: bool, agent_error?: string
}
```

⚠️ **`understood_pct` is `-1`, not `0`, when nothing has been assessed yet.** A goal the class
has not reached is not a goal scoring zero. Render `-1` as "not yet assessed" — showing it as
0% tells the teacher to intervene on a topic nobody has opened.

⚠️ **`assessed` < `roster` is coverage, not failure.** The agent only judges a student on a goal
it has heard evidence about. Do not treat the gap as "unknown" — `unknown` is a state the agent
asserts, and an absent row is the agent declining to assert anything.

## Progress — what changed over the lesson

**A lesson runs in phases (3 by default) and understanding is advanced between them**, so the
end of a session genuinely differs from its beginning. `session.current_phase` is the act being
played now; the transcript advances it, not a timer.

| Method | Path | Notes |
|---|---|---|
| `GET` | `/api/sessions/{id}/progress` | Beginning vs end, twice over. |

```
{
  phases_elapsed, phase_count,
  observed: ProgressView,          // the agent's first opinion vs its current one
  actual:   ProgressView | absent  // the simulation's phase-1 truth vs now
}

ProgressView = {
  source: "observed" | "actual",
  class:    StateDelta,
  goals:    [{goal_id, ordinal, short_label, delta: StateDelta}],
  students: [{student_id, name, team_id, is_human, delta: StateDelta}],
  goals_improved, goals_declined, goals_flat
}

StateDelta = {
  before: {state: n}, after: {state: n}, pairs,
  understood_pct_before, understood_pct_after,
  misunderstood_pct_before, misunderstood_pct_after,
  improved, declined, unchanged
}
```

⚠️ **`observed` and `actual` are two different measurements and must never be merged or
averaged.** `observed` is what the tool would report in a real classroom, where no ground truth
exists — it is the honest headline. `actual` is exact and available *only* because the students
are synthetic. The gap between them is how much to trust the first one, so show both.

⚠️ **`before` and `after` cover the SAME pairs.** A pair the agent has an opinion about now but
had none about at the start is excluded. Counting it would report a class improving when the
only thing that improved was the agent's coverage — pinned by
`TestCoverageGrowthIsNotLearning`.

⚠️ `understood_pct_*` is `-1` for an unmeasured population, never `0`. Same rule as the rest of
the dashboard.

## Accuracy — the demo's proof

| Method | Path | Notes |
|---|---|---|
| `GET` | `/api/sessions/{id}/accuracy` | Scores the agent against the simulation's hidden ground truth. |
| `GET` | `/api/sessions/{id}/truth` | `[{student_id, goal_id, phase, state}]` — the hidden variable, revealed, **one row per phase**. |

```
{scored, truth_pairs, correct, exact_pct, adjacent_pct,
 confusion: {<true_state>: {<inferred_state>: n}},
 per_state: {<state>: {truth_count, caught, recall, predict_count, precision}}}
```

`scored` counts pairs where both a truth and an assessment exist; `truth_pairs` is every pair
that has a truth at all, so `scored / truth_pairs` is coverage. **A human who joins has no
ground truth and is excluded from scoring** — nobody knows what a real person understands.

`per_state["misunderstands"].recall` is the headline number: the fraction of genuinely confused
students the agent actually caught.

⚠️ **Accuracy scores the agent's latest opinion against the CURRENT phase's truth**, not phase
1's. Judging a fresh assessment against where a student started would mark the agent wrong for
correctly noticing that somebody has since learned something.

Both routes exist **only because the students are synthetic**. Label them as such in the UI.

## Breakout rooms — where a real person joins

| Method | Path | Notes |
|---|---|---|
| `GET` | `/api/sessions/{id}/rooms` | `[{team_id, team_name, members[], humans}]` — the teams you can join and who is in them. |
| `POST` | `/api/sessions/{id}/join` | Body `{display_name, team_id?}`. Returns `201 {student, token}`. **Keep the token** — it authenticates every message; persist it in localStorage. Omitting `team_id` puts the person in the smallest team. `409 name_taken` when that name is already in the room, `404 unknown_team`, `400 name_too_long` (40 chars). |

⚠️ **Joining ADDS a student to the team; it does not take over a simulated one.** The team grows
by one. An earlier design renamed an existing seat, and the agent then assessed the newcomer
using the previous occupant's words — measured, and the reason it works this way now. A human
has no ground truth and is excluded from the accuracy panel.
| `GET` | `/api/sessions/{id}/teams/{teamID}/messages` | Last 200, oldest first. |
| `POST` | `/api/sessions/{id}/teams/{teamID}/messages` | Body `{token, body}`. `401 unknown_token`, `403 wrong_room`. A human's message is stored exactly like a scripted one and judged by the same path. |
| `POST` | `/api/alerts/{alertID}/resolve` | Teacher dismisses an alert. |

## Live stream

`GET /api/sessions/{id}/stream` — Server-Sent Events, one JSON object per `data:` line:

| `type` | `data` | What the client should do |
|---|---|---|
| `message` | the full `Message` | Append to that team's room. |
| `alert` | the full `Alert` | Prepend to the alert feed; only ever sent once per standing problem. |
| `assessments` | `{round}` | **Refetch `GET /api/sessions/{id}`** — the grid changed. The matrix is not pushed down the wire. |
| `monitor` | `{round, teams_checked, duration_ms, error}` | Update the engine status line. |
| `session` | `{status, error?}` | Session moved to running / paused / ended / error. |

A slow subscriber is dropped rather than allowed to block the simulation; `EventSource`
reconnects on its own, so refetch state on reconnect.
