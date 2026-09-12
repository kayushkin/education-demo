# Breakout — live group monitoring for teachers

**https://kayushkin.com/education-demo**

A teacher splits 30 students into 10 breakout teams and can be in one room at a time.
For the other nine she is guessing. This watches all ten and tells her where to walk.

An agent reads each group's conversation live and reports four things:

- **Who is confidently wrong.** A student explaining a goal incorrectly to teammates who
  do not know better. The error is spreading, and this is the most urgent thing in the room.
- **Which groups are stuck.** A goal no member of a group understands: they cannot teach
  themselves out of it, so nothing happens until an adult arrives.
- **Where the friction is.** Interpersonal conflict impeding the work — as distinct from
  disagreement about the material, which is usually healthy and is not flagged.
- **Who has gone quiet.** A student contributing nothing while the group works around them.

Around those it keeps a per-goal understanding grid for every student, class-wide percentages
per goal, and a collected list of what the class is getting wrong — the artifact a teacher
reteaches from next lesson.

## The lesson actually goes somewhere

A session runs in **three acts**, and understanding is advanced between them by a rule about
the *group*, not the individual: what decides whether you learn a goal is not where you
started but whether anybody sitting with you can explain it. One person who understands pulls
the rest up and corrects wrong ideas. Two people who half-grasp it can piece it together,
slower, and are much worse at catching an error. Nobody who can do either, and the group
stalls.

Measured over many simulated classes:

| | start | end |
|---|---|---|
| understand the goal | 34% | **67%** |
| hold it wrongly | 8% | **5%** |

and about 69% of team-goals improve, while ~15% of teams are still stuck on some goal at the
end — which is exactly who the teacher needed to reach.

**Misunderstanding is rare, and rare by construction rather than by tuning a dial.** It is the
least likely state to start in, and it usually gets corrected, because correcting it only needs
one person in the group who can explain and with three to a table that is usually true. It
*spreads* only in one specific climate: nobody can explain the goal, fewer than two people
half-grasp it, and somebody confidently wrong is filling the silence. That is the lone
misinformer, it happens in about 6% of team-goal phases, and it is the single thing this tool
exists to catch.

`GET /api/sessions/{id}/progress` reports the whole arc: per goal and per student, beginning
against end.

## The part that makes it more than a demo

The classroom is simulated: each student carries a hidden true understanding of each goal —
`understands`, `partial`, `unknown`, or `misunderstands` — and their dialogue is written to
reveal that state without stating it. The monitoring agent never sees it.

Which means **the agent can be scored**. `GET /api/sessions/{id}/accuracy` compares every
inference against the truth it was trying to recover. Most demos of this kind ask you to
take their word for it; this one shows the confusion matrix.

Measured on a 30-student, 10-team, 5-goal session (Forces and Motion), one monitoring round:

| | |
|---|---|
| Coverage | 139 / 150 student-goal pairs |
| Exact agreement | **82.7%** |
| Within one state | 95.0% |
| **Recall on `misunderstands`** | **100%** (18 of 18) |
| Precision on `misunderstands` | 62% |

The headline is the recall: it caught every student who genuinely held a wrong idea. The
precision says how it pays for that — it over-flags some `partial` and `unknown` students as
misunderstanding. For a tool whose job is to stop an error spreading, erring that way is the
right direction, but it is a real cost and the panel shows it rather than hiding it.

A real classroom has no ground truth, so none of this would be computable there. The accuracy
panel is honest about being an artifact of the simulation.

## Real people can join

Anyone can join a team at `/education-demo/join`. They get the team's live transcript and a
message box, and the agent judges them by exactly the same path as a simulated student.

A person joins as a **new** member and the team grows by one. An earlier version let them take
over a simulated seat, and the agent then assessed the newcomer using the previous occupant's
words — that is a measured bug, not a hypothetical. A human carries no ground truth, so they
are excluded from the accuracy panel: nobody knows what a real person actually understands.

Point a judge at a team, have them explain a goal wrong on purpose, and watch the teacher's
dashboard raise a critical alert about them within a round.

## Running it

```bash
go build -o education-demo-server ./cmd/education-demo-server
cd web && npm install && npm run build && cd ..
./education-demo-server -web web/dist
```

Deploy to kayushkin.com/education-demo with `./deploy.sh` — it builds both halves, checks the
front end's asset paths carry the prefix, installs the systemd unit, adds the nginx location
if missing, and refuses to finish unless the public URL answers.

### Flags

| Flag | Default | |
|---|---|---|
| `-addr` | `127.0.0.1:8316` | listen address |
| `-db` | `~/.config/education-demo/education-demo.db` | SQLite path |
| `-web` | *(none)* | built front end; omit to serve the API only |
| `-base-path` | *(none)* | path prefix, `/education-demo` in production |
| `-bridge-url` | `http://localhost:8160` | llm-bridge-server |
| `-instance` | `inst-cc-local` | llm-bridge instance for model calls |
| `-monitor-interval` | `25s` | gap between assessment rounds |

## How it works

```
POST /sessions          seed 10 teams, 30 students, draw hidden truth for ACT 1,
                        then advance it through acts 2 and 3 by the peer-learning rule
POST /sessions/{id}/start
      │
      ├─ 10 parallel calls ──→ one 3-act transcript per team, written FROM all three acts'
      │                        truth, so the LEARNING happens on the page: the model is told
      │                        which states changed and must show why
      │                        (60-120s; the session reports "starting" immediately)
      │
      ├─ player   ──→ drips each script into its room in real time, one goroutine per team.
      │               Playing a line from a later act advances the session into it, so the
      │               TRANSCRIPT decides when the class has moved on, not a timer.
      │
      └─ monitor  ──→ every 25s, one call per team that has moved, over its recent transcript
                      → assessments, alerts, misconceptions  → SSE → dashboard
```

Every model call goes through **llm-bridge-server's `/oneshot`**, which runs on the Claude Code
subscription. This process reads no API key and never talks to an LLM provider directly.

### Deliberate choices

**One planted scenario, not several.** A five-minute demo needs to reliably contain the alert
the tool is for, but planting several would make the class look full of them when it is not. So
`Build` forces exactly one lone misinformer — one group, one goal, one confidently wrong
student nobody there can correct — and returns it in `planted[]` rather than letting it look
like the agent found something the simulation did not put there. Everything else is the
learning rule running on its own, and it produces its own cases at about 6%.

**The progress view is reported twice and the two halves are never merged.** `observed` is the
agent's first opinion against its current one — what the tool would report in a real classroom,
and the honest headline. `actual` is the simulation's phase-1 truth against now: exact, and
available only because the students are synthetic. The gap between them is how much to trust
the first one.

**Coverage growing is not learning.** The progress view compares only student-goal pairs the
agent had an opinion about at *both* ends. Counting a pair it has just started hearing about
would report a class improving when the only thing that improved was the microphone. Pinned by
`TestCoverageGrowthIsNotLearning`.

**Silence is `unknown`, never `misunderstands`.** A student who knows they do not know asks a
question. A student who misunderstands teaches the error to their team. Collapsing the two
would destroy the single most valuable alert the tool raises, so the prompt, the vocabulary
and the scoring all keep them apart.

**A missing assessment is not `unknown`.** The agent only judges a student on a goal it has
heard evidence about. An absent row is the agent declining to assert; `unknown` is the agent
asserting. The dashboard renders them differently, and `understood_pct` is `-1` — "not yet
assessed" — rather than `0` for a goal the class has not reached.

**Degrading loudly.** If llm-bridge is unreachable the service still boots, still runs sessions
on deterministic fallback transcripts, and still reports disengagement — which is countable
from message volume. It writes **no understanding assessments at all**, because comprehension
is not derivable from who talked most, and a keyword-guessed grid that looks like real
assessment is worse than an empty one. `agent_ready` says which mode you are in, on the health
endpoint and in the UI.

**Alerts dedupe.** A group stuck on a goal for ten minutes is one thing the teacher should see
once. Alerts collapse on `(kind, team, student, goal)` while unresolved, and may be raised
again after she dismisses one.

## Layout

| Path | |
|---|---|
| `internal/model` | domain types and the vocabularies, served at `/api/vocabularies` |
| `internal/store` | SQLite. `_txlock=immediate` is load-bearing — see the concurrency test |
| `internal/simulation` | roster, hidden truth, the peer-learning rule (`learning.go`), transcript writing, playback |
| `internal/monitor` | the watching agent, and the model-free participation fallback |
| `internal/analytics` | per-goal rollups, beginning-vs-end progress, and the accuracy scoring |
| `internal/server` | HTTP, SSE hub, session runner |
| `web` | React 19 + TypeScript + Vite |

`CONTRACT.md` is the route table. `go test ./...` covers the scoring edge cases, the sequence
race, alert dedupe, and what happens to a seat when a human claims it.
