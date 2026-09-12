import type { CSSProperties, ReactNode } from 'react'
import { humanize, stateColor, stateGlyph } from '../lib/display'
import type { Vocabularies } from '../lib/types'

type Size = 'sm' | 'md' | 'lg'

/**
 * One understanding state. Colour is never the only channel: the mark also
 * carries a glyph, a title and — wherever it sits in a strip or a key — a
 * fixed position in the server's worst→best order.
 */
export function StateMark({ vocab, state, size = 'md', hollow = false }: {
  vocab: Vocabularies
  state: string
  size?: Size
  hollow?: boolean
}) {
  const i = vocab.understanding_states.indexOf(state)
  const cls = ['state-mark', size === 'sm' ? 'sm' : size === 'lg' ? 'lg' : '', hollow ? 'hollow' : '']
    .filter(Boolean).join(' ')
  return (
    <span
      className={cls}
      style={{ '--mark': stateColor(i) } as CSSProperties}
      title={humanize(state)}
      aria-label={humanize(state)}
      role="img"
    >
      {stateGlyph(i)}
    </span>
  )
}

/** The legend. Rendered from the vocabulary, in the order the server sent. */
export function StateKey({ vocab }: { vocab: Vocabularies }) {
  return (
    <div className="state-key">
      {vocab.understanding_states.map((s, i) => (
        <span className="state-key-item" key={s}>
          <StateMark vocab={vocab} state={s} size="sm" />
          <span>{humanize(s)}</span>
          <span className="idx">{i + 1}/{vocab.understanding_states.length}</span>
        </span>
      ))}
      <span className="state-key-item">
        <span className="state-mark sm hollow" style={{ '--mark': 'var(--state-unassessed)' } as CSSProperties}>–</span>
        <span>Not yet assessed</span>
      </span>
    </div>
  )
}

/**
 * A team-or-class distribution across the states, drawn worst→best left to
 * right. The unassessed remainder is hatched, not coloured — an absent
 * assessment is the agent declining to assert, which is a different thing from
 * the `unknown` state it does assert.
 */
export function UnderstandingStrip({ vocab, counts, assessed, roster }: {
  vocab: Vocabularies
  counts: Record<string, number>
  assessed: number
  roster: number
}) {
  const total = Math.max(roster, assessed, 1)
  const label = vocab.understanding_states
    .map((s) => `${humanize(s)} ${counts[s] ?? 0}`)
    .concat(`not yet assessed ${Math.max(0, roster - assessed)}`)
    .join(', ')
  return (
    <div className="strip" role="img" aria-label={label} title={label}>
      {vocab.understanding_states.map((s, i) => {
        const n = counts[s] ?? 0
        if (n <= 0) return null
        return (
          <span
            key={s}
            className="strip-seg"
            data-idx={i}
            style={{ width: `${(n / total) * 100}%`, background: stateColor(i) }}
          />
        )
      })}
      {roster > assessed && <span className="strip-unassessed" />}
    </div>
  )
}

export function Pill({ tone = 'default', children, title }: {
  tone?: 'default' | 'live' | 'amber' | 'alarm' | 'violet' | 'mute'
  children: ReactNode
  title?: string
}) {
  const cls = tone === 'default' ? 'pill' : `pill pill-${tone}`
  return <span className={cls} title={title}>{children}</span>
}

/** The server's own sentence about what went wrong, rendered verbatim. */
export function ErrorBanner({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div className="banner err" role="alert">
      <span className="glyph">✕</span>
      <span style={{ flex: 1 }}>{message}</span>
      {onRetry && <button className="btn btn-sm btn-ghost" onClick={onRetry}>Retry</button>}
    </div>
  )
}

export function SectionHead({ n, title, hint, right }: {
  n?: string
  title: string
  hint?: ReactNode
  right?: ReactNode
}) {
  return (
    <div className="section-head">
      {n && <span className="section-num">{n}</span>}
      <h2>{title}</h2>
      {hint && <span className="hint">{hint}</span>}
      {right}
    </div>
  )
}
