import type {
  Accuracy, Alert, CreateSessionResult, Health, JoinResult, Message,
  Preset, Room, Session, SessionState, TruthCell, Vocabularies,
} from './types'

// ---------------------------------------------------------------------------
// The one place that knows where the API lives.
//
// import.meta.env.BASE_URL is the path this app is published under — Vite sets
// it from `base` in vite.config.ts, which is '/education-demo/'. In production
// the Go server mounts BOTH the built assets and the API under that prefix, so
// every request must carry it or it 404s.
//
// `vite dev` is the single exception: vite serves the app under the base but
// does not serve the API at all, and the proxy rule in vite.config.ts matches
// the bare '/api' prefix, which never sees the base. So in dev the API root is
// '/'. That is the whole difference, stated once, here.
// ---------------------------------------------------------------------------
const apiRoot = import.meta.env.DEV ? '/' : import.meta.env.BASE_URL

/** Absolute URL for an API path. Always use this; never write a literal path. */
export function apiUrl(path: string): string {
  const tail = path.startsWith('/') ? path.slice(1) : path
  return apiRoot.endsWith('/') ? apiRoot + tail : `${apiRoot}/${tail}`
}

/** The router basename, from the same source as the API prefix. */
export const routerBasename = import.meta.env.BASE_URL.replace(/\/$/, '')

/**
 * An error the server described. `error` is the server's own sentence and is
 * rendered verbatim — the UI must not reword what went wrong.
 */
export class ApiError extends Error {
  readonly reason: string
  readonly status: number
  constructor(status: number, reason: string, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.reason = reason
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(apiUrl(path), {
    ...init,
    headers: init?.body ? { 'Content-Type': 'application/json', ...init?.headers } : init?.headers,
  })
  if (!res.ok) {
    let reason = `http_${res.status}`
    let message = `${res.status} ${res.statusText}`
    try {
      const body = await res.json()
      if (body && typeof body === 'object') {
        if (typeof body.reason === 'string') reason = body.reason
        if (typeof body.error === 'string') message = body.error
      }
    } catch {
      // Not JSON. Keep the status line — better than inventing a sentence.
    }
    throw new ApiError(res.status, reason, message)
  }
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

// -- meta -------------------------------------------------------------------

export const getHealth = () => request<Health>('api/health')
export const getVocabularies = () => request<Vocabularies>('api/vocabularies')
export const getPresets = () => request<Preset[]>('api/presets')

// -- sessions ---------------------------------------------------------------

export interface CreateSessionBody {
  preset_id?: string
  title?: string
  subject?: string
  goals?: string[]
  goal_labels?: string[]
  team_count?: number
  class_size?: number
  seed?: number
  plant_drama?: boolean
}

export const listSessions = () => request<Session[]>('api/sessions')
export const getSessionState = (id: string) => request<SessionState>(`api/sessions/${id}`)

export const createSession = (body: CreateSessionBody) =>
  request<CreateSessionResult>('api/sessions', { method: 'POST', body: JSON.stringify(body) })

export const startSession = (id: string, speed: number) =>
  request<unknown>(`api/sessions/${id}/start`, { method: 'POST', body: JSON.stringify({ speed }) })

export const pauseSession = (id: string) => request<unknown>(`api/sessions/${id}/pause`, { method: 'POST' })
export const resumeSession = (id: string) => request<unknown>(`api/sessions/${id}/resume`, { method: 'POST' })
export const endSession = (id: string) => request<unknown>(`api/sessions/${id}/end`, { method: 'POST' })
export const assessNow = (id: string) => request<unknown>(`api/sessions/${id}/assess`, { method: 'POST' })

// -- accuracy (only meaningful because the classroom is synthetic) -----------

export const getAccuracy = (id: string) => request<Accuracy>(`api/sessions/${id}/accuracy`)
export const getTruth = (id: string) => request<TruthCell[]>(`api/sessions/${id}/truth`)

// -- breakout rooms ---------------------------------------------------------

export const getRooms = (id: string) => request<Room[]>(`api/sessions/${id}/rooms`)

/**
 * Join as a real person. This ADDS a student to the team — it does not take
 * over a simulated one, because the agent would then read the previous
 * occupant's words as the newcomer's. Omitting `team_id` picks the smallest room.
 */
export const joinSession = (id: string, body: { display_name: string; team_id?: string }) =>
  request<JoinResult>(`api/sessions/${id}/join`, { method: 'POST', body: JSON.stringify(body) })

export const getTeamMessages = (id: string, teamID: string) =>
  request<Message[]>(`api/sessions/${id}/teams/${teamID}/messages`)

export const postTeamMessage = (id: string, teamID: string, token: string, body: string) =>
  request<Message>(`api/sessions/${id}/teams/${teamID}/messages`, {
    method: 'POST',
    body: JSON.stringify({ token, body }),
  })

export const resolveAlert = (alertID: string) =>
  request<Alert>(`api/alerts/${alertID}/resolve`, { method: 'POST' })

/** The live stream. Caller owns closing it. */
export const openSessionStream = (id: string) => new EventSource(apiUrl(`api/sessions/${id}/stream`))
