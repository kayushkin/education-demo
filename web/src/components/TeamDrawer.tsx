import { useMemo } from 'react'
import { humanize, severityColor, severityIndex, sinceLabel } from '../lib/display'
import type { Alert, Goal, Message, SessionState, TruthCell, Vocabularies } from '../lib/types'
import { Drawer } from './Drawer'
import { StateMark, UnderstandingStrip } from './primitives'
import { Transcript } from './Transcript'

export function TeamDrawer({ vocab, state, teamID, messages, truth, now, onClose, onOpenStudent }: {
  vocab: Vocabularies
  state: SessionState
  teamID: string
  messages: Message[]
  truth: TruthCell[] | null
  now: number
  onClose: () => void
  onOpenStudent: (studentID: string) => void
}) {
  const team = state.teams.find((t) => t.id === teamID)
  const members = useMemo(
    () => state.students.filter((s) => s.team_id === teamID),
    [state.students, teamID],
  )
  const cells = useMemo(
    () => state.team_goal_cells.filter((c) => c.team_id === teamID),
    [state.team_goal_cells, teamID],
  )
  const alerts = useMemo(
    () => state.alerts.filter((a) => a.team_id === teamID && !a.resolved),
    [state.alerts, teamID],
  )
  const assessmentFor = useMemo(() => {
    const m = new Map<string, string>()
    for (const a of state.assessments) m.set(`${a.student_id}::${a.goal_id}`, a.state)
    return m
  }, [state.assessments])
  const truthFor = useMemo(() => {
    if (!truth) return null
    const m = new Map<string, string>()
    for (const t of truth) m.set(`${t.student_id}::${t.goal_id}`, t.state)
    return m
  }, [truth])

  if (!team) return null
  const goalByID = new Map<string, Goal>(state.goals.map((g) => [g.id, g]))

  return (
    <Drawer
      title={team.name}
      subtitle={`${members.length} in the room · ${messages.length} lines`}
      badge={alerts.length > 0 ? <span className="pill pill-amber">{alerts.length} open</span> : undefined}
      onClose={onClose}
    >
      {alerts.length > 0 && (
        <div style={{ marginBottom: 18 }}>
          <div className="eyebrow" style={{ marginBottom: 8 }}>Open on this team</div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 7 }}>
            {alerts.map((a: Alert) => (
              <div key={a.id} className="alert" style={{ ['--sev' as string]: severityColor(severityIndex(vocab, a.severity)) }}>
                <div className="alert-head">
                  <span className="alert-kind">{humanize(a.kind)}</span>
                  <span className="pill pill-mute">{a.severity}</span>
                  <span className="alert-where">{sinceLabel(a.at, now)}</span>
                </div>
                <h3 className="alert-title">{a.title}</h3>
                {a.quote && <blockquote className="quote">“{a.quote}”</blockquote>}
                <p className="alert-detail">{a.detail}</p>
              </div>
            ))}
          </div>
        </div>
      )}

      <div style={{ marginBottom: 18 }}>
        <div className="eyebrow" style={{ marginBottom: 8 }}>Understanding by goal</div>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 9 }}>
          {state.goals.map((g) => {
            const c = cells.find((x) => x.goal_id === g.id)
            const stranded = Boolean(c && c.assessed > 0 && !c.any_understands)
            return (
              <div key={g.id} style={{ display: 'flex', flexDirection: 'column', gap: 5 }}>
                <div style={{ display: 'flex', gap: 8, alignItems: 'baseline' }}>
                  <span style={{ fontSize: 12.5, fontWeight: 600 }}>{g.ordinal}. {g.short_label}</span>
                  {stranded && <span className="pill pill-alarm">nobody understands this</span>}
                  <span className="mono" style={{ marginLeft: 'auto', fontSize: 10, color: 'var(--text-ghost)' }}>
                    {c ? `${c.assessed}/${c.roster}` : 'not yet assessed'}
                  </span>
                </div>
                {c
                  ? <UnderstandingStrip vocab={vocab} counts={c.counts} assessed={c.assessed} roster={c.roster} />
                  : <span className="strip"><span className="strip-unassessed" /></span>}
              </div>
            )
          })}
        </div>
      </div>

      <div style={{ marginBottom: 18 }}>
        <div className="eyebrow" style={{ marginBottom: 8 }}>Who is in here</div>
        <table className="classgrid" style={{ minWidth: 0, width: '100%' }}>
          <thead>
            <tr>
              <th className="corner">Student</th>
              {state.goals.map((g) => <th key={g.id} title={goalByID.get(g.id)?.text}>{g.short_label.slice(0, 7)}</th>)}
            </tr>
          </thead>
          <tbody>
            {members.map((s) => (
              <tr key={s.id}>
                <th>
                  <button className="who" onClick={() => onOpenStudent(s.id)}>
                    {s.is_human && <span style={{ color: 'var(--violet)' }}>◈ </span>}{s.name}
                  </button>
                </th>
                {state.goals.map((g) => {
                  const st = assessmentFor.get(`${s.id}::${g.id}`)
                  const tr = truthFor?.get(`${s.id}::${g.id}`)
                  return (
                    <td key={g.id} style={{ textAlign: 'center' }}>
                      {st
                        ? <span style={{ display: 'inline-flex', gap: 2, alignItems: 'center' }}>
                            <StateMark vocab={vocab} state={st} size="sm" />
                            {tr && tr !== st && <StateMark vocab={vocab} state={tr} size="sm" hollow />}
                          </span>
                        : <span style={{ color: 'var(--text-ghost)', fontFamily: 'var(--font-mono)', fontSize: 10 }}>–</span>}
                    </td>
                  )
                })}
              </tr>
            ))}
          </tbody>
        </table>
        {truthFor && (
          <div style={{ fontSize: 11, color: 'var(--text-mute)', marginTop: 7 }}>
            A hollow mark beside a solid one is the hidden truth where the agent disagreed.
          </div>
        )}
      </div>

      <div>
        <div className="eyebrow" style={{ marginBottom: 8 }}>Live transcript</div>
        {/* Its own scroll box: the transcript tails live, and without a bound
            it would drag the drawer past the alerts that justify opening it. */}
        <Transcript messages={messages} students={state.students} maxHeight="46vh" />
      </div>
    </Drawer>
  )
}
