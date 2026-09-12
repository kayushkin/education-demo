import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import * as api from '../lib/api'
import type { Preset } from '../lib/types'
import { CommandBar } from '../components/CommandBar'
import { ErrorBanner } from '../components/primitives'

export function Setup() {
  const navigate = useNavigate()
  const [presets, setPresets] = useState<Preset[] | null>(null)
  const [presetID, setPresetID] = useState<string>('')
  const [teamCount, setTeamCount] = useState(10)
  const [classSize, setClassSize] = useState(30)
  const [seed, setSeed] = useState('')
  const [plantDrama, setPlantDrama] = useState(true)
  const [speed, setSpeed] = useState(1)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [planted, setPlanted] = useState<string[] | null>(null)

  useEffect(() => {
    api.getPresets()
      .then((p) => { setPresets(p); if (p.length > 0) setPresetID(p[0].id) })
      .catch((e: Error) => setError(e.message))
  }, [])

  async function createAndStart() {
    setBusy(true)
    setError(null)
    setPlanted(null)
    try {
      const parsedSeed = seed.trim() === '' ? undefined : Number(seed.trim())
      const result = await api.createSession({
        preset_id: presetID,
        team_count: teamCount,
        class_size: classSize,
        plant_drama: plantDrama,
        ...(parsedSeed !== undefined && Number.isFinite(parsedSeed) ? { seed: parsedSeed } : {}),
      })
      setPlanted(result.planted)
      await api.startSession(result.session.id, speed)
      navigate(`/?session=${result.session.id}`)
    } catch (e) {
      setError((e as Error).message)
      setBusy(false)
    }
  }

  const perTeam = teamCount > 0 ? (classSize / teamCount) : 0

  return (
    <div className="app">
      <CommandBar />
      <div className="main" style={{ maxWidth: 1180 }}>
        <div className="stage" style={{ margin: '0 0 26px', padding: 0, maxWidth: 680 }}>
          <span className="eyebrow">New session</span>
          <h1>Build a class.</h1>
          <p className="lede">
            Every simulated student is assigned a hidden understanding state per goal before a word is
            said. The agent never sees it — which is what makes the accuracy panel possible later.
          </p>
        </div>

        {error && <ErrorBanner message={error} />}

        <div className="setup-grid">
          <div>
            <div className="eyebrow" style={{ marginBottom: 10 }}>Lesson</div>
            {!presets && <div className="generating" style={{ padding: 30 }}><span className="scan" /><p>Loading lessons…</p></div>}
            <div className="preset-list">
              {presets?.map((p) => (
                <button
                  key={p.id}
                  className={`preset${p.id === presetID ? ' on' : ''}`}
                  onClick={() => setPresetID(p.id)}
                  aria-pressed={p.id === presetID}
                >
                  <div className="t">{p.title}</div>
                  <div className="s">{p.subject} · {p.goals.length} goals</div>
                  {p.id === presetID && (
                    <ol>
                      {p.goals.map((g, i) => (
                        <li key={g}>
                          <strong style={{ color: 'var(--text)' }}>{p.labels[i] ?? `Goal ${i + 1}`}</strong>
                          {' — '}{g}
                        </li>
                      ))}
                    </ol>
                  )}
                </button>
              ))}
            </div>
          </div>

          <div className="knobs">
            <div className="panel" style={{ padding: 15, display: 'flex', flexDirection: 'column', gap: 14 }}>
              <div className="field">
                <label htmlFor="teams">Breakout teams</label>
                <input id="teams" className="input" type="number" min={1} max={20} value={teamCount}
                  onChange={(e) => setTeamCount(Math.max(1, Number(e.target.value) || 1))} />
              </div>
              <div className="field">
                <label htmlFor="size">Class size</label>
                <input id="size" className="input" type="number" min={1} max={120} value={classSize}
                  onChange={(e) => setClassSize(Math.max(1, Number(e.target.value) || 1))} />
              </div>
              <div style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--text-mute)' }}>
                ≈ {perTeam.toFixed(1)} students per room
              </div>
              <div className="field">
                <label htmlFor="speed">Playback speed</label>
                <select id="speed" className="select" value={speed} onChange={(e) => setSpeed(Number(e.target.value))}>
                  <option value={1}>1× — realistic classroom pace</option>
                  <option value={2}>2× — twice as fast</option>
                  <option value={4}>4× — demo pace</option>
                </select>
              </div>
              <div className="field">
                <label htmlFor="seed">Seed <span style={{ textTransform: 'none', letterSpacing: 0 }}>(optional)</span></label>
                <input id="seed" className="input" placeholder="leave blank for a fresh class" value={seed}
                  onChange={(e) => setSeed(e.target.value)} inputMode="numeric" />
              </div>
              <label className="toggle">
                <input type="checkbox" checked={plantDrama} onChange={(e) => setPlantDrama(e.target.checked)} />
                <span className="track" />
                <span>Plant scenarios</span>
              </label>
              <div style={{ fontSize: 11.5, color: 'var(--text-mute)', lineHeight: 1.45 }}>
                Forces a confidently-wrong student and a stranded team into the ground truth, so there is
                always something for the agent to find. The planted list is shown, never hidden.
              </div>

              <button className="btn btn-primary" onClick={() => void createAndStart()} disabled={busy || !presetID}>
                {busy ? 'Creating and starting…' : 'Create and start'}
              </button>
            </div>

            {planted && planted.length > 0 && (
              <div className="panel" style={{ padding: 14 }}>
                <div className="eyebrow" style={{ marginBottom: 8 }}>Planted into the ground truth</div>
                <ul className="planted">{planted.map((p) => <li key={p}>{p}</li>)}</ul>
              </div>
            )}

            {busy && (
              <div className="banner"><span className="glyph">⋯</span>
                <span>Writing {teamCount} transcripts. This takes 30–90 seconds — the dashboard will show the rooms filling as they land.</span>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
