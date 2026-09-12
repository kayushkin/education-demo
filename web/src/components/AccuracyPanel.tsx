import { useMemo, type CSSProperties } from 'react'
import { humanize, stateColor } from '../lib/display'
import type { Accuracy, Vocabularies } from '../lib/types'
import { SectionHead, StateMark } from './primitives'

/** A state name as a labelled token, so it never has to agree with the sentence. */
function StateToken({ vocab, state }: { vocab: Vocabularies; state: string }) {
  const i = vocab.understanding_states.indexOf(state)
  return (
    <span className="state-token" style={{ '--mark': stateColor(i) } as CSSProperties}>
      <StateMark vocab={vocab} state={state} size="sm" />
      {humanize(state)}
    </span>
  )
}

/**
 * The claim, and the proof of the claim.
 *
 * This panel only exists because the students are simulated: every one of them
 * carries a hidden understanding state the agent never sees, so the agent's
 * output can be scored against it. That is not possible with a real classroom,
 * and the panel says so rather than implying otherwise.
 */
export function AccuracyPanel({ vocab, accuracy, loading, error, onRetry }: {
  vocab: Vocabularies
  accuracy: Accuracy | null
  loading: boolean
  error: string | null
  onRetry: () => void
}) {
  const states = vocab.understanding_states
  /** The worst state is the one that matters: a student who is genuinely confused. */
  const worst = states[0]

  const hottestOffDiagonal = useMemo(() => {
    if (!accuracy) return 0
    let max = 0
    for (const t of states) for (const p of states) {
      if (t === p) continue
      max = Math.max(max, accuracy.confusion[t]?.[p] ?? 0)
    }
    return max
  }, [accuracy, states])

  if (error) {
    return (
      <section className="section">
        <SectionHead n="06" title="Ground truth" />
        <div className="banner err"><span className="glyph">✕</span><span>{error}</span>
          <button className="btn btn-sm btn-ghost" onClick={onRetry}>Retry</button></div>
      </section>
    )
  }

  if (loading || !accuracy) {
    return (
      <section className="section">
        <SectionHead n="06" title="Ground truth" />
        <div className="proof">
          <div className="generating" style={{ padding: '34px 20px' }}>
            <span className="scan" />
            <p>Scoring every assessment the agent has made against the hidden state each simulated student was given.</p>
          </div>
        </div>
      </section>
    )
  }

  const worstScore = accuracy.per_state[worst]
  const coverage = accuracy.truth_pairs > 0 ? (accuracy.scored / accuracy.truth_pairs) * 100 : 0

  return (
    <section className="section">
      <SectionHead
        n="06"
        title="Ground truth"
        right={<span className="pill pill-violet" style={{ marginLeft: 'auto' }}>revealed</span>}
      />
      <div className="proof">
        {/* The state names are third-person verbs ("misunderstands"), so they are
            shown as tokens rather than inflected into the sentence — a vocabulary
            the server owns cannot be trusted to agree with an English plural. */}
        {worstScore && worstScore.truth_count > 0 ? (
          <p className="headline">
            The agent caught{' '}
            <span className="big">{worstScore.caught} of {worstScore.truth_count}</span>{' '}
            students whose hidden state was <StateToken vocab={vocab} state={worst} /> —{' '}
            <strong>{worstScore.recall.toFixed(0)}% recall</strong> on the one state a teacher
            cannot afford to miss.
          </p>
        ) : (
          <p className="headline">
            No simulated student is hiding a <StateToken vocab={vocab} state={worst} /> yet, so there is
            nothing to catch. The figures below score everything the agent has asserted so far.
          </p>
        )}

        <div className="statrow">
          <div className="stat">
            <div className="k">Exact agreement</div>
            <div className="v" style={{ color: 'var(--live)' }}>{accuracy.exact_pct.toFixed(1)}<small>%</small></div>
            <div className="n">{accuracy.correct} of {accuracy.scored} calls on the nose</div>
          </div>
          <div className="stat">
            <div className="k">Within one state</div>
            <div className="v">{accuracy.adjacent_pct.toFixed(1)}<small>%</small></div>
            <div className="n">off by at most one rung of the scale</div>
          </div>
          <div className="stat">
            <div className="k">Coverage</div>
            <div className="v">{coverage.toFixed(0)}<small>%</small></div>
            <div className="n">{accuracy.scored} scored / {accuracy.truth_pairs} pairs with a truth</div>
          </div>
          <div className="stat">
            <div className="k">{humanize(worst)} recall</div>
            <div className="v" style={{ color: 'var(--violet)' }}>
              {worstScore ? worstScore.recall.toFixed(0) : '–'}<small>%</small>
            </div>
            <div className="n">the headline number</div>
          </div>
        </div>

        <div style={{ display: 'grid', gridTemplateColumns: 'auto minmax(0, 1fr)', gap: 26, marginTop: 20, alignItems: 'start' }}
          className="proof-cols">
          <div>
            <div className="eyebrow" style={{ marginBottom: 9 }}>Confusion — truth ↓ vs agent →</div>
            <div className="table-scroll">
              <table className="matrix">
                <thead>
                  <tr>
                    <th />
                    {states.map((s, i) => (
                      <th key={s} title={`Agent said: ${humanize(s)}`}>
                        <span style={{ color: stateColor(i) }}>{humanize(s)}</span>
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {states.map((t, ti) => (
                    <tr key={t}>
                      <th className="rowhead" title={`Truth: ${humanize(t)}`}>
                        <span style={{ color: stateColor(ti) }}>{humanize(t)}</span>
                      </th>
                      {states.map((p) => {
                        const n = accuracy.confusion[t]?.[p] ?? 0
                        const diag = t === p
                        const hot = !diag && n > 0 && n >= hottestOffDiagonal
                        const cls = ['', n === 0 ? 'zero' : '', diag ? 'diag' : 'off', hot ? 'hot' : ''].join(' ').trim()
                        return <td key={p} className={cls} title={`truth ${humanize(t)} → agent ${humanize(p)}: ${n}`}>{n}</td>
                      })}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>

          <div>
            <div className="eyebrow" style={{ marginBottom: 9 }}>Per state</div>
            <div className="table-scroll">
              <table className="ptable">
                <thead>
                  <tr>
                    <th>State</th>
                    <th>Truth</th>
                    <th>Caught</th>
                    <th>Recall</th>
                    <th>Said</th>
                    <th>Precision</th>
                  </tr>
                </thead>
                <tbody>
                  {states.map((s, i) => {
                    const ps = accuracy.per_state[s]
                    if (!ps) return null
                    return (
                      <tr key={s} className={s === worst ? 'headline-row' : undefined}>
                        <td style={{ color: stateColor(i) }}>{humanize(s)}</td>
                        <td>{ps.truth_count}</td>
                        <td>{ps.caught}</td>
                        <td>
                          <span style={{ display: 'inline-flex', gap: 7, alignItems: 'center', justifyContent: 'flex-end' }}>
                            <span className="bar" style={{ '--fill': stateColor(i) } as CSSProperties}>
                              <i style={{ width: `${Math.max(0, Math.min(100, ps.recall))}%` }} />
                            </span>
                            {ps.truth_count > 0 ? `${ps.recall.toFixed(0)}%` : '–'}
                          </span>
                        </td>
                        <td>{ps.predict_count}</td>
                        <td>{ps.predict_count > 0 ? `${ps.precision.toFixed(0)}%` : '–'}</td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          </div>
        </div>

        <div className="proof-caveat">
          <span className="mark">⚠</span>
          <span>
            <strong style={{ color: 'var(--text-dim)' }}>This scoreboard is only possible because the classroom is synthetic.</strong>{' '}
            Every simulated student was assigned a hidden understanding state before the lesson began, and the
            agent never sees it — so its output can be scored against it. A real classroom has no answer key,
            and this panel would not exist. Humans who join are excluded from every number here: nobody knows
            what a real person understands.
          </span>
        </div>
      </div>
    </section>
  )
}
