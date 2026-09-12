import { useMemo, useState, type CSSProperties } from 'react'
import {
  deltaGlyph, deltaSign, humanize, signedPoints, stateColor, UNASSESSED_PCT,
} from '../lib/display'
import type { Progress, ProgressView, StateDelta, Team, Vocabularies } from '../lib/types'
import { SectionHead, UnderstandingStrip } from './primitives'

// ===========================================================================
// The form, and why.
// ===========================================================================
//
// The question is "did each of ~5 topics get more understood by the end", and
// the teacher has to answer it in one second across all of them. The candidates:
//
//   · Grouped bars — ten bars for five goals. Direction is a second-order read:
//     you compare the heights of two adjacent bars, per goal, five times. The
//     size of the change is never drawn, only inferred.
//   · Slope chart — direction reads instantly, but five lines between two
//     shared axes cross each other, and once they cross each line needs a
//     label at both ends or a legend to tell them apart.
//   · Paired stacked bars — carries the full four-state composition, which is
//     the one thing the other two lose, but the understood share is a segment
//     boundary floating at a different offset in every bar, so the headline
//     number is the hardest thing on it to compare.
//
//   → DUMBBELL (connected dot plot), one row per goal on one shared 0–100%
//     axis. The change IS a drawn object: the segment's length is the size of
//     it and its direction is the sign, so five goals read as five arrows
//     pointing mostly one way. Each row carries its own label, so there is
//     nothing to decode against a legend, and a goal that went backwards is a
//     segment pointing the other way — a shape difference, before any colour.
//     The four-state composition it would otherwise lose is kept underneath as
//     a paired before/after strip, which is exactly the paired-stacked-bar
//     option demoted to where it belongs: the detail under the headline.
//
// Colour is never the only channel here, same rule as the rest of the console:
// direction is carried by the dots' relative POSITION, by an arrow glyph in the
// readout, and by a signed number — the segment's colour is the fourth copy of
// it, not the first.

const UNDERSTOOD = 'understood'
const MISUNDERSTOOD = 'misunderstood'

/** Which way is good. Understanding rising is good; misunderstanding rising is not. */
type Polarity = 1 | -1

function goodness(sign: -1 | 0 | 1 | null, polarity: Polarity): 'better' | 'worse' | 'flat' | 'none' {
  if (sign === null) return 'none'
  if (sign === 0) return 'flat'
  return sign === polarity ? 'better' : 'worse'
}

/**
 * One measure's before → after on the shared 0–100% axis.
 *
 * `before`/`after` of -1 mean the population was never measured. That is not a
 * zero and not a flat line — the track is replaced by the words, the same way
 * every other unassessed number in this dashboard is rendered.
 */
function Dumbbell({ measure, before, after, polarity, tone, size = 'md' }: {
  measure: string
  before: number
  after: number
  polarity: Polarity
  /** The colour of the quantity itself, for the end dot. */
  tone: string
  size?: 'md' | 'sm'
}) {
  const sign = deltaSign(before, after)
  const good = goodness(sign, polarity)

  if (sign === null) {
    return (
      <div className={`ba-track ${size}`} data-good="none">
        <span className="ba-na">not yet assessed</span>
      </div>
    )
  }

  const lo = Math.min(before, after)
  const hi = Math.max(before, after)
  const label =
    `${measure}: ${before.toFixed(0)}% at the start, ${after.toFixed(0)}% now — ` +
    (good === 'flat' ? 'unchanged' : `${signedPoints(before, after)} points, ${good === 'better' ? 'the right way' : 'the wrong way'}`)

  return (
    <div className={`ba-track ${size}`} data-good={good} role="img" aria-label={label} title={label}>
      <span className="ba-scale">
        <span className="ba-grid" aria-hidden="true" />
        <span className="ba-seg" style={{ left: `${lo}%`, width: `${hi - lo}%` }} />
        <span className="ba-dot before" style={{ left: `${before}%` }} />
        <span className="ba-dot after" style={{ left: `${after}%`, '--dot': tone } as CSSProperties} />
      </span>
    </div>
  )
}

/** The numbers beside a track, always printed — the chart never has to be read alone. */
function Readout({ measure, before, after, polarity }: {
  measure: string
  before: number
  after: number
  polarity: Polarity
}) {
  const sign = deltaSign(before, after)
  const good = goodness(sign, polarity)
  if (sign === null) {
    return (
      <div className="ba-read" data-good="none">
        <span className="ba-measure">{measure}</span>
        <span className="ba-nums na">–</span>
      </div>
    )
  }
  return (
    <div className="ba-read" data-good={good}>
      <span className="ba-measure">{measure}</span>
      <span className="ba-nums">
        {before.toFixed(0)}<small>%</small>
        <i className="ba-arrow">→</i>
        <b>{after.toFixed(0)}<small>%</small></b>
      </span>
      <span className="ba-delta">{deltaGlyph(sign)} {signedPoints(before, after)}</span>
    </div>
  )
}

/** A goal's row: the headline dumbbell, the misunderstood one, the composition. */
function GoalRow({ vocab, ordinal, label, delta, truthDelta }: {
  vocab: Vocabularies
  ordinal: number
  label: string
  delta: StateDelta
  /** The simulation's own before/after for this goal, when revealed. */
  truthDelta: StateDelta | null
}) {
  const understoodSign = deltaSign(delta.understood_pct_before, delta.understood_pct_after)
  const good = goodness(understoodSign, 1)
  const understoodColor = stateColor(vocab.understanding_states.length - 1)
  const misunderstoodColor = stateColor(0)

  return (
    <div className="ba-row" data-good={good}>
      <div className="ba-head">
        <span className="ord">G{ordinal}</span>
        <span className="lab">{label}</span>
        {good === 'worse' && (
          <span className="pill pill-decline" title="Fewer students understand this goal than when the lesson started. This is the one to act on.">
            ▼ went backwards
          </span>
        )}
        <span className="ba-pairs">
          {delta.pairs > 0
            ? `${delta.pairs} comparable ${delta.pairs === 1 ? 'judgement' : 'judgements'}`
            : 'nothing comparable yet'}
        </span>
      </div>

      <div className="ba-lines">
        <Dumbbell
          measure={UNDERSTOOD} polarity={1} tone={understoodColor}
          before={delta.understood_pct_before} after={delta.understood_pct_after}
        />
        <Readout
          measure={UNDERSTOOD} polarity={1}
          before={delta.understood_pct_before} after={delta.understood_pct_after}
        />
        <Dumbbell
          measure={MISUNDERSTOOD} polarity={-1} tone={misunderstoodColor} size="sm"
          before={delta.misunderstood_pct_before} after={delta.misunderstood_pct_after}
        />
        <Readout
          measure={MISUNDERSTOOD} polarity={-1}
          before={delta.misunderstood_pct_before} after={delta.misunderstood_pct_after}
        />
        {truthDelta && (
          <>
            <Dumbbell
              measure={`ground truth ${UNDERSTOOD}`} polarity={1} tone="var(--truth)" size="sm"
              before={truthDelta.understood_pct_before} after={truthDelta.understood_pct_after}
            />
            <div className="ba-read truth">
              <span className="ba-measure">truth</span>
              <span className="ba-nums">
                {fmtPct(truthDelta.understood_pct_before)}
                <i className="ba-arrow">→</i>
                <b>{fmtPct(truthDelta.understood_pct_after)}</b>
              </span>
              <span className="ba-delta">
                {truthDelta.pairs > 0 ? `${truthDelta.pairs} pairs` : '–'}
              </span>
            </div>
          </>
        )}
      </div>

      {delta.pairs > 0 && (
        <div className="ba-comp">
          <span className="eyebrow">before</span>
          <UnderstandingStrip vocab={vocab} counts={delta.before} assessed={delta.pairs} roster={delta.pairs} />
          <span className="eyebrow">after</span>
          <UnderstandingStrip vocab={vocab} counts={delta.after} assessed={delta.pairs} roster={delta.pairs} />
        </div>
      )}
    </div>
  )
}

function fmtPct(n: number): string {
  return n === UNASSESSED_PCT ? '–' : `${n.toFixed(0)}%`
}

// --- students ---------------------------------------------------------------

type SortKey = 'followup' | 'improved' | 'name' | 'team'

const SORTS: { key: SortKey; label: string }[] = [
  { key: 'followup', label: 'Needs follow-up first' },
  { key: 'improved', label: 'Most improved first' },
  { key: 'name', label: 'Name' },
  { key: 'team', label: 'Team' },
]

export function ProgressPanel({ vocab, progress, teams, loading, error, reveal, onRetry, onOpenStudent }: {
  vocab: Vocabularies
  progress: Progress | null
  teams: Team[]
  loading: boolean
  error: string | null
  /** The existing "Reveal ground truth" toggle. `actual` is a CHECK, never a swap. */
  reveal: boolean
  onRetry: () => void
  onOpenStudent: (studentID: string) => void
}) {
  const [sort, setSort] = useState<SortKey>('followup')

  const teamName = useMemo(() => new Map(teams.map((t) => [t.id, t.name])), [teams])

  // `actual` may simply not be there — a real classroom has no answer key, and
  // even a simulated one has none before its first phase is recorded.
  const truth: ProgressView | null = reveal ? (progress?.actual ?? null) : null
  const truthGoal = useMemo(() => {
    if (!truth) return null
    return new Map(truth.goals.map((g) => [g.goal_id, g.delta]))
  }, [truth])
  const truthStudent = useMemo(() => {
    if (!truth) return null
    return new Map(truth.students.map((s) => [s.student_id, s.delta]))
  }, [truth])

  const observed = progress?.observed ?? null

  const students = useMemo(() => {
    if (!observed) return []
    const rows = observed.students.slice()
    const gain = (d: StateDelta) =>
      deltaSign(d.understood_pct_before, d.understood_pct_after) === null
        ? null
        : d.understood_pct_after - d.understood_pct_before
    const byName = (a: typeof rows[number], b: typeof rows[number]) => a.name.localeCompare(b.name)

    rows.sort((a, b) => {
      if (sort === 'name') return byName(a, b)
      if (sort === 'team') {
        const t = (teamName.get(a.team_id) ?? '').localeCompare(teamName.get(b.team_id) ?? '')
        return t !== 0 ? t : byName(a, b)
      }
      // A student with nothing comparable yet is not "stuck" — the agent simply
      // has not heard enough from them twice. They sort last in both directions
      // rather than being read as a result.
      const ga = gain(a.delta), gb = gain(b.delta)
      if (ga === null && gb === null) return byName(a, b)
      if (ga === null) return 1
      if (gb === null) return -1
      if (ga !== gb) return sort === 'improved' ? gb - ga : ga - gb
      // Same movement: whoever is still holding the most wrong ideas goes first.
      const ma = a.delta.misunderstood_pct_after, mb = b.delta.misunderstood_pct_after
      if (ma !== mb) return sort === 'improved' ? ma - mb : mb - ma
      const ua = a.delta.understood_pct_after, ub = b.delta.understood_pct_after
      if (ua !== ub) return sort === 'improved' ? ub - ua : ua - ub
      return byName(a, b)
    })
    return rows
  }, [observed, sort, teamName])

  if (error) {
    return (
      <section className="section">
        <SectionHead n="03" title="Learning this session" />
        <div className="banner err">
          <span className="glyph">✕</span><span style={{ flex: 1 }}>{error}</span>
          <button className="btn btn-sm btn-ghost" onClick={onRetry}>Retry</button>
        </div>
      </section>
    )
  }

  if (loading || !progress || !observed) {
    return (
      <section className="section">
        <SectionHead n="03" title="Learning this session" />
        <div className="panel">
          <div className="generating" style={{ padding: '34px 20px' }}>
            <span className="scan" />
            <p>Comparing the agent's first opinion of every student on every goal against its current one.</p>
          </div>
        </div>
      </section>
    )
  }

  const cls = observed.class
  const midLesson = progress.phases_elapsed < progress.phase_count
  // The denominator is the goals the agent can actually compare. The server
  // folds "nothing comparable yet" into goals_flat, which is right for its own
  // count and wrong for a headline — a goal nobody has opened did not fail to
  // improve. improved/declined come from the server untouched.
  const comparableGoals = observed.goals.filter((g) => g.delta.pairs > 0).length
  const unmeasuredGoals = observed.goals.length - comparableGoals

  return (
    <section className="section">
      <SectionHead
        n="03"
        title="Learning this session"
        hint={
          <>
            The agent's first call on each student-goal against its call now, over the pairs
            it can compare. A track that runs the right way is green; one that runs the wrong
            way is ochre.
          </>
        }
        right={
          <span className={`pill ${midLesson ? 'pill-amber' : 'pill-live'}`} style={{ marginLeft: 'auto' }}
            title={midLesson
              ? 'The lesson is still running. These are the numbers so far, not an end-of-session result.'
              : 'Every phase of the lesson has been played.'}>
            phase {progress.phases_elapsed} / {progress.phase_count}
          </span>
        }
      />

      <div className="panel learn">
        {midLesson ? (
          <div className="banner warn" style={{ marginBottom: 16 }}>
            <span className="glyph">⚠</span>
            <span>
              <strong>This is a mid-lesson reading, not an end-of-session result.</strong>{' '}
              The class is on phase {progress.phases_elapsed} of {progress.phase_count} — understanding is
              still being advanced between phases, so these figures will move again.
            </span>
          </div>
        ) : null}

        {comparableGoals === 0 ? (
          <p className="headline">
            Nothing to compare yet. The agent has to have judged the same student on the same
            goal <strong>twice</strong> before there is a before and an after — a pair it has an
            opinion about now but had none about at the start would read as a student improving
            when all that improved was the agent's coverage.
          </p>
        ) : (
          <p className="headline">
            <span className="big">{observed.goals_improved} of {comparableGoals}</span>{' '}
            {comparableGoals === 1 ? 'goal is' : 'goals are'} more understood than at the start
            {observed.goals_declined > 0 && (
              <> — and <strong style={{ color: 'var(--decline-ink)' }}>{observed.goals_declined} went backwards</strong></>
            )}.
            {unmeasuredGoals > 0 && (
              <> {unmeasuredGoals} {unmeasuredGoals === 1 ? 'goal has' : 'goals have'} nothing comparable yet.</>
            )}
          </p>
        )}

        <div className="statrow">
          <div className="stat">
            <div className="k">Understood now</div>
            <div className="v" style={{ color: 'var(--growth-ink)' }}>
              {fmtPct(cls.understood_pct_after).replace('%', '')}<small>%</small>
            </div>
            <div className="n">
              {cls.pairs > 0
                ? <>was {fmtPct(cls.understood_pct_before)} · {deltaGlyph(deltaSign(cls.understood_pct_before, cls.understood_pct_after))} {signedPoints(cls.understood_pct_before, cls.understood_pct_after)} points</>
                : 'not yet assessed'}
            </div>
          </div>
          <div className="stat">
            <div className="k">Misunderstood now</div>
            <div className="v" style={{ color: 'var(--alert-ink)' }}>
              {fmtPct(cls.misunderstood_pct_after).replace('%', '')}<small>%</small>
            </div>
            <div className="n">
              {cls.pairs > 0
                ? <>was {fmtPct(cls.misunderstood_pct_before)} · {deltaGlyph(deltaSign(cls.misunderstood_pct_before, cls.misunderstood_pct_after))} {signedPoints(cls.misunderstood_pct_before, cls.misunderstood_pct_after)} points</>
                : 'not yet assessed'}
            </div>
          </div>
          <div className="stat">
            <div className="k">Judgements that rose</div>
            <div className="v" style={{ color: 'var(--growth-ink)' }}>{cls.improved}</div>
            <div className="n">{cls.declined} fell · {cls.unchanged} held · {cls.pairs} compared</div>
          </div>
          <div className="stat">
            <div className="k">Goals improved</div>
            <div className="v">{observed.goals_improved}<small>/{comparableGoals || '–'}</small></div>
            <div className="n">{observed.goals_declined} declined · {Math.max(0, observed.goals_flat - unmeasuredGoals)} flat</div>
          </div>
        </div>

        {truth && (
          <div className="truth-check">
            <span className="pill pill-violet">ground truth</span>
            <span>
              The simulation's own phase-1 states against now:{' '}
              <strong>{fmtPct(truth.class.understood_pct_before)} → {fmtPct(truth.class.understood_pct_after)}</strong>{' '}
              understood, <strong>{fmtPct(truth.class.misunderstood_pct_before)} → {fmtPct(truth.class.misunderstood_pct_after)}</strong>{' '}
              misunderstood, over {truth.class.pairs} pairs, with {truth.goals_improved} of{' '}
              {truth.goals.filter((g) => g.delta.pairs > 0).length} goals up. That is the{' '}
              <em>check</em> on the numbers above, not a replacement for them — the figures the
              teacher actually gets are the agent's, and the distance between the two is how much
              to trust them.
            </span>
          </div>
        )}
        {reveal && !progress.actual && (
          <div className="truth-check">
            <span className="pill pill-violet">ground truth</span>
            <span>
              The server returned no ground-truth comparison for this session, so there is nothing
              to check the agent's figures against. The numbers above stand on their own — which is
              also the only thing a real classroom ever offers.
            </span>
          </div>
        )}

        <div className="ba-axis">
          <span className="eyebrow">per goal · share of comparable judgements</span>
          <span className="ba-ticks" aria-hidden="true"><i>0%</i><i>25%</i><i>50%</i><i>75%</i><i>100%</i></span>
        </div>

        <div className="ba-list">
          {observed.goals.map((g) => (
            <GoalRow
              key={g.goal_id}
              vocab={vocab}
              ordinal={g.ordinal}
              label={g.short_label}
              delta={g.delta}
              truthDelta={truthGoal?.get(g.goal_id) ?? null}
            />
          ))}
        </div>

        <div className="learn-students">
          <div className="learn-students-head">
            <span className="eyebrow">per student · pooled across their goals</span>
            <label className="sort-by">
              <span className="eyebrow">order</span>
              <select className="select" value={sort} onChange={(e) => setSort(e.target.value as SortKey)}>
                {SORTS.map((s) => <option key={s.key} value={s.key}>{s.label}</option>)}
              </select>
            </label>
          </div>

          <div className="table-scroll">
            <table className="ptable progress-table">
              <thead>
                <tr>
                  <th>Student</th>
                  <th>Team</th>
                  <th colSpan={2}>Understood</th>
                  <th colSpan={2}>Misunderstood</th>
                  <th>Moved</th>
                  {truth && <th>Truth understood</th>}
                </tr>
              </thead>
              <tbody>
                {students.map((s) => {
                  const d = s.delta
                  const sign = deltaSign(d.understood_pct_before, d.understood_pct_after)
                  const good = goodness(sign, 1)
                  const t = truthStudent?.get(s.student_id) ?? null
                  return (
                    <tr key={s.student_id} data-good={good}>
                      <td>
                        <button className="who" onClick={() => onOpenStudent(s.student_id)}>
                          {s.is_human && <span style={{ color: 'var(--accent-ink)' }} aria-label="real person">◈ </span>}
                          {s.name}
                        </button>
                      </td>
                      <td className="dim">{teamName.get(s.team_id) ?? '–'}</td>
                      <td className="track-cell">
                        <Dumbbell
                          measure={`${s.name} ${UNDERSTOOD}`} polarity={1} size="sm"
                          tone={stateColor(vocab.understanding_states.length - 1)}
                          before={d.understood_pct_before} after={d.understood_pct_after}
                        />
                      </td>
                      <td className="nums" data-good={good}>
                        {d.pairs > 0
                          ? <>{fmtPct(d.understood_pct_before)} → <b>{fmtPct(d.understood_pct_after)}</b>{' '}
                              <span className="ba-delta">{deltaGlyph(sign)} {signedPoints(d.understood_pct_before, d.understood_pct_after)}</span></>
                          : <span className="na">not yet assessed</span>}
                      </td>
                      <td className="track-cell">
                        <Dumbbell
                          measure={`${s.name} ${MISUNDERSTOOD}`} polarity={-1} size="sm"
                          tone={stateColor(0)}
                          before={d.misunderstood_pct_before} after={d.misunderstood_pct_after}
                        />
                      </td>
                      <td className="nums" data-good={goodness(deltaSign(d.misunderstood_pct_before, d.misunderstood_pct_after), -1)}>
                        {d.pairs > 0
                          ? <>{fmtPct(d.misunderstood_pct_before)} → <b>{fmtPct(d.misunderstood_pct_after)}</b></>
                          : <span className="na">–</span>}
                      </td>
                      <td className="moved">
                        {d.pairs > 0
                          ? <>
                              <span title={`${d.improved} of this student's judgements rose`} style={{ color: d.improved > 0 ? 'var(--growth-ink)' : 'var(--ink-faint)' }}>▲{d.improved}</span>{' '}
                              <span title={`${d.declined} fell`} style={{ color: d.declined > 0 ? 'var(--decline-ink)' : 'var(--ink-faint)' }}>▼{d.declined}</span>{' '}
                              <span title={`${d.unchanged} held`} style={{ color: 'var(--ink-faint)' }}>={d.unchanged}</span>
                            </>
                          : <span className="na">–</span>}
                      </td>
                      {truth && (
                        <td className="nums truth">
                          {t && t.pairs > 0
                            ? <>{fmtPct(t.understood_pct_before)} → <b>{fmtPct(t.understood_pct_after)}</b></>
                            : <span className="na">{s.is_human ? 'real person' : '–'}</span>}
                        </td>
                      )}
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </div>

        <div className="proof-caveat">
          <span className="mark">⚠</span>
          <span>
            Every figure here is over the student-goal pairs the agent judged{' '}
            <strong style={{ color: 'var(--ink)' }}>both at the start and now</strong>. A pair it has an
            opinion about today but had none about then is left out on purpose: counting it would
            report a class improving when the only thing that improved was how much the agent had
            heard. States are the server's own, in its worst→best order ({vocab.understanding_states.map(humanize).join(' · ')}).
          </span>
        </div>
      </div>
    </section>
  )
}
