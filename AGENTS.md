# About education-demo

## What it owns

`127.0.0.1:8316`, published at `https://kayushkin.com/education-demo`. Live monitoring of classroom breakout groups: a teacher splits a class (30 students across 10 teams by default) into rooms and can be in one room at a time, while an agent reads all ten transcripts and flags **who is confidently wrong** — a student teaching an error to teammates who cannot correct it, the alert the tool exists for — which groups are **stuck** on a goal nobody in them understands, interpersonal **conflict**, and who has **gone quiet**. It also keeps a per-student per-goal understanding grid, class-wide percentages per goal, and a collected misconception list. `CONTRACT.md` is the route table; `README.md` is the pitch and `DEMO.md` the run-through. Nothing else on this host depends on it.

## Where this prompt lives

These sections are stored in agent-store as a project prompt collection and rendered, with identical text, to `AGENTS.md` and `CLAUDE.md` at the root of this repo, so that whichever file a harness reads it gets the same thing. Edit them on dash `/files`, or edit either rendered file: the 15-minute scan carries the edit back into the sections and out to the other file. The host prompt keeps one row for this repo with only what an agent elsewhere needs.

# How it works

## The classroom is simulated, and that is the point

Each student carries a hidden true understanding per goal — `misunderstands`, `unknown`, `partial` or `understands` — and their dialogue is written from it without stating it. The monitoring agent never sees it, which is what makes the agent **scorable**: `GET /api/sessions/{id}/accuracy` compares every inference against the truth it was trying to recover, and `GET /api/sessions/{id}/truth` reveals the hidden variable, one row per phase. On one measured 30-student, 10-team, 5-goal run (Forces and Motion, one monitoring round): 139 of 150 student-goal pairs covered, 82.7% exact agreement, 95.0% within one state, **100% recall on `misunderstands` (18 of 18)** and 62% precision on it. The recall is the headline and the precision is what it costs — the agent over-flags some `partial` and `unknown` students, which is the safe direction for a tool whose job is to stop an error spreading, and the panel shows the cost rather than hiding it. ⚠️ Accuracy scores the agent's latest opinion against the **current** phase's truth, not phase 1's; judging a fresh assessment against where a student started would mark the agent wrong for correctly noticing that somebody has since learned something. A real classroom has no ground truth, so both routes exist only because the students are synthetic — label them as such in the UI.

## A lesson runs in phases

A session runs in phases (3 by default, `phase_count` on `POST /api/sessions`) and understanding is advanced between them, so the end of a lesson genuinely differs from its beginning. The advance happens one team and one goal at a time (`simulation.AdvanceTeamGoal`), because that is the unit the learning actually happens in — who is sitting with you decides whether you get it. Every phase's truth is frozen as a `PhaseTruth` snapshot, which is why `/truth` returns every phase rather than only the current one, and why accuracy is scored against the current phase.

## `planted[]` names what the simulation forced

`POST /api/sessions` builds with `plant_drama` true and forces **exactly one** situation into the ground truth: a lone misinformer — a group where nobody can explain a goal and one member is confidently wrong about it, so their account is the only one in the room. `POST /api/sessions` returns it in `planted[]` and the UI shows it, so nobody reads the agent finding it as the agent finding something the simulation did not put there. **Only one, and only this one.** The learning rule in `internal/simulation/learning.go` reaches that state on its own in about 6% of team-goal phases, and everywhere else the group teaches itself; an earlier version planted a stuck group and a confident explainer as two separate arrangements, which made both look common when they are not. What planting buys is that a five-minute demo reliably contains one.

## A real person joins by being added, never by taking a seat

`POST /api/sessions/{id}/join` with `{display_name, team_id?}` returns `201 {student, token}`; an omitted `team_id` puts the person in the smallest team. Keep the token — it authenticates every message the person posts. **Joining ADDS a student to the team; it does not take over a simulated one**, and the team grows by one. An earlier design renamed an existing seat, and the agent then assessed the newcomer using the previous occupant's words. A human's message is stored exactly like a scripted one and judged by the same path, but a human carries no ground truth and is **excluded from the accuracy panel** — nobody knows what a real person understands. Refusals: `409 name_taken` (a duplicate display name in one room would make the transcript ambiguous for an agent that resolves speakers by name), `404 unknown_team`, `400 name_too_long` at 40 characters, and on posting a message `401 unknown_token` or `403 wrong_room`.

## Every model call is one oneshot on the subscription

Every model call goes through **llm-bridge-server `POST /instances/{id}/oneshot`** (`internal/agent/client.go`) on the Claude Code subscription: one call per team to write that team's transcript at session start, then one call per team per monitoring round, 25 seconds apart by default. **No API key is read by this process** — grep finds no `ANTHROPIC_API_KEY` and no provider SDK anywhere in the tree. The instance is `-instance` / `EDUCATION_DEMO_INSTANCE`, default `inst-cc-local`, and `-model` / `EDUCATION_DEMO_MODEL` overrides the instance's default model when set.

## It degrades loudly, and `agent_ready` says which mode you are in

With llm-bridge unreachable the service still boots — the unit `Wants` it rather than `Requires` it — and logs a warning. Sessions then run **deterministic fallback transcripts** built from ground truth (`internal/simulation/script.go`, keyed by true state so the dialogue still matches what a student knows) and the monitor reports disengagement, which is countable from message volume, but writes **no understanding assessments at all**: comprehension is not derivable from who talked most, and asserting it from silence would be a guess dressed as a measurement. `agent_ready` (with `agent_error`) on `GET /api/health` and on the session state says which mode you are in. A silent fallback would let a wholly canned demo look like a working one, which is the failure this shape exists to prevent.

## Three things the UI must not round off

`GET /api/vocabularies` serves `understanding_states` (ordered worst to best), `alert_kinds`, `severities` and `session_statuses`; **the UI renders from that, never from its own copy**. `understood_pct` is `-1`, not `0`, for a population nothing has been assessed for — a goal the class has not reached is not a goal scoring zero, and showing it as 0% tells the teacher to intervene on a topic nobody has opened. `assessed` below `roster` is **coverage, not failure**: the agent only judges a student on a goal it has heard evidence about, so an absent row is the agent declining to assert anything, while `unknown` is a state it does assert. Errors carry `{"reason": "<machine_slug>", "error": "<human sentence>"}` and the `error` is rendered verbatim.

# Access and operations

## Unit, address and the bridge service token

Systemd user unit `education-demo.service`, tracked at `systemd/education-demo.service` and installed by `deploy.sh`. It runs `%h/bin/education-demo-server -addr 127.0.0.1:8316 -base-path /education-demo -web %h/repos/education-demo/web/dist -monitor-interval 25s`, and binds **127.0.0.1 only** — nginx is the one way in. `Wants=llm-bridge.service`, deliberately not `Requires`. Environment in the unit: `LLM_BRIDGE_URL`, `EDUCATION_DEMO_INSTANCE`, `EDUCATION_DEMO_DB`. **llm-bridge-server gates its routes and answers an uncredentialed call 401**, so the token comes from the host-local `~/.config/principal-gating-tokens.env` as `LLMBRIDGE_SERVICE_TOKEN`, loaded by the drop-in `education-demo.service.d/principal-gating.conf` — never put it in the unit file, which is tracked in this repo. `bridgeauth.StampRequestsToBridge`, called once early in `main`, installs a process-wide transport that stamps `X-LLM-Bridge-Service-Token` on requests **to the bridge's host only**, so the token is never sent to any other service the binary calls. An unset token is not an error there; the bridge's 401 names the missing header.

## nginx publishes one prefix

`location /education-demo` on the `kayushkin.com` vhost proxies to `127.0.0.1:8316` with the prefix **kept** — no trailing slash on `proxy_pass` — so the Go server sees `/education-demo/...` and its `-base-path` matches. `proxy_buffering off`, `proxy_cache off` and `proxy_read_timeout 3600s` are load-bearing: the dashboard holds an SSE stream open for the whole lesson, which dies at the default 60s read timeout, and buffering holds every event back until a buffer fills, which makes a live feed look frozen. The template is `nginx-education-demo.conf`; `deploy.sh` inserts it before the catch-all `location /` so the more specific prefix wins. ⚠️ `nginx.conf` includes **every** file in `/etc/nginx/sites-enabled/`, suffix or not, so a backup written beside the original loads as a second copy of the whole vhost — `deploy.sh` writes backups to `/etc/nginx/backups` and refuses to reload if `nginx -t` reports a conflicting server name.

# Working in this repo

## Build, test and the database

Module `github.com/kayushkin/education-demo`, server in `cmd/education-demo-server`, front end in `web/` (React 19 + Vite + react-router, `npm run build`, `npm test` is vitest). Plain `go build` and `go test ./...` — **no build tag**, unlike the FTS stores. SQLite at `~/.config/education-demo/education-demo.db` (`EDUCATION_DEMO_DB`), DSN `_journal_mode=WAL&_foreign_keys=on&_txlock=immediate&_busy_timeout=5000`. **`_txlock=immediate` is load-bearing**: `appendMessage` bumps the session's sequence number inside its transaction, and without the write lock taken at `BEGIN` two concurrent appends can read the same value. `TestConcurrentAppendsGetDistinctSeq` in `internal/store/store_test.go` is the test that fails if it is removed.

## Deploy

`./deploy.sh` runs `~/bin/deploy-gate check` first, then builds both halves, installs the binary and unit, restarts, installs the nginx location if it is missing, and records the deploy with `deploy-gate record`. Three refusals worth knowing before you fight one: it **refuses a front end whose `web/dist/index.html` does not reference `/education-demo/assets/`**, because a wrong vite base 404s every asset and the page loads blank behind nginx with nothing but network-tab errors; it compares the **build id of the running process** against the binary it just built, rather than reading a route's status code, because an unmatched API path used to answer 200 with `index.html` and once made a failed restart look deployed; and it fails unless `https://kayushkin.com/education-demo/api/health` answers 200. Pushes to `github.com/kayushkin/education-demo`.
