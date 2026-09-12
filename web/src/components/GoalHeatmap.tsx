import { useMemo, type CSSProperties } from 'react'
import { humanize, stateColor, UNASSESSED_PCT } from '../lib/display'
import type { Goal, GoalRollup, Team, TeamGoalCell, Vocabularies } from '../lib/types'
import { SectionHead, StateKey, UnderstandingStrip } from './primitives'

interface Props {
  vocab: Vocabularies
  goals: Goal[]
  teams: Team[]
  cells: TeamGoalCell[]
  rollups: GoalRollup[]
  onOpenTeam: (teamID: string) => void
}

export function GoalHeatmap({ vocab, goals, teams, cells, rollups, onOpenTeam }: Props) {
  const lookup = useMemo(() => {
    const m = new Map<string, TeamGoalCell>()
    for (const c of cells) m.set(`${c.goal_id}::${c.team_id}`, c)
    return m
  }, [cells])

  const understandsState = vocab.understanding_states[vocab.understanding_states.length - 1]
  const understandsColor = stateColor(vocab.understanding_states.length - 1)

  return (
    <section className="section">
      <SectionHead
        n="04"
        title="Goals × teams"
        hint={
          <>
            Percent of each team <em>assessed as “{humanize(understandsState).toLowerCase()}”</em>. A hatched cell is a
            goal that team has not opened — not a zero.
          </>
        }
      />

      <div className="panel" style={{ padding: 14, marginBottom: 12 }}>
        <div className="heatmap-wrap">
          <table className="heatmap">
            <thead>
              <tr>
                <th className="corner">Goal</th>
                {teams.map((t) => (
                  <th key={t.id} title={t.name}>{t.name.replace(/^Team\s*/i, 'T')}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {goals.map((g) => (
                <tr key={g.id}>
                  <th title={g.text}>{g.ordinal}. {g.short_label}</th>
                  {teams.map((t) => {
                    const c = lookup.get(`${g.id}::${t.id}`)
                    const assessed = c?.assessed ?? 0
                    if (!c || assessed === 0) {
                      return (
                        <td key={t.id} style={{ padding: '0 2px' }}>
                          <button className="cell empty" onClick={() => onOpenTeam(t.id)}
                            title={`${t.name} · ${g.short_label}: not yet assessed`}>
                            <span className="pc">–</span>
                          </button>
                        </td>
                      )
                    }
                    const good = c.counts[understandsState] ?? 0
                    const share = good / assessed
                    return (
                      <td key={t.id} style={{ padding: '0 2px' }}>
                        <button
                          className={`cell${c.any_understands ? '' : ' stranded'}`}
                          onClick={() => onOpenTeam(t.id)}
                          // One quantity, one sequential scale. Tinting by the
                          // team's dominant state instead mixed four hues into
                          // the same dark ground and produced unreadable mud —
                          // and implied a second variable that is not there.
                          style={{
                            background: `color-mix(in srgb, ${understandsColor} ${8 + share * 74}%, var(--ink-100))`,
                          }}
                          title={`${t.name} · ${g.short_label} — ${vocab.understanding_states
                            .map((s) => `${humanize(s)} ${c.counts[s] ?? 0}`).join(', ')} · assessed ${c.assessed}/${c.roster}${
                            c.any_understands ? '' : ' · NOBODY in this team understands it'}`}
                        >
                          <span className="pc" style={{ color: share > .5 ? 'var(--ink-000)' : 'var(--text)' }}>
                            {Math.round(share * 100)}
                          </span>
                        </button>
                      </td>
                    )
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <div style={{ marginTop: 14, paddingTop: 12, borderTop: '1px solid var(--rule-faint)' }}>
          <StateKey vocab={vocab} />
        </div>
      </div>

      <div className="rollups">
        {rollups.map((r) => {
          const goal = goals.find((g) => g.id === r.goal_id)
          const notAssessed = r.understood_pct === UNASSESSED_PCT
          return (
            <div className="rollup" key={r.goal_id}>
              <div className="rollup-head">
                <span className="ord">G{r.ordinal}</span>
                <span className="lab">{r.short_label}</span>
                {notAssessed
                  ? <span className="pct na" title="No student has been assessed on this goal yet. This is not zero.">not yet assessed</span>
                  : <span className="pct" style={{ color: stateColor(vocab.understanding_states.length - 1) }}>
                      {r.understood_pct.toFixed(0)}<small style={{ fontSize: 11, color: 'var(--text-mute)' }}>%</small>
                    </span>}
              </div>
              {goal && <div className="rollup-goal">{goal.text}</div>}
              <UnderstandingStrip vocab={vocab} counts={r.counts} assessed={r.assessed} roster={r.roster} />
              <div className="rollup-foot">
                <span title="Students the agent has heard enough from to judge on this goal">
                  coverage {r.assessed}/{r.roster}
                </span>
                {vocab.understanding_states.map((s, i) => (
                  (r.counts[s] ?? 0) > 0 ? (
                    <span key={s} className="pill" style={{ '--c': stateColor(i), color: stateColor(i), borderColor: 'var(--rule)' } as CSSProperties}>
                      {r.counts[s]} {humanize(s).toLowerCase()}
                    </span>
                  ) : null
                ))}
                {r.teams_with_no_understanding.length > 0 && (
                  <span className="pill pill-alarm" title="These teams have discussed this goal and nobody in them understands it">
                    stranded: {r.teams_with_no_understanding.join(', ')}
                  </span>
                )}
              </div>
            </div>
          )
        })}
      </div>
    </section>
  )
}
