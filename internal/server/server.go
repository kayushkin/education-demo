// Package server is the HTTP surface: session lifecycle, the team rooms a
// human can type into, the teacher's dashboard reads, and the SSE stream.
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kayushkin/education-demo/internal/agent"
	"github.com/kayushkin/education-demo/internal/analytics"
	"github.com/kayushkin/education-demo/internal/model"
	"github.com/kayushkin/education-demo/internal/monitor"
	"github.com/kayushkin/education-demo/internal/simulation"
	"github.com/kayushkin/education-demo/internal/store"
)

// Config is everything the server needs that it does not own.
type Config struct {
	Store *store.Store
	Agent *agent.Client
	// BasePath is the path prefix the app is mounted under, e.g.
	// "/education-demo". Routes are registered beneath it so the same binary
	// works at the root in development and under a prefix behind nginx.
	BasePath string
	// WebDir is the built front end to serve. Empty serves the API only.
	WebDir string
	// MonitorInterval is the gap between assessment rounds.
	MonitorInterval time.Duration
	// AgentReady reports whether the model path was reachable at boot, so the
	// UI can say "participation only" plainly instead of showing an empty
	// grid that looks like a bug.
	AgentReady bool
	AgentError string
}

type Server struct {
	cfg Config
	hub *hub

	mu      sync.Mutex
	runners map[string]*runner
}

func New(cfg Config) *Server {
	if cfg.MonitorInterval <= 0 {
		cfg.MonitorInterval = 25 * time.Second
	}
	return &Server{cfg: cfg, hub: newHub(), runners: map[string]*runner{}}
}

// Handler builds the mux. Every API route lives under BasePath + "/api".
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	base := strings.TrimRight(s.cfg.BasePath, "/")
	api := base + "/api"

	mux.HandleFunc("GET "+api+"/health", s.handleHealth)
	mux.HandleFunc("GET "+api+"/vocabularies", s.handleVocabularies)
	mux.HandleFunc("GET "+api+"/presets", s.handlePresets)

	mux.HandleFunc("GET "+api+"/sessions", s.handleListSessions)
	mux.HandleFunc("POST "+api+"/sessions", s.handleCreateSession)
	mux.HandleFunc("GET "+api+"/sessions/{id}", s.handleGetSession)
	mux.HandleFunc("GET "+api+"/sessions/{id}/stream", s.handleStream)
	mux.HandleFunc("GET "+api+"/sessions/{id}/accuracy", s.handleAccuracy)
	mux.HandleFunc("GET "+api+"/sessions/{id}/truth", s.handleTruth)
	mux.HandleFunc("POST "+api+"/sessions/{id}/start", s.handleStart)
	mux.HandleFunc("POST "+api+"/sessions/{id}/pause", s.handlePause)
	mux.HandleFunc("POST "+api+"/sessions/{id}/resume", s.handleResume)
	mux.HandleFunc("POST "+api+"/sessions/{id}/end", s.handleEnd)
	mux.HandleFunc("POST "+api+"/sessions/{id}/assess", s.handleAssessNow)

	mux.HandleFunc("GET "+api+"/sessions/{id}/rooms", s.handleRooms)
	mux.HandleFunc("POST "+api+"/sessions/{id}/join", s.handleJoin)
	mux.HandleFunc("GET "+api+"/sessions/{id}/teams/{teamID}/messages", s.handleTeamMessages)
	mux.HandleFunc("POST "+api+"/sessions/{id}/teams/{teamID}/messages", s.handlePostMessage)
	mux.HandleFunc("POST "+api+"/alerts/{alertID}/resolve", s.handleResolveAlert)

	if s.cfg.WebDir != "" {
		s.mountStatic(mux, base)
	}
	return logging(mux)
}

// ---------- small helpers ----------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write json: %v", err)
	}
}

// writeError sends a machine-readable reason alongside the message. The UI
// shows the message verbatim rather than inventing its own wording.
func writeError(w http.ResponseWriter, status int, reason, msg string) {
	writeJSON(w, status, map[string]string{"reason": reason, "error": msg})
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid request body: "+err.Error())
		return false
	}
	return true
}

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if !strings.HasSuffix(r.URL.Path, "/stream") {
			log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
		}
	})
}

// ---------- meta ----------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"agent_ready": s.cfg.AgentReady,
		"agent_error": s.cfg.AgentError,
	})
}

// handleVocabularies serves every closed set the UI renders, so the front end
// never keeps its own copy to drift from this one.
func (s *Server) handleVocabularies(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"understanding_states": model.UnderstandingStates,
		"alert_kinds":          model.AlertKinds,
		"severities":           model.Severities,
		"session_statuses":     model.SessionStatuses,
	})
}

func (s *Server) handlePresets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, Presets)
}

// ---------- session lifecycle ----------

type createSessionRequest struct {
	PresetID   string   `json:"preset_id"`
	Title      string   `json:"title"`
	Subject    string   `json:"subject"`
	Goals      []string `json:"goals"`
	GoalLabels []string `json:"goal_labels"`
	TeamCount  int      `json:"team_count"`
	ClassSize  int      `json:"class_size"`
	Seed       int64    `json:"seed"`
	PlantDrama *bool    `json:"plant_drama"`
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req createSessionRequest
	if !decodeBody(w, r, &req) {
		return
	}

	bp := simulation.Blueprint{
		Title: req.Title, Subject: req.Subject,
		GoalTexts: req.Goals, GoalLabels: req.GoalLabels,
		TeamCount: req.TeamCount, ClassSize: req.ClassSize,
		Seed: req.Seed, PlantDrama: true,
	}
	if req.PlantDrama != nil {
		bp.PlantDrama = *req.PlantDrama
	}
	if req.PresetID != "" {
		p := presetByID(req.PresetID)
		if p == nil {
			writeError(w, http.StatusBadRequest, "unknown_preset",
				fmt.Sprintf("no preset %q; see GET /api/presets", req.PresetID))
			return
		}
		bp.GoalTexts, bp.GoalLabels = p.Goals, p.Labels
		if bp.Title == "" {
			bp.Title = p.Title
		}
		if bp.Subject == "" {
			bp.Subject = p.Subject
		}
	}
	if bp.TeamCount == 0 {
		bp.TeamCount = 10
	}
	if bp.ClassSize == 0 {
		bp.ClassSize = 30
	}
	if bp.Seed == 0 {
		bp.Seed = time.Now().UnixNano()
	}
	if bp.Title == "" {
		writeError(w, http.StatusBadRequest, "missing_title", "title is required")
		return
	}

	built, err := simulation.Build(bp)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_blueprint", err.Error())
		return
	}

	built.Session.CreatedAt = time.Now().UTC()
	if err := s.cfg.Store.CreateSession(&built.Session); err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
		return
	}
	for i := range built.Goals {
		if err := s.cfg.Store.CreateGoal(&built.Goals[i]); err != nil {
			writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
			return
		}
	}
	for i := range built.Teams {
		if err := s.cfg.Store.CreateTeam(&built.Teams[i]); err != nil {
			writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
			return
		}
	}
	for i := range built.Students {
		if err := s.cfg.Store.CreateStudent(&built.Students[i]); err != nil {
			writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
			return
		}
	}
	for _, t := range built.Truths {
		if err := s.cfg.Store.SetTruth(t); err != nil {
			writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
			return
		}
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"session":  built.Session,
		"goals":    built.Goals,
		"teams":    built.Teams,
		"students": built.Students,
		"planted":  built.Planted,
	})
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	list, err := s.cfg.Store.ListSessions()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// sessionState is the dashboard's single read. One call rather than eight
// keeps the client's reconciliation simple and its first paint fast.
type sessionState struct {
	Session        model.Session            `json:"session"`
	Goals          []model.Goal             `json:"goals"`
	Teams          []model.Team             `json:"teams"`
	Students       []model.Student          `json:"students"`
	Assessments    []model.Assessment       `json:"assessments"`
	Alerts         []model.Alert            `json:"alerts"`
	Misconceptions []model.Misconception    `json:"misconceptions"`
	GoalRollups    []analytics.GoalRollup   `json:"goal_rollups"`
	TeamGoalCells  []analytics.TeamGoalCell `json:"team_goal_cells"`
	Runner         *runnerStatus            `json:"runner"`
	AgentReady     bool                     `json:"agent_ready"`
	AgentError     string                   `json:"agent_error,omitempty"`
}

func (s *Server) loadState(sessionID string) (*sessionState, error) {
	sess, err := s.cfg.Store.GetSession(sessionID)
	if err != nil {
		return nil, err
	}
	goals, err := s.cfg.Store.ListGoals(sessionID)
	if err != nil {
		return nil, err
	}
	teams, err := s.cfg.Store.ListTeams(sessionID)
	if err != nil {
		return nil, err
	}
	students, err := s.cfg.Store.ListStudents(sessionID)
	if err != nil {
		return nil, err
	}
	assessments, err := s.cfg.Store.ListAssessments(sessionID)
	if err != nil {
		return nil, err
	}
	alerts, err := s.cfg.Store.ListAlerts(sessionID, true)
	if err != nil {
		return nil, err
	}
	misc, err := s.cfg.Store.ListMisconceptions(sessionID)
	if err != nil {
		return nil, err
	}
	rollups, cells := analytics.Rollup(goals, teams, students, assessments)

	st := &sessionState{
		Session: *sess, Goals: goals, Teams: teams, Students: students,
		Assessments: assessments, Alerts: alerts, Misconceptions: misc,
		GoalRollups: rollups, TeamGoalCells: cells,
		AgentReady: s.cfg.AgentReady, AgentError: s.cfg.AgentError,
	}
	s.mu.Lock()
	if r, ok := s.runners[sessionID]; ok {
		status := r.status()
		st.Runner = &status
	}
	s.mu.Unlock()
	return st, nil
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	st, err := s.loadState(r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "unknown_session", "no such session")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

type startRequest struct {
	// Speed multiplies scripted pacing. 1 is realistic.
	Speed float64 `json:"speed"`
}

// handleStart writes every team's transcript, then begins playback and
// monitoring.
//
// Script generation is ten parallel model calls and takes tens of seconds, so
// it runs in the background and the session is marked running immediately. The
// dashboard shows the rooms filling as each team's script lands.
func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	var req startRequest
	if r.ContentLength > 0 && !decodeBody(w, r, &req) {
		return
	}
	if req.Speed <= 0 {
		req.Speed = 1
	}

	sess, err := s.cfg.Store.GetSession(sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "unknown_session", "no such session")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
		return
	}

	s.mu.Lock()
	if _, live := s.runners[sessionID]; live {
		s.mu.Unlock()
		writeError(w, http.StatusConflict, "already_running", "session is already running")
		return
	}
	s.mu.Unlock()

	if sess.Status == model.SessionEnded {
		writeError(w, http.StatusConflict, "session_ended", "this session has ended; create a new one")
		return
	}

	if err := s.cfg.Store.SetSessionStatus(sessionID, model.SessionRunning); err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status": "starting", "message": "writing group transcripts",
	})

	go s.beginSession(sessionID, req.Speed)
}

// beginSession generates scripts then starts the runner. It is deliberately
// detached from the request: the caller already has its 202.
func (s *Server) beginSession(sessionID string, speed float64) {
	ctx := context.Background()

	unplayed, err := s.cfg.Store.CountUnplayedLines(sessionID)
	if err != nil {
		log.Printf("session %s: count lines: %v", sessionID, err)
		return
	}
	if unplayed == 0 {
		if err := s.generateScripts(ctx, sessionID); err != nil {
			log.Printf("session %s: generate scripts: %v", sessionID, err)
			s.hub.publish(sessionID, Event{Type: EventSession, Data: map[string]any{
				"status": "error", "error": err.Error(),
			}})
			return
		}
	}

	teams, err := s.cfg.Store.ListTeams(sessionID)
	if err != nil {
		log.Printf("session %s: list teams: %v", sessionID, err)
		return
	}

	run := &runner{
		sessionID: sessionID,
		st:        s.cfg.Store,
		hub:       s.hub,
		player: &simulation.Player{
			Store: s.cfg.Store, SessionID: sessionID, Speed: speed,
		},
		mon: &monitor.Monitor{
			Store: s.cfg.Store, Agent: s.cfg.Agent,
			MinNewMessages: 4, WindowSize: 45, Concurrency: 5,
		},
	}
	s.mu.Lock()
	s.runners[sessionID] = run
	s.mu.Unlock()

	s.hub.publish(sessionID, Event{Type: EventSession, Data: map[string]any{"status": "running"}})
	run.start(ctx, teams, s.cfg.MonitorInterval)
}

func (s *Server) generateScripts(ctx context.Context, sessionID string) error {
	goals, err := s.cfg.Store.ListGoals(sessionID)
	if err != nil {
		return err
	}
	teams, err := s.cfg.Store.ListTeams(sessionID)
	if err != nil {
		return err
	}
	truths, err := s.cfg.Store.ListTruths(sessionID)
	if err != nil {
		return err
	}
	truthMap := map[string]map[string]model.UnderstandingState{}
	for _, t := range truths {
		if truthMap[t.StudentID] == nil {
			truthMap[t.StudentID] = map[string]model.UnderstandingState{}
		}
		truthMap[t.StudentID][t.GoalID] = t.State
	}

	var reqs []simulation.ScriptRequest
	for i, t := range teams {
		students, err := s.cfg.Store.ListTeamStudents(t.ID)
		if err != nil {
			return err
		}
		// Only simulated seats get scripted lines. A seat already claimed by a
		// human speaks for itself.
		var simulated []model.Student
		for _, st := range students {
			if !st.IsHuman {
				simulated = append(simulated, st)
			}
		}
		if len(simulated) == 0 {
			continue
		}
		req := simulation.ScriptRequest{
			Team: t, Goals: goals, Students: simulated, Truth: truthMap,
		}
		// Plant the two social situations in fixed rooms so a demo always has
		// one of each to point at, and say which in the logs.
		switch i {
		case 2:
			req.Flavour = "Two students in this group get into a mild argument about who is doing the work. It is friction, not disagreement about the material, and it slows the group down."
		case 5:
			req.Flavour = "One student in this group barely participates: at most one very short, disengaged line in the whole transcript."
		}
		reqs = append(reqs, req)
	}

	writer := &simulation.Writer{Agent: s.cfg.Agent, Concurrency: 5}
	lines, errs := writer.WriteAll(ctx, sessionID, reqs)
	for _, e := range errs {
		// Loud: a team on the fallback script is a degraded demo, and the
		// operator must be able to see which and why.
		log.Printf("session %s: script generation fell back: %v", sessionID, e)
	}
	if len(lines) == 0 {
		return fmt.Errorf("no transcript produced for any team (%d errors)", len(errs))
	}
	log.Printf("session %s: %d scripted lines across %d teams (%d fell back)",
		sessionID, len(lines), len(reqs), len(errs))
	return s.cfg.Store.InsertScriptedLines(lines)
}

func (s *Server) handlePause(w http.ResponseWriter, r *http.Request)  { s.setPause(w, r, true) }
func (s *Server) handleResume(w http.ResponseWriter, r *http.Request) { s.setPause(w, r, false) }

func (s *Server) setPause(w http.ResponseWriter, r *http.Request, paused bool) {
	sessionID := r.PathValue("id")
	s.mu.Lock()
	run, ok := s.runners[sessionID]
	s.mu.Unlock()
	if !ok {
		writeError(w, http.StatusConflict, "not_running", "session is not running")
		return
	}
	run.setPaused(paused)
	status := model.SessionRunning
	if paused {
		status = model.SessionPaused
	}
	if err := s.cfg.Store.SetSessionStatus(sessionID, status); err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
		return
	}
	s.hub.publish(sessionID, Event{Type: EventSession, Data: map[string]any{"status": string(status)}})
	writeJSON(w, http.StatusOK, map[string]any{"status": string(status)})
}

func (s *Server) handleEnd(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	s.mu.Lock()
	run, ok := s.runners[sessionID]
	delete(s.runners, sessionID)
	s.mu.Unlock()
	if ok {
		run.stop()
	}
	if err := s.cfg.Store.SetSessionStatus(sessionID, model.SessionEnded); err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
		return
	}
	s.hub.publish(sessionID, Event{Type: EventSession, Data: map[string]any{"status": "ended"}})
	writeJSON(w, http.StatusOK, map[string]any{"status": "ended"})
}

// handleAssessNow forces a monitoring round immediately, for a demo that does
// not want to wait for the next tick.
func (s *Server) handleAssessNow(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	s.mu.Lock()
	run, ok := s.runners[sessionID]
	s.mu.Unlock()
	if !ok {
		writeError(w, http.StatusConflict, "not_running", "session is not running")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	// MinNewMessages is bypassed: an explicit request means assess what is
	// there, however little has changed.
	//
	// Concurrency is raised to cover every team in one wave. The background
	// loop keeps 5 to spread its load between ticks, but a forced round has
	// somebody waiting on it: at 5 a ten-team session runs two waves and was
	// measured at 2m19s, long enough that the caller assumes it has hung and
	// navigates away mid-request.
	forced := *run.mon
	forced.MinNewMessages = 1
	teams, err := s.cfg.Store.ListTeams(sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
		return
	}
	if len(teams) > forced.Concurrency {
		forced.Concurrency = len(teams)
	}
	checked, err := forced.RunRound(ctx, sessionID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "assess_failed", err.Error())
		return
	}
	s.hub.publish(sessionID, Event{Type: EventAssessments, Data: map[string]any{"forced": true}})
	writeJSON(w, http.StatusOK, map[string]any{"teams_checked": checked})
}

// ---------- accuracy and truth ----------

func (s *Server) handleAccuracy(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	truths, err := s.cfg.Store.ListTruths(sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
		return
	}
	assessments, err := s.cfg.Store.ListAssessments(sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, analytics.Score(truths, assessments))
}

// handleTruth reveals the simulation's hidden ground truth.
//
// It is a demo affordance and it is honest about what it is: this endpoint
// exists only because the students are synthetic. A real deployment would not
// have it, because there would be nothing behind it.
func (s *Server) handleTruth(w http.ResponseWriter, r *http.Request) {
	truths, err := s.cfg.Store.ListTruths(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, truths)
}

// ---------- rooms: joining and talking ----------

// handleRooms lists the teams a person can join, with who is already in them.
func (s *Server) handleRooms(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	students, err := s.cfg.Store.ListStudents(sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
		return
	}
	teams, err := s.cfg.Store.ListTeams(sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
		return
	}
	type room struct {
		TeamID   string   `json:"team_id"`
		TeamName string   `json:"team_name"`
		Members  []string `json:"members"`
		Humans   int      `json:"humans"`
	}
	out := make([]room, 0, len(teams))
	for _, t := range teams {
		rm := room{TeamID: t.ID, TeamName: t.Name, Members: []string{}}
		for _, st := range students {
			if st.TeamID != t.ID {
				continue
			}
			rm.Members = append(rm.Members, st.Name)
			if st.IsHuman {
				rm.Humans++
			}
		}
		out = append(out, rm)
	}
	writeJSON(w, http.StatusOK, out)
}

type joinRequest struct {
	TeamID      string `json:"team_id"`
	DisplayName string `json:"display_name"`
}

// handleJoin seats a real person in a team.
//
// The person is added as a NEW student rather than taking over a simulated
// one. Renaming an existing seat would re-attribute that seat's entire
// transcript to whoever just sat down, and the agent would assess the newcomer
// on words somebody else said.
func (s *Server) handleJoin(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	var req joinRequest
	if !decodeBody(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.DisplayName)
	if name == "" {
		writeError(w, http.StatusBadRequest, "missing_name", "display_name is required")
		return
	}
	if len(name) > 40 {
		writeError(w, http.StatusBadRequest, "name_too_long", "display_name must be under 40 characters")
		return
	}

	if _, err := s.cfg.Store.GetSession(sessionID); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "unknown_session", "no such session")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
		return
	}

	teams, err := s.cfg.Store.ListTeams(sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
		return
	}
	if len(teams) == 0 {
		writeError(w, http.StatusConflict, "no_teams", "this session has no teams")
		return
	}

	students, err := s.cfg.Store.ListStudents(sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
		return
	}

	target := ""
	if req.TeamID != "" {
		for _, t := range teams {
			if t.ID == req.TeamID {
				target = t.ID
			}
		}
		if target == "" {
			writeError(w, http.StatusNotFound, "unknown_team", "no such team in this session")
			return
		}
	} else {
		// No team asked for: put them in the smallest one, so joining does not
		// pile every visitor into the same room.
		size := map[string]int{}
		for _, st := range students {
			size[st.TeamID]++
		}
		best := -1
		for _, t := range teams {
			if best < 0 || size[t.ID] < best {
				best, target = size[t.ID], t.ID
			}
		}
	}

	// A duplicate display name in the same room would make the transcript
	// ambiguous for the agent, which resolves speakers by name.
	for _, st := range students {
		if st.TeamID == target && strings.EqualFold(strings.TrimSpace(st.Name), name) {
			writeError(w, http.StatusConflict, "name_taken",
				fmt.Sprintf("someone in that group is already called %q; pick another name", name))
			return
		}
	}

	student := &model.Student{
		ID: uuid.NewString(), SessionID: sessionID, TeamID: target,
		Name: name, IsHuman: true, JoinToken: uuid.NewString(),
	}
	if err := s.cfg.Store.AddHumanStudent(student); err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"student": student,
		"token":   student.JoinToken,
	})
}

func (s *Server) handleTeamMessages(w http.ResponseWriter, r *http.Request) {
	msgs, err := s.cfg.Store.ListTeamMessagesTail(r.PathValue("teamID"), 200)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

type postMessageRequest struct {
	Token string `json:"token"`
	Body  string `json:"body"`
}

// handlePostMessage is a real person talking in a breakout room. The message
// is stored exactly like a scripted one, so the monitoring agent judges a
// human and a simulated student by the same path.
func (s *Server) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	sessionID, teamID := r.PathValue("id"), r.PathValue("teamID")
	var req postMessageRequest
	if !decodeBody(w, r, &req) {
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" {
		writeError(w, http.StatusBadRequest, "empty_message", "body is required")
		return
	}
	if len(body) > 2000 {
		writeError(w, http.StatusBadRequest, "message_too_long", "message must be under 2000 characters")
		return
	}

	student, err := s.cfg.Store.GetStudentByToken(req.Token)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "unknown_token", "join the session before posting")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
		return
	}
	if student.TeamID != teamID || student.SessionID != sessionID {
		writeError(w, http.StatusForbidden, "wrong_room", "that seat is not in this room")
		return
	}

	msg := &model.Message{
		ID: uuid.NewString(), SessionID: sessionID, TeamID: teamID,
		StudentID: student.ID, Body: body, At: time.Now().UTC(),
	}
	if err := s.cfg.Store.AppendMessage(msg); err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
		return
	}
	s.hub.publish(sessionID, Event{Type: EventMessage, Data: *msg})
	writeJSON(w, http.StatusCreated, msg)
}

func (s *Server) handleResolveAlert(w http.ResponseWriter, r *http.Request) {
	if err := s.cfg.Store.ResolveAlert(r.PathValue("alertID")); err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"resolved": true})
}

// ---------- SSE ----------

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "no_flush", "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// nginx buffers proxied responses by default, which holds every event
	// until the buffer fills and makes a live dashboard look frozen.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	ch := s.hub.subscribe(sessionID)
	defer s.hub.unsubscribe(sessionID, ch)

	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	keepalive := time.NewTicker(20 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case payload, open := <-ch:
			if !open {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// Shutdown stops every live session.
func (s *Server) Shutdown() {
	s.mu.Lock()
	runners := make([]*runner, 0, len(s.runners))
	for _, r := range s.runners {
		runners = append(runners, r)
	}
	s.runners = map[string]*runner{}
	s.mu.Unlock()
	for _, r := range runners {
		r.stop()
	}
}
