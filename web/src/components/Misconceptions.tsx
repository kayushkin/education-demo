import { useMemo } from 'react'
import type { Goal, Misconception, Team } from '../lib/types'
import { SectionHead } from './primitives'

export function Misconceptions({ items, goals, teams, onOpenTeam }: {
  items: Misconception[]
  goals: Goal[]
  teams: Team[]
  onOpenTeam: (teamID: string) => void
}) {
  const goalLabel = useMemo(() => new Map(goals.map((g) => [g.id, g.short_label])), [goals])
  const teamName = useMemo(() => new Map(teams.map((t) => [t.id, t.name])), [teams])

  const ranked = useMemo(
    () => items.slice().sort((a, b) => b.count - a.count || b.team_ids.length - a.team_ids.length),
    [items],
  )

  return (
    <section className="section">
      <SectionHead
        n="05"
        title="What to reteach"
        hint="Every wrong idea the agent heard, ranked by how often it was said and by how many rooms it reached."
      />
      {ranked.length === 0 ? (
        <div className="panel empty-note">
          <b>Nothing to reteach yet</b>
          Wrong ideas collect here as the agent hears them, with the rooms they spread to.
        </div>
      ) : (
        <div className="misc-list">
          {ranked.map((m) => (
            <article className="misc" key={m.id}>
              <div className="misc-count">
                {m.count}
                <small>SAID</small>
              </div>
              <div>
                <p className="misc-text">“{m.text}”</p>
                <div className="misc-meta">
                  <span className="pill pill-mute">{goalLabel.get(m.goal_id) ?? 'goal'}</span>
                  <span className="pill" style={{ color: m.team_ids.length > 1 ? 'var(--caution-ink)' : undefined }}>
                    {m.team_ids.length} room{m.team_ids.length === 1 ? '' : 's'}
                  </span>
                  {m.team_ids.map((id) => (
                    <button key={id} className="btn btn-sm btn-ghost" onClick={() => onOpenTeam(id)}>
                      {teamName.get(id) ?? id.slice(0, 6)}
                    </button>
                  ))}
                </div>
              </div>
            </article>
          ))}
        </div>
      )}
    </section>
  )
}
