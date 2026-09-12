import { useMemo, useState, type CSSProperties } from 'react'
import { alertRank, humanize, isTopPriority, severityColor, severityIndex, sinceLabel } from '../lib/display'
import * as api from '../lib/api'
import type { Alert, Goal, Student, Team, Vocabularies } from '../lib/types'
import { SectionHead } from './primitives'

interface Props {
  vocab: Vocabularies
  alerts: Alert[]
  teams: Team[]
  students: Student[]
  goals: Goal[]
  freshAlertIds: Set<string>
  now: number
  onOpenTeam: (teamID: string) => void
  onOpenStudent: (studentID: string) => void
  onResolved: (alertID: string) => void
}

export function AlertFeed(props: Props) {
  const { vocab, alerts, teams, students, goals, freshAlertIds, now } = props
  const [resolving, setResolving] = useState<Record<string, boolean>>({})
  const [failure, setFailure] = useState<string | null>(null)

  const teamName = useMemo(() => new Map(teams.map((t) => [t.id, t.name])), [teams])
  const studentName = useMemo(() => new Map(students.map((s) => [s.id, s.name])), [students])
  const goalLabel = useMemo(() => new Map(goals.map((g) => [g.id, g.short_label])), [goals])

  // Ranked: severity first, then the kind's own position in the vocabulary
  // (most urgent kind first), then newest.
  const ranked = useMemo(() => {
    return alerts
      .filter((a) => !a.resolved)
      .slice()
      .sort((a, b) => {
        const d = alertRank(vocab, b.kind, b.severity) - alertRank(vocab, a.kind, a.severity)
        if (d !== 0) return d
        return new Date(b.at).getTime() - new Date(a.at).getTime()
      })
  }, [alerts, vocab])

  const criticalCount = ranked.filter((a) => isTopPriority(vocab, a.kind, a.severity)).length

  async function resolve(a: Alert) {
    setResolving((p) => ({ ...p, [a.id]: true }))
    setFailure(null)
    try {
      await api.resolveAlert(a.id)
      props.onResolved(a.id)
    } catch (e) {
      setFailure((e as Error).message)
      setResolving((p) => ({ ...p, [a.id]: false }))
    }
  }

  return (
    <div className="feed panel" style={{ padding: '13px 13px 13px', minHeight: 0, flex: 1 }}>
      <SectionHead
        n="01"
        title="Where to go"
        right={
          <span style={{ marginLeft: 'auto', display: 'flex', gap: 6, alignItems: 'center' }}>
            {criticalCount > 0 && (
              <span className="pill pill-alarm"><i className="dot dot-pulse" />{criticalCount} urgent</span>
            )}
            <span className="pill pill-mute">{ranked.length} open</span>
          </span>
        }
      />

      {failure && <div className="banner err" role="alert"><span className="glyph">✕</span><span>{failure}</span></div>}

      <div className="feed-scroll">
        {ranked.length === 0 && (
          <div className="empty-note">
            <b>No open alerts</b>
            Every team is either working cleanly or has not said enough yet for the
            agent to assert anything. Alerts appear here the moment one does.
          </div>
        )}

        {ranked.map((a, i) => {
          const sevIdx = severityIndex(vocab, a.severity)
          const top = isTopPriority(vocab, a.kind, a.severity)
          const who = a.student_id ? studentName.get(a.student_id) : undefined
          const where = teamName.get(a.team_id) ?? 'Team'
          return (
            <article
              key={a.id}
              className={`alert${top ? ' top-priority' : ''}`}
              style={{
                '--sev': severityColor(sevIdx),
                // An alert that arrived over the wire snaps in immediately; the
                // ones already there on load reveal in sequence down the feed.
                animationDelay: freshAlertIds.has(a.id) ? '0ms' : `${Math.min(i, 9) * 40}ms`,
              } as CSSProperties}
            >
              <div className="alert-head">
                {top && <span className="pill pill-alarm"><i className="dot dot-pulse" />Go now</span>}
                <span className="alert-kind">{humanize(a.kind)}</span>
                <span className="pill pill-mute">{a.severity}</span>
                <span className="alert-where">{sinceLabel(a.at, now)}</span>
              </div>

              <h3 className="alert-title">{a.title}</h3>

              {a.quote && <blockquote className="quote">“{a.quote}”</blockquote>}

              <p className="alert-detail clamped" title={a.detail}>{a.detail}</p>

              <div className="alert-foot">
                <button className="btn btn-sm btn-ghost" onClick={() => props.onOpenTeam(a.team_id)}>
                  {where}
                </button>
                {a.student_id && who && (
                  <button className="btn btn-sm btn-ghost" onClick={() => props.onOpenStudent(a.student_id!)}>
                    {who}
                  </button>
                )}
                {a.goal_id && <span className="pill pill-mute">{goalLabel.get(a.goal_id) ?? 'goal'}</span>}
                <button
                  className="linkish"
                  style={{ marginLeft: 'auto' }}
                  disabled={resolving[a.id]}
                  onClick={() => void resolve(a)}
                >
                  {resolving[a.id] ? 'dismissing…' : 'dismiss'}
                </button>
              </div>
            </article>
          )
        })}
      </div>
    </div>
  )
}
