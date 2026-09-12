import { useCallback, useEffect, useRef, useState } from 'react'
import * as api from './api'
import type { Message, SessionState, StreamEvent, Vocabularies } from './types'

/** Keeps per-team transcripts bounded; a long session would otherwise grow forever. */
const MAX_MESSAGES_PER_TEAM = 400

export function useVocabularies(): { vocab: Vocabularies | null; error: string | null } {
  const [vocab, setVocab] = useState<Vocabularies | null>(null)
  const [error, setError] = useState<string | null>(null)
  useEffect(() => {
    let alive = true
    api.getVocabularies()
      .then((v) => { if (alive) setVocab(v) })
      .catch((e: Error) => { if (alive) setError(e.message) })
    return () => { alive = false }
  }, [])
  return { vocab, error }
}

export interface MonitorTick {
  round: number
  teams_checked: number
  duration_ms: number
  error?: string
}

export interface LiveSession {
  state: SessionState | null
  messages: Record<string, Message[]>
  monitor: MonitorTick | null
  /** Alert ids that arrived over the wire this mount — used to animate them in. */
  freshAlertIds: Set<string>
  connected: boolean
  loading: boolean
  error: string | null
  refetch: () => Promise<void>
  /** Optimistically drop a resolved alert; the next refetch confirms it. */
  dropAlert: (alertID: string) => void
}

/**
 * The dashboard's whole data layer: one read of `GET /api/sessions/{id}`, one
 * SSE subscription, and the contract's refetch rule — the assessment grid is
 * never pushed down the wire, so an `assessments` event means refetch.
 */
export function useLiveSession(sessionID: string | null): LiveSession {
  const [state, setState] = useState<SessionState | null>(null)
  const [messages, setMessages] = useState<Record<string, Message[]>>({})
  const [monitor, setMonitor] = useState<MonitorTick | null>(null)
  const [freshAlertIds, setFresh] = useState<Set<string>>(new Set())
  const [connected, setConnected] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const inFlight = useRef(false)

  const refetch = useCallback(async () => {
    if (!sessionID || inFlight.current) return
    inFlight.current = true
    try {
      const next = await api.getSessionState(sessionID)
      setState(next)
      setError(null)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      inFlight.current = false
    }
  }, [sessionID])

  // Initial load: session state, then every team's transcript in parallel so
  // the cards have real chatter on the first paint rather than after the first
  // SSE line.
  useEffect(() => {
    if (!sessionID) { setState(null); setMessages({}); return }
    let alive = true
    setLoading(true)
    setState(null)
    setMessages({})
    setMonitor(null)
    setFresh(new Set())
    api.getSessionState(sessionID)
      .then(async (next) => {
        if (!alive) return
        setState(next)
        setError(null)
        const logs = await Promise.all(
          next.teams.map((t) =>
            api.getTeamMessages(sessionID, t.id)
              .then((m) => [t.id, m] as const)
              .catch(() => [t.id, [] as Message[]] as const)),
        )
        if (!alive) return
        setMessages(Object.fromEntries(logs))
      })
      .catch((e: Error) => { if (alive) setError(e.message) })
      .finally(() => { if (alive) setLoading(false) })
    return () => { alive = false }
  }, [sessionID])

  // The live stream.
  useEffect(() => {
    if (!sessionID) return
    const es = api.openSessionStream(sessionID)
    let opened = false

    es.onopen = () => {
      setConnected(true)
      // A reconnect means we missed events while away; the contract says to
      // refetch state rather than assume continuity.
      if (opened) void refetch()
      opened = true
    }
    es.onerror = () => setConnected(false)

    es.onmessage = (ev: MessageEvent<string>) => {
      let parsed: StreamEvent
      try {
        parsed = JSON.parse(ev.data) as StreamEvent
      } catch {
        return
      }
      switch (parsed.type) {
        case 'message': {
          const m = parsed.data
          setMessages((prev) => {
            const cur = prev[m.team_id] ?? []
            if (cur.some((x) => x.id === m.id)) return prev
            const next = [...cur, m]
            return { ...prev, [m.team_id]: next.slice(-MAX_MESSAGES_PER_TEAM) }
          })
          break
        }
        case 'alert': {
          const a = parsed.data
          setState((prev) => {
            if (!prev) return prev
            if (prev.alerts.some((x) => x.id === a.id)) return prev
            return { ...prev, alerts: [a, ...prev.alerts] }
          })
          setFresh((prev) => new Set(prev).add(a.id))
          break
        }
        case 'assessments':
          // The grid is not on the wire. Go read it.
          void refetch()
          break
        case 'monitor':
          setMonitor(parsed.data)
          break
        case 'session':
          void refetch()
          break
      }
    }

    return () => { es.close(); setConnected(false) }
  }, [sessionID, refetch])

  const dropAlert = useCallback((alertID: string) => {
    setState((prev) => prev ? { ...prev, alerts: prev.alerts.filter((a) => a.id !== alertID) } : prev)
  }, [])

  return { state, messages, monitor, freshAlertIds, connected, loading, error, refetch, dropAlert }
}

/** A ticking clock for relative timestamps, so "12s ago" actually moves. */
export function useNow(intervalMs = 5000): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), intervalMs)
    return () => window.clearInterval(id)
  }, [intervalMs])
  return now
}
