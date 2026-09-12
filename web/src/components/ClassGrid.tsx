import { useMemo, type CSSProperties } from 'react'
import { humanize, latestTruthByPair, pairKey, stateColor, stateGlyph } from '../lib/display'
import type { Assessment, Goal, Student, Team, TruthCell, Vocabularies } from '../lib/types'
import { SectionHead } from './primitives'

interface Props {
  vocab: Vocabularies
  students: Student[]
  teams: Team[]
  goals: Goal[]
  assessments: Assessment[]
  /** Only present when the operator has revealed the simulation's hidden variable. */
  truth: TruthCell[] | null
  onOpenStudent: (studentID: string) => void
}

export function ClassGrid({ vocab, students, teams, goals, assessments, truth, onOpenStudent }: Props) {
  const inferred = useMemo(() => {
    const m = new Map<string, Assessment>()
    for (const a of assessments) m.set(pairKey(a.student_id, a.goal_id), a)
    return m
  }, [assessments])

  const truthMap = useMemo(() => {
    if (!truth) return null
    // Truth arrives one row PER PHASE. The overlay has to show the same phase
    // the accuracy panel scores against — the latest — or it would ring cells
    // the scoreboard counted as correct.
    const latest = latestTruthByPair(truth)
    return new Map([...latest].map(([k, t]) => [k, t.state] as const))
  }, [truth])

  const ordered = useMemo(() => {
    const rank = new Map(teams.map((t, i) => [t.id, i]))
    return students.slice().sort((a, b) => {
      const d = (rank.get(a.team_id) ?? 0) - (rank.get(b.team_id) ?? 0)
      return d !== 0 ? d : a.name.localeCompare(b.name)
    })
  }, [students, teams])

  const teamName = useMemo(() => new Map(teams.map((t) => [t.id, t.name])), [teams])

  const mismatches = useMemo(() => {
    if (!truthMap) return 0
    let n = 0
    for (const [k, state] of truthMap) {
      const a = inferred.get(k)
      if (a && a.state !== state) n++
    }
    return n
  }, [truthMap, inferred])

  return (
    <section className="section">
      <SectionHead
        n="05"
        title="Every student"
        hint={
          truthMap
            ? <>Inferred state fills each cell; the wedge in the corner is the hidden truth. <strong style={{ color: 'var(--alarm)' }}>{mismatches} ringed cells</strong> are where the agent was wrong.</>
            : <>One row per student, one column per goal. A hatched cell means the agent has not heard enough from that student on that goal to say anything.</>
        }
      />
      <div className="panel" style={{ padding: 14 }}>
        <div className="grid-wrap">
          <table className="classgrid">
            <thead>
              <tr>
                <th className="corner">Student</th>
                {goals.map((g) => <th key={g.id} title={g.text}>{g.short_label}</th>)}
              </tr>
            </thead>
            <tbody>
              {ordered.map((s, i) => {
                const newTeam = i === 0 || ordered[i - 1].team_id !== s.team_id
                return (
                  <tr key={s.id} className={newTeam && i > 0 ? 'team-sep' : undefined}>
                    <th>
                      <button className="who" onClick={() => onOpenStudent(s.id)}>
                        {s.is_human && <span style={{ color: 'var(--violet)' }} aria-label="real person">◈ </span>}
                        {s.name}
                        <span className="team-tag">{teamName.get(s.team_id)}</span>
                      </button>
                    </th>
                    {goals.map((g) => {
                      const key = pairKey(s.id, g.id)
                      const a = inferred.get(key)
                      const t = truthMap?.get(key)
                      const ai = a ? vocab.understanding_states.indexOf(a.state) : -1
                      const ti = t ? vocab.understanding_states.indexOf(t) : -1
                      const miss = Boolean(a && t && a.state !== t)
                      const title = [
                        `${s.name} · ${g.short_label}`,
                        a ? `agent says: ${humanize(a.state)} (confidence ${a.confidence.toFixed(2)})` : 'agent has not asserted anything here',
                        t ? `truth: ${humanize(t)}` : truthMap ? (s.is_human ? 'no ground truth — a real person' : 'no truth for this pair') : '',
                      ].filter(Boolean).join('\n')
                      return (
                        <td key={g.id}>
                          <button
                            className={`gcell${a ? '' : ' none'}${miss ? ' miss' : ''}`}
                            onClick={() => onOpenStudent(s.id)}
                            title={title}
                            style={{ background: a ? stateColor(ai) : undefined } as CSSProperties}
                          >
                            {a ? stateGlyph(ai) : '–'}
                            {t && <span className="truth-wedge" style={{ '--truth': stateColor(ti) } as CSSProperties} />}
                          </button>
                        </td>
                      )
                    })}
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      </div>
    </section>
  )
}
