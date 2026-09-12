import { useMemo } from 'react'
import type { Alert, Goal, Message, Student, Team, TeamGoalCell, Vocabularies } from '../lib/types'
import { SectionHead, UnderstandingStrip } from './primitives'

interface Props {
  vocab: Vocabularies
  teams: Team[]
  goals: Goal[]
  students: Student[]
  cells: TeamGoalCell[]
  alerts: Alert[]
  messages: Record<string, Message[]>
  onOpenTeam: (teamID: string) => void
}

const CHATTER_LINES = 3

export function TeamFloor({ vocab, teams, goals, students, cells, alerts, messages, onOpenTeam }: Props) {
  const byTeam = useMemo(() => {
    const m = new Map<string, Map<string, TeamGoalCell>>()
    for (const c of cells) {
      let g = m.get(c.team_id)
      if (!g) { g = new Map(); m.set(c.team_id, g) }
      g.set(c.goal_id, c)
    }
    return m
  }, [cells])

  const roster = useMemo(() => {
    const m = new Map<string, Student[]>()
    for (const s of students) {
      const list = m.get(s.team_id)
      if (list) list.push(s); else m.set(s.team_id, [s])
    }
    return m
  }, [students])

  const studentName = useMemo(() => new Map(students.map((s) => [s.id, s.name])), [students])

  const openAlerts = useMemo(() => {
    const m = new Map<string, number>()
    for (const a of alerts) {
      if (a.resolved) continue
      m.set(a.team_id, (m.get(a.team_id) ?? 0) + 1)
    }
    return m
  }, [alerts])

  // A team is stranded on a goal when it has engaged with that goal and nobody
  // in it understands. Peer correction cannot arrive from inside the room.
  const strandedGoals = useMemo(() => {
    const m = new Map<string, Set<string>>()
    for (const c of cells) {
      if (c.assessed > 0 && !c.any_understands) {
        const set = m.get(c.team_id) ?? new Set<string>()
        set.add(c.goal_id)
        m.set(c.team_id, set)
      }
    }
    return m
  }, [cells])

  const speakers = useMemo(() => {
    const m = new Map<string, Set<string>>()
    for (const [teamID, log] of Object.entries(messages)) {
      m.set(teamID, new Set(log.map((x) => x.student_id)))
    }
    return m
  }, [messages])

  return (
    <section className="section">
      <SectionHead
        n="02"
        title="The floor"
        hint={
          <>
            Ten rooms at once. A hairline ring means the team has engaged a goal and
            <strong style={{ color: 'var(--alert-ink)' }}> nobody in it understands</strong> — it cannot dig itself out.
          </>
        }
      />
      <div className="floor">
        {teams.map((t) => {
          const stranded = strandedGoals.get(t.id)
          const members = roster.get(t.id) ?? []
          const spoke = speakers.get(t.id) ?? new Set<string>()
          const log = messages[t.id] ?? []
          const recent = log.slice(-CHATTER_LINES)
          const alertCount = openAlerts.get(t.id) ?? 0
          return (
            <button
              key={t.id}
              className={`team-card${stranded && stranded.size ? ' stranded' : ''}`}
              onClick={() => onOpenTeam(t.id)}
              aria-label={`Open ${t.name} transcript`}
            >
              <div className="team-card-head">
                <span className="team-name">{t.name}</span>
                <span className="team-badges">
                  {stranded && stranded.size > 0 && (
                    <span className="pill pill-alarm" title={`No one understands ${stranded.size} goal(s) this team has discussed`}>
                      stranded ×{stranded.size}
                    </span>
                  )}
                  {alertCount > 0 && <span className="pill pill-amber">{alertCount} alert{alertCount > 1 ? 's' : ''}</span>}
                </span>
              </div>

              <div className="roster">
                {members.map((s) => (
                  <span
                    key={s.id}
                    className={`chip${s.is_human ? ' human' : ''}${spoke.has(s.id) ? '' : ' quiet'}`}
                    title={s.is_human ? 'A real person joined this seat' : spoke.has(s.id) ? '' : 'Has not spoken in the window we hold'}
                  >
                    {s.is_human && <span aria-hidden>◈</span>}
                    {s.name}
                  </span>
                ))}
              </div>

              <div style={{ display: 'flex', flexDirection: 'column', gap: 5 }}>
                {goals.map((g) => {
                  const c = byTeam.get(t.id)?.get(g.id)
                  const isStranded = stranded?.has(g.id) ?? false
                  return (
                    <div className={`goalrow${isStranded ? ' stranded' : ''}`} key={g.id}>
                      <span className="lbl" title={g.text}>{g.short_label}</span>
                      {c
                        ? <UnderstandingStrip vocab={vocab} counts={c.counts} assessed={c.assessed} roster={c.roster} />
                        : <span className="strip"><span className="strip-unassessed" /></span>}
                      <span className="cov">{c ? `${c.assessed}/${c.roster}` : `0/${members.length}`}</span>
                    </div>
                  )
                })}
              </div>

              <div className="chatter">
                {recent.length === 0
                  ? <span className="chatter-empty">No transcript yet.</span>
                  : recent.map((m) => (
                    <span className="chatter-line" key={m.id}>
                      <b>{studentName.get(m.student_id) ?? '—'}</b>{'  '}{m.body}
                    </span>
                  ))}
              </div>
            </button>
          )
        })}
      </div>
    </section>
  )
}
