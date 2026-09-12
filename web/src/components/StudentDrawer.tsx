import { useMemo } from 'react'
import { humanize, latestTruthByPair, severityColor, severityIndex, sinceLabel } from '../lib/display'
import type { SessionState, TruthCell, Vocabularies } from '../lib/types'
import { Drawer } from './Drawer'
import { StateMark } from './primitives'

export function StudentDrawer({ vocab, state, studentID, truth, now, onClose, onOpenTeam }: {
  vocab: Vocabularies
  state: SessionState
  studentID: string
  truth: TruthCell[] | null
  now: number
  onClose: () => void
  onOpenTeam: (teamID: string) => void
}) {
  const student = state.students.find((s) => s.id === studentID)
  const team = state.teams.find((t) => t.id === student?.team_id)

  const byGoal = useMemo(() => {
    const m = new Map(state.assessments.filter((a) => a.student_id === studentID).map((a) => [a.goal_id, a]))
    return m
  }, [state.assessments, studentID])

  const truthByGoal = useMemo(() => {
    if (!truth) return null
    // Truth arrives one row per phase. Take the latest — the phase the accuracy
    // panel scores against — rather than whichever row sorted last.
    const latest = latestTruthByPair(truth.filter((t) => t.student_id === studentID))
    return new Map([...latest.values()].map((t) => [t.goal_id, t.state] as const))
  }, [truth, studentID])

  const alerts = useMemo(
    () => state.alerts.filter((a) => a.student_id === studentID && !a.resolved),
    [state.alerts, studentID],
  )

  if (!student) return null

  return (
    <Drawer
      title={student.name}
      subtitle={`${team?.name ?? 'no team'} · ${student.is_human ? 'real person' : 'simulated'}`}
      badge={student.is_human ? <span className="pill pill-violet">◈ human</span> : undefined}
      onClose={onClose}
    >
      <div style={{ display: 'flex', gap: 8, marginBottom: 16 }}>
        {team && <button className="btn btn-sm btn-ghost" onClick={() => onOpenTeam(team.id)}>Open {team.name} transcript</button>}
      </div>

      {student.is_human && truthByGoal && (
        <div className="banner" style={{ background: 'var(--accent-fill)' }}>
          <span className="glyph" style={{ color: 'var(--accent-ink)' }}>◈</span>
          <span>A real person has no ground truth, so nothing here is scored. The agent still judges them by exactly the same path as everyone else.</span>
        </div>
      )}

      <div className="eyebrow" style={{ marginBottom: 9 }}>Per goal, and what the agent heard</div>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 10, marginBottom: 20 }}>
        {state.goals.map((g) => {
          const a = byGoal.get(g.id)
          const t = truthByGoal?.get(g.id)
          const wrong = Boolean(a && t && a.state !== t)
          return (
            <div
              key={g.id}
              style={{
                border: `1px solid ${wrong ? 'var(--alert)' : 'var(--hairline-faint)'}`,
                borderRadius: 10, padding: '13px 14px', background: 'var(--surface)',
              }}
            >
              <div style={{ display: 'flex', gap: 9, alignItems: 'center', marginBottom: 6 }}>
                <span className="mono" style={{ fontSize: 10, color: 'var(--ink-faint)' }}>G{g.ordinal}</span>
                <span style={{ fontSize: 12.5, fontWeight: 600 }}>{g.short_label}</span>
                <span style={{ marginLeft: 'auto', display: 'flex', gap: 7, alignItems: 'center' }}>
                  {a
                    ? <>
                        <StateMark vocab={vocab} state={a.state} />
                        <span style={{ fontSize: 12 }}>{humanize(a.state)}</span>
                        <span className="mono" style={{ fontSize: 10, color: 'var(--ink-faint)' }}>
                          conf {a.confidence.toFixed(2)}
                        </span>
                      </>
                    : <span className="pill pill-mute" title="The agent has not heard evidence about this student on this goal. That is not the same as `unknown`, which the agent asserts.">
                        no assertion yet
                      </span>}
                </span>
              </div>
              <div style={{ fontSize: 12, color: 'var(--ink-muted)', lineHeight: 1.45, marginBottom: a?.evidence ? 7 : 0 }}>
                {g.text}
              </div>
              {a?.evidence && <blockquote className="quote" style={{ margin: 0 }}>“{a.evidence}”</blockquote>}
              {t && (
                <div style={{ marginTop: 8, display: 'flex', gap: 7, alignItems: 'center', fontSize: 11.5 }}>
                  <span className="eyebrow">truth</span>
                  <StateMark vocab={vocab} state={t} size="sm" hollow />
                  <span style={{ color: wrong ? 'var(--alert-ink)' : 'var(--ink-muted)' }}>
                    {humanize(t)}{wrong ? ' — the agent got this one wrong' : ' — agreed'}
                  </span>
                </div>
              )}
            </div>
          )
        })}
      </div>

      {alerts.length > 0 && (
        <>
          <div className="eyebrow" style={{ marginBottom: 9 }}>Alerts naming {student.name}</div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 7 }}>
            {alerts.map((a) => (
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
        </>
      )}
    </Drawer>
  )
}
