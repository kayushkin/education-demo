import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import * as api from '../lib/api'
import type { Message, Room, Session, Student, StreamEvent } from '../lib/types'
import { CommandBar } from '../components/CommandBar'
import { ErrorBanner } from '../components/primitives'
import { Transcript } from '../components/Transcript'

interface Membership {
  token: string
  student: Student
  team_id: string
  team_name: string
}

const membershipKey = (sessionID: string) => `breakout.membership.${sessionID}`

function loadMembership(sessionID: string): Membership | null {
  try {
    const raw = localStorage.getItem(membershipKey(sessionID))
    return raw ? (JSON.parse(raw) as Membership) : null
  } catch {
    return null
  }
}

export function Join() {
  const { sessionId } = useParams()
  const navigate = useNavigate()

  const [sessions, setSessions] = useState<Session[] | null>(null)
  const [listError, setListError] = useState<string | null>(null)

  useEffect(() => {
    if (sessionId) return
    api.listSessions()
      .then(setSessions)
      .catch((e: Error) => setListError(e.message))
  }, [sessionId])

  if (sessionId) return <BreakoutRoom sessionID={sessionId} />

  return (
    <div className="app">
      <CommandBar />
      <div className="join-form">
        <div>
          <span className="eyebrow">Join a breakout room</span>
          <h1 style={{ fontSize: 24, margin: '8px 0 8px', letterSpacing: '-.025em' }}>Sit in with a team.</h1>
          <p style={{ color: 'var(--ink-soft)', fontSize: 14, lineHeight: 1.55, margin: 0 }}>
            You will be added to the room as yourself. Everything you say is read by the same agent that
            reads the simulated students — say something confidently wrong and watch the teacher's board.
          </p>
        </div>
        {listError && <ErrorBanner message={listError} />}
        {!sessions && !listError && <div className="generating" style={{ padding: 28 }}><span className="scan" /><p>Finding lessons…</p></div>}
        {sessions?.length === 0 && (
          <div className="empty-note"><b>No lessons running</b>Ask the teacher to start one, then reload.</div>
        )}
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          {sessions?.map((s) => (
            <button key={s.id} className="preset" onClick={() => navigate(`/join/${s.id}`)}>
              <div className="t">{s.title}</div>
              <div className="s">{s.subject} · {s.status}</div>
            </button>
          ))}
        </div>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------

function BreakoutRoom({ sessionID }: { sessionID: string }) {
  const [membership, setMembership] = useState<Membership | null>(() => loadMembership(sessionID))
  const [students, setStudents] = useState<Student[]>([])
  const [messages, setMessages] = useState<Message[]>([])
  const [session, setSession] = useState<Session | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [sendError, setSendError] = useState<string | null>(null)
  const [draft, setDraft] = useState('')
  const [sending, setSending] = useState(false)
  const [connected, setConnected] = useState(false)

  const teamID = membership?.team_id ?? null
  const knownIDs = useRef(new Set<string>())

  const loadRoster = useCallback(async () => {
    const st = await api.getSessionState(sessionID)
    setStudents(st.students)
    setSession(st.session)
    knownIDs.current = new Set(st.students.map((s) => s.id))
  }, [sessionID])

  // Roster and backlog, once we know which room we are in.
  useEffect(() => {
    if (!teamID) {
      api.getSessionState(sessionID).then((st) => setSession(st.session)).catch(() => {})
      return
    }
    let alive = true
    Promise.all([loadRoster(), api.getTeamMessages(sessionID, teamID)])
      .then(([, log]) => { if (alive) setMessages(log) })
      .catch((e: Error) => { if (alive) setError(e.message) })
    return () => { alive = false }
  }, [sessionID, teamID, loadRoster])

  // Live: only this room's lines matter to a student.
  useEffect(() => {
    if (!teamID) return
    const es = api.openSessionStream(sessionID)
    es.onopen = () => setConnected(true)
    es.onerror = () => setConnected(false)
    es.onmessage = (ev: MessageEvent<string>) => {
      let parsed: StreamEvent
      try { parsed = JSON.parse(ev.data) as StreamEvent } catch { return }
      if (parsed.type !== 'message') return
      const m = parsed.data
      if (m.team_id !== teamID) return
      // Somebody new joined the room; go learn their name.
      if (!knownIDs.current.has(m.student_id)) void loadRoster()
      setMessages((prev) => (prev.some((x) => x.id === m.id) ? prev : [...prev, m]))
    }
    return () => { es.close(); setConnected(false) }
  }, [sessionID, teamID, loadRoster])

  async function send() {
    const body = draft.trim()
    if (!body || !membership) return
    setSending(true)
    setSendError(null)
    try {
      await api.postTeamMessage(sessionID, membership.team_id, membership.token, body)
      setDraft('')
    } catch (e) {
      setSendError((e as Error).message)
    } finally {
      setSending(false)
    }
  }

  function leave() {
    localStorage.removeItem(membershipKey(sessionID))
    setMembership(null)
    setMessages([])
  }

  if (!membership) {
    return (
      <JoinForm
        sessionID={sessionID}
        sessionTitle={session?.title}
        onJoined={(m) => {
          localStorage.setItem(membershipKey(sessionID), JSON.stringify(m))
          setMembership(m)
        }}
      />
    )
  }

  return (
    <div className="app">
      <CommandBar />
      <div className="room">
        <header className="room-head">
          <div style={{ minWidth: 0 }}>
            <div className="t">{membership.team_name}</div>
            <div className="s">{session?.title ?? 'lesson'} · you are {membership.student.name}</div>
          </div>
          <span className={`pill ${connected ? 'pill-live' : 'pill-mute'}`} style={{ marginLeft: 'auto' }}>
            {connected && <i className="dot dot-pulse" />}{connected ? 'live' : 'reconnecting'}
          </span>
          <button className="btn btn-sm btn-ghost" onClick={leave}>Leave</button>
        </header>

        {error && <div style={{ padding: '10px 16px 0' }}><ErrorBanner message={error} /></div>}

        <div className="room-log">
          <Transcript
            messages={messages}
            students={students}
            meStudentID={membership.student.id}
            emptyNote="Your team has not started talking yet. Say something to get it going."
          />
        </div>

        {sendError && <div style={{ padding: '0 16px' }}><ErrorBanner message={sendError} /></div>}

        <form
          className="room-compose"
          onSubmit={(e) => { e.preventDefault(); void send() }}
        >
          <input
            className="input"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            placeholder={`Say something as ${membership.student.name}…`}
            enterKeyHint="send"
            autoComplete="off"
            maxLength={600}
          />
          <button className="btn btn-primary" type="submit" disabled={sending || draft.trim() === ''}>
            {sending ? '…' : 'Send'}
          </button>
        </form>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------

function JoinForm({ sessionID, sessionTitle, onJoined }: {
  sessionID: string
  sessionTitle?: string
  onJoined: (m: Membership) => void
}) {
  const [rooms, setRooms] = useState<Room[] | null>(null)
  const [teamID, setTeamID] = useState<string>('')
  const [name, setName] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const load = useCallback(() => {
    api.getRooms(sessionID)
      .then(setRooms)
      .catch((e: Error) => setError(e.message))
  }, [sessionID])

  useEffect(load, [load])

  async function join(withTeam: string | undefined) {
    const display = name.trim()
    if (!display) { setError('Enter a display name first.'); return }
    setBusy(true)
    setError(null)
    try {
      const res = await api.joinSession(sessionID, {
        display_name: display,
        ...(withTeam ? { team_id: withTeam } : {}),
      })
      const room = rooms?.find((r) => r.team_id === res.student.team_id)
      onJoined({
        token: res.token,
        student: res.student,
        team_id: res.student.team_id,
        team_name: room?.team_name ?? 'Your room',
      })
    } catch (e) {
      setError((e as Error).message)
      // A name clash or a vanished team both change what the picker should show.
      load()
      setBusy(false)
    }
  }

  return (
    <div className="app">
      <CommandBar />
      <div className="join-form">
        <div>
          <span className="eyebrow">{sessionTitle ?? 'Lesson'}</span>
          <h1 style={{ fontSize: 24, margin: '8px 0 6px', letterSpacing: '-.025em' }}>Pick a room.</h1>
          <p style={{ color: 'var(--ink-soft)', fontSize: 13.5, lineHeight: 1.55, margin: 0 }}>
            You join as a new member — you are not replacing anyone. The agent reads your words the same
            way it reads everyone else's.
          </p>
        </div>

        {error && <ErrorBanner message={error} onRetry={load} />}

        <div className="field">
          <label htmlFor="dn">Your display name</label>
          <input
            id="dn"
            className="input"
            style={{ fontSize: 16 }}
            value={name}
            maxLength={40}
            placeholder="e.g. Sam"
            onChange={(e) => setName(e.target.value)}
            autoComplete="off"
          />
          <span style={{ fontSize: 11, color: 'var(--ink-faint)', fontFamily: 'var(--font-num)' }}>
            {name.trim().length}/40 · must be unique inside the room
          </span>
        </div>

        <div>
          <div className="eyebrow" style={{ marginBottom: 8 }}>Rooms</div>
          {!rooms && !error && <div className="generating" style={{ padding: 24 }}><span className="scan" /><p>Looking at the rooms…</p></div>}
          <div className="seat-grid">
            {rooms?.map((r) => (
              <button
                key={r.team_id}
                className={`seat${teamID === r.team_id ? ' on' : ''}`}
                onClick={() => setTeamID(r.team_id)}
                aria-pressed={teamID === r.team_id}
                title={r.members.join(', ')}
              >
                <div className="n">{r.team_name}</div>
                <div className="tm">
                  {r.members.length} in room{r.humans > 0 ? ` · ${r.humans} human` : ''}
                </div>
                <div className="tm" style={{ color: 'var(--ink-faint)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                  {r.members.join(', ') || 'empty'}
                </div>
              </button>
            ))}
          </div>
        </div>

        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
          <button className="btn btn-primary" disabled={busy || !teamID || name.trim() === ''}
            onClick={() => void join(teamID)}>
            {busy ? 'Joining…' : teamID ? 'Join this room' : 'Pick a room'}
          </button>
          <button className="btn" disabled={busy || name.trim() === ''} onClick={() => void join(undefined)}
            title="The server puts you in the smallest room">
            Just put me somewhere
          </button>
        </div>
      </div>
    </div>
  )
}
