# Run of show

Five minutes. The order matters: show the problem, show it being caught, then prove it was right.

## Before you start

```bash
systemctl --user status education-demo
curl -s https://kayushkin.com/education-demo/api/health
```

`agent_ready` must be `true`. If it is `false` the demo still runs, but on canned transcripts
with participation-only monitoring — say so rather than letting it pass as the real thing.

**Create and start the session 3-4 minutes before you present.** Writing ten transcripts takes
30-90 seconds, and the first assessments land about 40 seconds after that. Walking on stage to
an empty dashboard is the one avoidable failure here.

Use the **Forces and Motion** preset. Its goals each have a well-known wrong answer a student
can state confidently, which is what makes the flagship alert fire.

## 1 · The problem (30s)

Ten breakout groups, thirty students, one teacher. Show the dashboard with all ten rooms live
and talking at once.

> "She can be in one of these rooms. For the other nine she is guessing."

## 2 · Who is confidently wrong (90s) — the core

Open the alert feed and go to the top `critical` item. It will be something like:

> **Tariq — "constant velocity means zero forces" — survived correction and closed the discussion**

Click through to the team's transcript and show the quote in context. Make these three points:

- It is not flagging a wrong answer on a worksheet. It is flagging a wrong answer **being taught
  to teammates who cannot correct it**.
- The teammates in that room mostly do not know this goal, so nobody will fix it. The error
  spreads until an adult arrives.
- The alert names the student, the goal, and the exact words. She knows where to walk and what
  to say before she gets there.

Then show a `goal_unmastered` alert: a group where **nobody** understands a goal. They cannot
teach themselves out of it.

## 3 · A judge joins and gets caught (60s) — the moment

Have someone in the audience open `/education-demo/join` on their phone, pick a team, and type
one confidently wrong sentence. Something like:

> "guys I worked out goal 4 — mass and weight are the same thing, they both get smaller on the Moon"

Then hit **Assess now** on the dashboard rather than waiting for the tick.

Within a round they appear in the alert feed as `confidently_wrong`, `critical`, quoted. This
lands harder than anything on the slides: it is unmistakably not scripted, because they wrote it.

## 4 · The class view (45s)

- **Per-goal percentages** across all ten groups — where the whole class is weak, not just one room.
- **The misconception list**, ranked. On Forces and Motion it collects the real ones: heavier
  objects fall faster, weight and normal force as a third-law pair, impetus theory. That list is
  the artifact she reteaches from next lesson.

## 5 · Proving it (60s) — the close

Turn on **Reveal ground truth**.

> "Every student here has a true understanding of every goal, which the agent never saw. So
> unlike most of what you'll see today, this can be scored."

Lead with the recall number:

> "Eighteen students genuinely held a wrong idea. It caught eighteen."

Then the honest half, without being asked:

> "It also flagged eleven students who were only partly confused. Sixty-two percent precision.
> For a tool whose job is to stop an error spreading, over-flagging is the safe direction — but
> it's a real cost, and it's on the screen rather than in a footnote."

Finish on the confusion matrix and the 82.7% exact / 95% adjacent figures.

## If something breaks

| | |
|---|---|
| Dashboard empty after start | Transcripts are still being written. `runner.script_left` > 0 means it is working. |
| No alerts yet | **Assess now** forces a round instead of waiting 25s. |
| Alert feed frozen | SSE dropped. Reload — the dashboard refetches state on mount. |
| `agent_ready: false` | llm-bridge is down. Say the demo is on fallback transcripts; do not claim the assessments are live. |
| Everything is on fire | A second session on a different preset, created earlier, is the cheapest insurance. Make one. |

## Questions you will get

**"Is this just keyword matching?"** No. Show a `partial` assessment beside a `misunderstands`
one: both mention the goal, and the difference is whether the claim is correct. Then show the
evidence quote on each.

**"What about privacy?"** Real deployment reads breakout chat the platform already stores. It
should be opt-in per class, and the transcript should stay with the school. Not solved here —
this is a hackathon build and the classroom is synthetic.

**"Does it work on speech?"** Not yet. Everything here is text. Group audio needs diarisation
before this pipeline can touch it, and that is the honest next step, not a checkbox.

**"Why not just ask students to self-report?"** The students who most need help are the ones who
do not know they need it. A student who misunderstands is confident. Self-report cannot find them;
that is the whole premise.
