import type { Vocabularies } from './types'

// Presentation for server-owned enums.
//
// Nothing here hardcodes an enum member. Colour and glyph are chosen by the
// value's POSITION in the list the server sent, so a renamed or added state
// still renders — and `understanding_states` arriving worst-to-best is the only
// ordering fact the UI relies on.

const STATE_RAMP = ['var(--state-0)', 'var(--state-1)', 'var(--state-2)', 'var(--state-3)']
const SEVERITY_RAMP = ['var(--sev-0)', 'var(--sev-1)', 'var(--sev-2)']

/** Glyphs read worst→best: struck out, unknown, half, full. */
const STATE_GLYPHS = ['✕', '?', '◐', '●']

export const UNASSESSED_PCT = -1

export function stateIndex(vocab: Vocabularies, state: string): number {
  return vocab.understanding_states.indexOf(state)
}

/** Colour for a state, by its rank in the worst→best order. */
export function stateColor(index: number): string {
  if (index < 0) return 'var(--state-x)'
  return STATE_RAMP[Math.min(index, STATE_RAMP.length - 1)]
}

export function stateGlyph(index: number): string {
  if (index < 0) return '–'
  return STATE_GLYPHS[index] ?? String(index + 1)
}

export function severityIndex(vocab: Vocabularies, severity: string): number {
  return vocab.severities.indexOf(severity)
}

export function severityColor(index: number): string {
  if (index < 0) return 'var(--sev-0)'
  return SEVERITY_RAMP[Math.min(index, SEVERITY_RAMP.length - 1)]
}

/**
 * Urgency rank for an alert. Higher is more urgent.
 *
 * Severity dominates. Within one severity, `alert_kinds` arrives most-urgent
 * first — `confidently_wrong` leads it — so the kind's own position breaks the
 * tie. Neither ordering is written down here; both come off the vocabulary.
 */
export function alertRank(vocab: Vocabularies, kind: string, severity: string): number {
  const sev = severityIndex(vocab, severity)
  const kinds = vocab.alert_kinds
  const k = kinds.indexOf(kind)
  const kindScore = k < 0 ? 0 : kinds.length - k
  return (sev < 0 ? 0 : sev + 1) * 1000 + kindScore
}

/** Is this the single most urgent class of problem the product knows about? */
export function isTopPriority(vocab: Vocabularies, kind: string, severity: string): boolean {
  const worstSeverity = vocab.severities[vocab.severities.length - 1]
  const worstKind = vocab.alert_kinds[0]
  return severity === worstSeverity && kind === worstKind
}

/** `confidently_wrong` → `Confidently wrong`. Slug formatting, at the edge. */
export function humanize(slug: string): string {
  if (!slug) return ''
  const s = slug.replace(/_/g, ' ')
  return s.charAt(0).toUpperCase() + s.slice(1)
}

export function pct(n: number, digits = 0): string {
  return `${n.toFixed(digits)}%`
}

/**
 * Go marshals an unset time.Time as `0001-01-01T00:00:00Z`, and the runner sends
 * exactly that for `last_round_at` until its first monitoring round lands.
 * Parsing it succeeds, so the only way to catch it is to reject the date — which
 * is worth doing, because otherwise the status line reads "17753280h ago".
 */
const EARLIEST_REAL_TIME = Date.UTC(2000, 0, 1)

function parseServerTime(iso: string | undefined): number | null {
  if (!iso) return null
  const t = new Date(iso).getTime()
  if (Number.isNaN(t) || t < EARLIEST_REAL_TIME) return null
  return t
}

export function clockTime(iso: string): string {
  const t = parseServerTime(iso)
  if (t === null) return '--:--'
  return new Date(t).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false })
}

export function sinceLabel(iso: string | undefined, now: number): string {
  const t = parseServerTime(iso)
  if (t === null) return 'not yet'
  const secs = Math.max(0, Math.round((now - t) / 1000))
  if (secs < 60) return `${secs}s ago`
  const mins = Math.floor(secs / 60)
  if (mins < 60) return `${mins}m ago`
  return `${Math.floor(mins / 60)}h ago`
}
