import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import * as api from '../lib/api'
import { sinceLabel } from '../lib/display'
import { useLiveSession, useNow, useVocabularies } from '../lib/hooks'
import type { Accuracy, Progress, Session, TruthCell } from '../lib/types'
import { AccuracyPanel } from '../components/AccuracyPanel'
import { AlertFeed } from '../components/AlertFeed'
import { ClassGrid } from '../components/ClassGrid'
import { CommandBar } from '../components/CommandBar'
import { GoalHeatmap } from '../components/GoalHeatmap'
import { Misconceptions } from '../components/Misconceptions'
import { ProgressPanel } from '../components/ProgressPanel'
import { ErrorBanner } from '../components/primitives'
import { StudentDrawer } from '../components/StudentDrawer'
import { TeamDrawer } from '../components/TeamDrawer'
import { TeamFloor } from '../components/TeamFloor'

const TRUTH_STORAGE_KEY = 'breakout.revealTruth'

export function Dashboard() {
  const [params, setParams] = useSearchParams()
  const { vocab, error: vocabError } = useVocabularies()
  const now = useNow(4000)

  const [sessions, setSessions] = useState<Session[] | null>(null)
  const [sessionsError, setSessionsError] = useState<string | null>(null)

  const selected = params.get('session')
  const live = useLiveSession(selected)
  const { state, refetch } = live

  const [openTeam, setOpenTeam] = useState<string | null>(null)
  const [openStudent, setOpenStudent] = useState<string | null>(null)
  const [busy, setBusy] = useState<string | null>(null)
  // A forced round is ten model calls and has measured over two minutes. The
  // button alone reads as hung at that length, and a teacher who thinks it has
  // hung navigates away and aborts the request mid-flight. An elapsed count
  // shows it is alive.
  const [busySince, setBusySince] = useState<number | null>(null)
  const [busyElapsed, setBusyElapsed] = useState(0)
  const [actionError, setActionError] = useState<string | null>(null)

  const [progress, setProgress] = useState<Progress | null>(null)
  const [progressLoading, setProgressLoading] = useState(false)
  const [progressError, setProgressError] = useState<string | null>(null)

  const [reveal, setReveal] = useState(() => localStorage.getItem(TRUTH_STORAGE_KEY) === '1')
  const [accuracy, setAccuracy] = useState<Accuracy | null>(null)
  const [truth, setTruth] = useState<TruthCell[] | null>(null)
  const [proofLoading, setProofLoading] = useState(false)
  const [proofError, setProofError] = useState<string | null>(null)

  const loadSessions = useCallback(() => {
    api.listSessions()
      .then((list) => { setSessions(list); setSessionsError(null) })
      .catch((e: Error) => setSessionsError(e.message))
  }, [])

  useEffect(() => { loadSessions() }, [loadSessions])

  // Default to the newest session rather than making the teacher pick one.
  useEffect(() => {
    if (!selected && sessions && sessions.length > 0) {
      // Prefer a lesson that is actually live. The list is newest-first, and
      // taking [0] blindly landed first-time visitors on an ENDED session
      // whose numbers were stale and half-covered — measured on the live site.
      const liveOne = sessions.find((x) => x.status === 'running' || x.status === 'paused')
      setParams({ session: (liveOne ?? sessions[0]).id }, { replace: true })
    }
  }, [selected, sessions, setParams])

  const rounds = state?.runner?.rounds ?? 0
  const assessmentCount = state?.assessments.length ?? 0
  const currentPhase = state?.session.current_phase ?? 0

  const loadProgress = useCallback(async () => {
    if (!selected) return
    setProgressLoading(true)
    try {
      setProgress(await api.getProgress(selected))
      setProgressError(null)
    } catch (e) {
      setProgressError((e as Error).message)
    } finally {
      setProgressLoading(false)
    }
  }, [selected])

  // The before/after is recomputed from the assessment grid, so it is stale the
  // moment the grid moves — and the grid is never pushed down the wire. Same
  // refetch trigger the ground-truth proof uses. `current_phase` is in there too
  // because understanding is advanced BETWEEN phases: a phase change moves the
  // answer even when no new assessment has landed.
  useEffect(() => {
    if (!selected) { setProgress(null); return }
    void loadProgress()
  }, [selected, rounds, assessmentCount, currentPhase, loadProgress])

  const loadProof = useCallback(async () => {
    if (!selected) return
    setProofLoading(true)
    setProofError(null)
    try {
      const [acc, tru] = await Promise.all([api.getAccuracy(selected), api.getTruth(selected)])
      setAccuracy(acc)
      setTruth(tru)
    } catch (e) {
      setProofError((e as Error).message)
    } finally {
      setProofLoading(false)
    }
  }, [selected])

  // Ground truth is re-scored whenever the grid moves, so the proof never lags
  // the claim it is proving.
  useEffect(() => {
    if (!reveal || !selected) { return }
    void loadProof()
  }, [reveal, selected, rounds, assessmentCount, loadProof])

  useEffect(() => {
    localStorage.setItem(TRUTH_STORAGE_KEY, reveal ? '1' : '0')
    if (!reveal) { setAccuracy(null); setTruth(null); setProofError(null) }
  }, [reveal])

  useEffect(() => {
    if (busySince === null) { setBusyElapsed(0); return }
    const id = window.setInterval(
      () => setBusyElapsed(Math.round((Date.now() - busySince) / 1000)), 1000)
    return () => window.clearInterval(id)
  }, [busySince])

  async function act(label: string, fn: () => Promise<unknown>) {
    setBusy(label)
    setBusySince(Date.now())
    setActionError(null)
    try {
      await fn()
      await refetch()
      loadSessions()
    } catch (e) {
      setActionError((e as Error).message)
    } finally {
      setBusy(null)
      setBusySince(null)
    }
  }

  const totalMessages = useMemo(
    () => Object.values(live.messages).reduce((n, l) => n + l.length, 0),
    [live.messages],
  )

  // --- shells ------------------------------------------------------------

  if (vocabError) {
    return <Shell><div className="main"><ErrorBanner message={vocabError} onRetry={() => location.reload()} /></div></Shell>
  }
  if (!vocab) {
    return <Shell><div className="main"><div className="generating"><span className="scan" /><p>Loading vocabularies…</p></div></div></Shell>
  }
  if (sessionsError) {
    return <Shell><div className="main"><ErrorBanner message={sessionsError} onRetry={loadSessions} /></div></Shell>
  }
  if (sessions && sessions.length === 0) {
    return <Shell><EmptyStage /></Shell>
  }
  if (!state) {
    return (
      <Shell>
        <div className="main">
          {live.error && <ErrorBanner message={live.error} onRetry={() => void refetch()} />}
          <div className="generating"><span className="scan" /><p>Opening the session…</p></div>
        </div>
      </Shell>
    )
  }

  const s = state.session
  const runner = state.runner
  // Every enum the UI *renders* comes off GET /api/vocabularies. Session status
  // is the one the UI also *branches* on — which lifecycle button is legal is a
  // fact about what each status means, and an ordered list cannot carry that.
  // The status is still displayed as the server's own word, never a local copy.
  const isSetup = s.status === 'setup'
  const isEnded = s.status === 'ended'
  // Ten parallel model calls write the transcripts before the runner exists.
  // The session is already `running`; nothing has been said yet.
  const generating = !isSetup && !isEnded && runner === null && totalMessages === 0

  return (
    <Shell
      bar={
        <>
          <div className="cmdbar-title hide-sm">
            <span className="t">{s.title}</span>
            <span className="s">{s.subject}</span>
          </div>

          <div className="cmdbar-spacer" />

          <div className="readout hide-sm">
            <span className={`pill ${s.status === 'running' ? 'pill-live' : s.status === 'paused' ? 'pill-amber' : 'pill-mute'}`}>
              {live.connected && s.status === 'running' && <i className="dot dot-pulse" />}
              {s.status}
            </span>
            <span className="sep" />
            {/* A progress number read at phase 1 of 3 is not an end-of-lesson
                number, so the bar always says which act is playing. */}
            <span title="The act of the lesson being played now. Understanding is advanced between acts.">
              phase <b>{s.current_phase}</b>/<b>{s.phase_count}</b>
            </span>
            {runner && (
              <>
                <span className="sep" />
                <span>round <b>{runner.rounds}</b></span>
                <span>queue <b>{runner.script_left}</b></span>
                {live.monitor && <span>{live.monitor.teams_checked} rooms · {(live.monitor.duration_ms / 1000).toFixed(1)}s</span>}
                {runner.last_round_at && <span>{sinceLabel(runner.last_round_at, now)}</span>}
              </>
            )}
            {!state.agent_ready && <span className="pill pill-amber" title={state.agent_error}>participation only</span>}
          </div>

          <div className="cmdbar-group">
            {sessions && sessions.length > 1 && (
              <select
                className="select"
                style={{ width: 'auto', maxWidth: 190, fontSize: 12 }}
                value={s.id}
                onChange={(e) => setParams({ session: e.target.value })}
              >
                {sessions.map((x) => <option key={x.id} value={x.id}>{x.title} · {x.status}</option>)}
              </select>
            )}
            {isSetup && (
              <button className="btn btn-primary" disabled={busy !== null}
                onClick={() => void act('start', () => api.startSession(s.id, 1))}>
                {busy === 'start' ? 'starting…' : 'Start lesson'}
              </button>
            )}
            {s.status === 'running' && (
              <button className="btn" disabled={busy !== null}
                onClick={() => void act('pause', () => api.pauseSession(s.id))}>Pause</button>
            )}
            {s.status === 'paused' && (
              <button className="btn btn-primary" disabled={busy !== null}
                onClick={() => void act('resume', () => api.resumeSession(s.id))}>Resume</button>
            )}
            {!isSetup && !isEnded && (
              <button className="btn" disabled={busy !== null}
                title="Run a monitoring round now instead of waiting for the tick. One model call per group, so it can take a couple of minutes."
                onClick={() => void act('assess', () => api.assessNow(s.id))}>
                {busy === 'assess' ? `reading rooms… ${busyElapsed}s` : 'Assess now'}
              </button>
            )}
            {!isEnded && (
              <button className="btn btn-ghost btn-danger" disabled={busy !== null}
                onClick={() => void act('end', () => api.endSession(s.id))}>End</button>
            )}
            <label className="toggle" title="Score the agent against the simulation's hidden ground truth">
              <input type="checkbox" checked={reveal} onChange={(e) => setReveal(e.target.checked)} />
              <span className="track" />
              <span className="toggle-label">Reveal ground truth</span>
            </label>
          </div>
        </>
      }
    >
      <div className="main">
        {actionError && <ErrorBanner message={actionError} />}
        {state.agent_error && (
          <div className="banner warn">
            <span className="glyph">⚠</span>
            <span>{state.agent_error} — sessions still run on fallback transcripts with participation-only monitoring.</span>
          </div>
        )}
        {runner?.last_error && (
          <div className="banner warn"><span className="glyph">⚠</span><span>{runner.last_error}</span></div>
        )}

        {isSetup && <NotStarted onStart={() => void act('start', () => api.startSession(s.id, 1))} busy={busy === 'start'} />}
        {generating && <Generating teamCount={state.teams.length} />}

        {!isSetup && !generating && (
          <div className="deck">
            <div className="rail">
              <AlertFeed
                vocab={vocab}
                alerts={state.alerts}
                teams={state.teams}
                students={state.students}
                goals={state.goals}
                freshAlertIds={live.freshAlertIds}
                now={now}
                onOpenTeam={setOpenTeam}
                onOpenStudent={setOpenStudent}
                onResolved={live.dropAlert}
              />
            </div>

            <div style={{ minWidth: 0 }}>
              <TeamFloor
                vocab={vocab}
                teams={state.teams}
                goals={state.goals}
                students={state.students}
                cells={state.team_goal_cells}
                alerts={state.alerts}
                messages={live.messages}
                onOpenTeam={setOpenTeam}
              />
              <ProgressPanel
                vocab={vocab}
                progress={progress}
                teams={state.teams}
                loading={progressLoading}
                error={progressError}
                reveal={reveal}
                onRetry={() => void loadProgress()}
                onOpenStudent={setOpenStudent}
              />
              <GoalHeatmap
                vocab={vocab}
                goals={state.goals}
                teams={state.teams}
                cells={state.team_goal_cells}
                rollups={state.goal_rollups}
                onOpenTeam={setOpenTeam}
              />
              <Misconceptions
                items={state.misconceptions}
                goals={state.goals}
                teams={state.teams}
                onOpenTeam={setOpenTeam}
              />
              <ClassGrid
                vocab={vocab}
                students={state.students}
                teams={state.teams}
                goals={state.goals}
                assessments={state.assessments}
                truth={truth}
                onOpenStudent={setOpenStudent}
              />
              {reveal && (
                <AccuracyPanel
                  vocab={vocab}
                  accuracy={accuracy}
                  loading={proofLoading}
                  error={proofError}
                  onRetry={() => void loadProof()}
                />
              )}
            </div>
          </div>
        )}
      </div>

      {openTeam && (
        <TeamDrawer
          vocab={vocab}
          state={state}
          teamID={openTeam}
          messages={live.messages[openTeam] ?? []}
          truth={truth}
          now={now}
          onClose={() => setOpenTeam(null)}
          onOpenStudent={(id) => { setOpenTeam(null); setOpenStudent(id) }}
        />
      )}
      {openStudent && (
        <StudentDrawer
          vocab={vocab}
          state={state}
          studentID={openStudent}
          truth={truth}
          now={now}
          onClose={() => setOpenStudent(null)}
          onOpenTeam={(id) => { setOpenStudent(null); setOpenTeam(id) }}
        />
      )}
    </Shell>
  )
}

function Shell({ bar, children }: { bar?: React.ReactNode; children: React.ReactNode }) {
  return (
    <div className="app">
      <CommandBar>{bar}</CommandBar>
      {children}
    </div>
  )
}

function EmptyStage() {
  return (
    <div className="stage">
      <span className="eyebrow">No session yet</span>
      <h1>You cannot be in every room at once.</h1>
      <p className="lede">
        Split a class into breakout teams and an agent listens to every room at the same time,
        reads what each student actually understands, and tells you which room to walk to next.
      </p>
      <ol className="steps">
        <li><b>Create a session</b><span>Pick a lesson, a team count and a class size. Each simulated student is given a hidden understanding state the agent never sees.</span></li>
        <li><b>Start it</b><span>Every room begins talking at once. Transcripts take 30–90 seconds to write, then the conversation drips in at classroom pace.</span></li>
        <li><b>Watch the feed</b><span>The agent reads every room on a tick and raises alerts — the loudest being a student confidently teaching an error to teammates who cannot tell.</span></li>
        <li><b>Prove it</b><span>Flip “Reveal ground truth” and the agent's every call is scored against the hidden states. That only works because this classroom is synthetic.</span></li>
      </ol>
      <Link className="btn btn-primary" to="/setup" style={{ display: 'inline-block' }}>Create a session</Link>
    </div>
  )
}

function NotStarted({ onStart, busy }: { onStart: () => void; busy: boolean }) {
  return (
    <div className="stage" style={{ marginTop: '5vh' }}>
      <span className="eyebrow">Ready</span>
      <h1>The rooms are set. Nobody has spoken yet.</h1>
      <p className="lede">
        Starting the lesson writes every team's conversation, then plays it back at classroom pace
        while the agent listens. Writing the transcripts takes 30–90 seconds.
      </p>
      <button className="btn btn-primary" onClick={onStart} disabled={busy}>
        {busy ? 'Starting…' : 'Start lesson'}
      </button>
    </div>
  )
}

function Generating({ teamCount }: { teamCount: number }) {
  return (
    <div className="generating">
      <span className="pill pill-live"><i className="dot dot-pulse" />writing transcripts</span>
      <h2>{teamCount} rooms are being written right now.</h2>
      <p>
        Each team's conversation is generated in full before playback begins — {teamCount} model calls
        running in parallel. This takes <b>30–90 seconds</b>, and then the rooms start talking and the
        first assessments land about 40 seconds later.
      </p>
      <span className="scan" />
      <div className="room-skel">
        {Array.from({ length: teamCount }, (_, i) => <div className="skel" key={i} style={{ animationDelay: `${i * 90}ms` }} />)}
      </div>
    </div>
  )
}
