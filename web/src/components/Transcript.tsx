import { useEffect, useRef } from 'react'
import { clockTime } from '../lib/display'
import type { Message, Student } from '../lib/types'

function isScrollable(el: Element): boolean {
  const overflow = getComputedStyle(el).overflowY
  return (overflow === 'auto' || overflow === 'scroll') && el.scrollHeight > el.clientHeight
}

/**
 * Pin a scroll box to its newest line.
 *
 * Deliberately not `scrollIntoView`: that scrolls every scrollable ancestor,
 * so a transcript inside a drawer dragged the whole drawer to the bottom and
 * hid the alerts and the per-goal breakdown above it — which are the reason
 * the drawer was opened. This moves exactly one container.
 */
function pinToBottom(anchor: HTMLElement | null) {
  let el: Element | null = anchor?.parentElement ?? null
  while (el) {
    if (isScrollable(el)) {
      el.scrollTop = el.scrollHeight
      return
    }
    el = el.parentElement
  }
}

/**
 * A team's conversation. A real person's line is marked — the agent judges it by
 * exactly the same path as a scripted one, but the teacher should know which is
 * which.
 */
export function Transcript({ messages, students, meStudentID, maxHeight, emptyNote }: {
  messages: Message[]
  students: Student[]
  meStudentID?: string
  /** Bound the log to its own scroll box instead of growing the page. */
  maxHeight?: number | string
  emptyNote?: string
}) {
  const byID = new Map(students.map((s) => [s.id, s]))
  const endRef = useRef<HTMLDivElement>(null)
  const count = messages.length

  useEffect(() => { pinToBottom(endRef.current) }, [count])

  if (messages.length === 0) {
    return (
      <div className="empty-note">
        <b>Nothing said yet</b>
        {emptyNote ?? 'Lines appear here the moment anyone in the room speaks.'}
      </div>
    )
  }

  const body = (
    <div className="transcript">
      {messages.map((m, i) => {
        const s = byID.get(m.student_id)
        const same = i > 0 && messages[i - 1].student_id === m.student_id
        const cls = [
          'msg',
          s?.is_human ? 'is-human' : '',
          m.student_id === meStudentID ? 'is-me' : '',
          same ? 'same-speaker' : '',
          i >= messages.length - 1 ? 'fresh' : '',
        ].filter(Boolean).join(' ')
        return (
          <div className={cls} key={m.id}>
            <span className="who" title={`${s?.name ?? '—'} · ${clockTime(m.at)}`}>
              {s?.is_human ? '◈ ' : ''}{s?.name ?? '—'}
            </span>
            <span className="body">{m.body}</span>
          </div>
        )
      })}
      <div ref={endRef} />
    </div>
  )

  if (maxHeight === undefined) return body
  return <div className="transcript-box" style={{ maxHeight }}>{body}</div>
}
