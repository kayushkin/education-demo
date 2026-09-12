// Package simulation builds a synthetic classroom and drives its dialogue.
//
// The simulated students carry a hidden ground-truth understanding of each
// goal, drawn here and never shown to the monitoring agent. That is the whole
// point of the simulation: it makes the agent's inference scoreable, which is
// something a real classroom cannot do.
package simulation

import (
	"fmt"
	"math/rand"

	"github.com/google/uuid"
	"github.com/kayushkin/education-demo/internal/model"
)

// StateWeights is the chance of drawing each understanding state for a
// student-goal pair. Values are relative, not required to sum to anything.
type StateWeights map[model.UnderstandingState]int

// DefaultStateWeights is where a class STARTS: the material has been presented
// once and about a third of them have it.
//
// These are the opening odds, not the shape of the session. Understanding is
// advanced between phases by the peer-learning rule in learning.go, and over
// three phases these weights measure out at roughly:
//
//	understood      34% -> 67%     (it doubles; the group teaches itself)
//	misunderstands   8% ->  5%     (most wrong ideas get corrected by a peer)
//	team-goals improving                  69%, none go backwards on understanding
//	teams still stuck on a goal at the end 15%   (what the teacher must reach)
//	the lone-misinformer case              6%    of team-goal phases
//
// Misunderstanding is deliberately the rarest opening state. It is rare again
// as an OUTCOME for a different reason: correcting it only needs one person in
// the group who can explain, and with three to a team that is usually true.
var DefaultStateWeights = StateWeights{
	model.StateUnderstands:    34,
	model.StatePartial:        32,
	model.StateUnknown:        26,
	model.StateMisunderstands: 8,
}

func (w StateWeights) draw(rng *rand.Rand) model.UnderstandingState {
	total := 0
	// Iterating the ordered vocabulary, not the map, keeps a seeded run
	// reproducible: Go randomizes map iteration order on every pass.
	for _, s := range model.UnderstandingStates {
		total += w[s]
	}
	if total <= 0 {
		return model.StateUnknown
	}
	n := rng.Intn(total)
	for _, s := range model.UnderstandingStates {
		n -= w[s]
		if n < 0 {
			return s
		}
	}
	return model.StateUnknown
}

// studentNames is the roster pool. Thirty distinct, short, and easy to tell
// apart at a glance on a dashboard showing ten teams at once.
var studentNames = []string{
	"Amara", "Ben", "Chloe", "Diego", "Elena", "Farid", "Grace", "Hugo",
	"Iris", "Jonah", "Kira", "Liam", "Maya", "Noah", "Olive", "Priya",
	"Quinn", "Rosa", "Sam", "Tariq", "Uma", "Victor", "Wren", "Xavier",
	"Yuki", "Zane", "Anika", "Bruno", "Cleo", "Dmitri",
}

// Blueprint is everything needed to stand up a session before any dialogue.
type Blueprint struct {
	Title      string
	Subject    string
	GoalTexts  []string
	GoalLabels []string
	TeamCount  int
	ClassSize  int
	Weights    StateWeights
	PlantDrama bool
	Seed       int64
	// PhaseCount is how many segments the lesson runs in. Understanding is
	// advanced between them, so this is the only reason the beginning and the
	// end of a session differ at all.
	PhaseCount int
}

// Built is the seeded classroom, returned for the caller to persist.
type Built struct {
	Session  model.Session
	Goals    []model.Goal
	Teams    []model.Team
	Students []model.Student
	// PhaseTruths[i] is the ground truth during phase i+1. The first is drawn,
	// the rest are advanced by the peer-learning rule. Keeping every phase is
	// what lets the dashboard answer "what changed over the lesson" exactly
	// rather than inferring it from the agent's own shifting opinion.
	PhaseTruths []model.PhaseTruth
	// Planted names the scenarios forced into the ground truth, so the demo
	// script and the README can state plainly what was arranged rather than
	// implying the agent found something the simulation did not put there.
	Planted []string
}

// Build assembles the roster and draws every student's ground truth.
//
// It deliberately plants a few guaranteed situations when PlantDrama is set.
// Pure random draw across 30 students very often yields a class where no team
// is fully stuck and nobody is confidently wrong in front of others, and a
// demo that shows an empty alert feed proves nothing. Planting is stated in
// Built.Planted rather than hidden.
func Build(bp Blueprint) (*Built, error) {
	if len(bp.GoalTexts) == 0 {
		return nil, fmt.Errorf("blueprint has no goals")
	}
	if bp.TeamCount <= 0 {
		return nil, fmt.Errorf("team count must be positive, got %d", bp.TeamCount)
	}
	if bp.ClassSize <= 0 || bp.ClassSize > len(studentNames) {
		return nil, fmt.Errorf("class size must be 1..%d, got %d", len(studentNames), bp.ClassSize)
	}
	if bp.ClassSize < bp.TeamCount {
		return nil, fmt.Errorf("class of %d cannot fill %d teams", bp.ClassSize, bp.TeamCount)
	}
	weights := bp.Weights
	if len(weights) == 0 {
		weights = DefaultStateWeights
	}
	rng := rand.New(rand.NewSource(bp.Seed))

	sessionID := uuid.NewString()
	out := &Built{Planted: []string{}}
	out.Session = model.Session{
		ID: sessionID, Title: bp.Title, Subject: bp.Subject, Status: model.SessionSetup,
	}

	for i, text := range bp.GoalTexts {
		label := ""
		if i < len(bp.GoalLabels) {
			label = bp.GoalLabels[i]
		}
		out.Goals = append(out.Goals, model.Goal{
			ID: uuid.NewString(), SessionID: sessionID, Ordinal: i + 1,
			Text: text, ShortLabel: label,
		})
	}

	for i := 0; i < bp.TeamCount; i++ {
		out.Teams = append(out.Teams, model.Team{
			ID: uuid.NewString(), SessionID: sessionID, Ordinal: i + 1,
			Name: fmt.Sprintf("Team %d", i+1),
		})
	}

	// Deal students round-robin so team sizes differ by at most one.
	names := append([]string(nil), studentNames[:bp.ClassSize]...)
	rng.Shuffle(len(names), func(i, j int) { names[i], names[j] = names[j], names[i] })
	for i, name := range names {
		team := out.Teams[i%bp.TeamCount]
		out.Students = append(out.Students, model.Student{
			ID: uuid.NewString(), SessionID: sessionID, TeamID: team.ID,
			Name: name, IsHuman: false, JoinToken: uuid.NewString(),
		})
	}

	phases := bp.PhaseCount
	if phases <= 0 {
		phases = DefaultPhaseCount
	}

	// Phase 1: drawn.
	truth := map[string]map[string]model.UnderstandingState{}
	for _, st := range out.Students {
		truth[st.ID] = map[string]model.UnderstandingState{}
		for _, g := range out.Goals {
			truth[st.ID][g.ID] = weights.draw(rng)
		}
	}
	if bp.PlantDrama {
		plantLoneMisinformer(rng, out, truth)
	}
	out.PhaseTruths = append(out.PhaseTruths, snapshotTruth(1, out.Students, out.Goals, truth))

	// Phases 2..n: advanced, one team and one goal at a time, because that is
	// the unit the learning actually happens in — who is sitting with you
	// decides whether you get it.
	membersByTeam := map[string][]model.Student{}
	for _, st := range out.Students {
		membersByTeam[st.TeamID] = append(membersByTeam[st.TeamID], st)
	}
	for phase := 2; phase <= phases; phase++ {
		for _, team := range out.Teams {
			members := membersByTeam[team.ID]
			for _, g := range out.Goals {
				before := make([]model.UnderstandingState, len(members))
				for i, st := range members {
					before[i] = truth[st.ID][g.ID]
				}
				after := AdvanceTeamGoal(rng, before)
				for i, st := range members {
					truth[st.ID][g.ID] = after[i]
				}
			}
		}
		out.PhaseTruths = append(out.PhaseTruths, snapshotTruth(phase, out.Students, out.Goals, truth))
	}
	return out, nil
}

// snapshotTruth freezes the current state as one phase's record.
func snapshotTruth(phase int, students []model.Student, goals []model.Goal,
	truth map[string]map[string]model.UnderstandingState) model.PhaseTruth {

	out := model.PhaseTruth{Phase: phase}
	for _, st := range students {
		for _, g := range goals {
			out.Truths = append(out.Truths, model.Truth{
				StudentID: st.ID, GoalID: g.ID, Phase: phase, State: truth[st.ID][g.ID],
			})
		}
	}
	return out
}

// plantLoneMisinformer forces exactly ONE instance of the situation this tool
// exists to catch: a group where nobody can explain a goal and one member is
// confidently wrong about it, so their account is the only one in the room.
//
// Only one, and only this one. The earlier version planted a stuck group AND a
// confident explainer as separate arrangements, which made both look common.
// They are not: the rule in learning.go reaches this state on its own in about
// 6% of team-goal phases, and everywhere else the group teaches itself. What
// planting buys is that a five-minute demo reliably contains one, not that the
// class is full of them.
//
// It is named in Built.Planted rather than hidden, so nobody reads the agent
// finding it as the agent finding something nobody put there.
func plantLoneMisinformer(rng *rand.Rand, b *Built,
	truth map[string]map[string]model.UnderstandingState) {

	byTeam := map[string][]model.Student{}
	for _, st := range b.Students {
		byTeam[st.TeamID] = append(byTeam[st.TeamID], st)
	}
	team := b.Teams[rng.Intn(len(b.Teams))]
	goal := b.Goals[rng.Intn(len(b.Goals))]
	members := byTeam[team.ID]
	if len(members) == 0 {
		return
	}

	speaker := members[rng.Intn(len(members))]
	truth[speaker.ID][goal.ID] = model.StateMisunderstands
	// Everyone else is left unable to challenge it: no explainer, and not two
	// partial holders either, which is exactly the climate the learning rule
	// treats as misinformation spreading.
	for _, st := range members {
		if st.ID != speaker.ID {
			truth[st.ID][goal.ID] = model.StateUnknown
		}
	}
	b.Planted = append(b.Planted, fmt.Sprintf(
		"%s: %s is confidently wrong about goal %d (%s) and nobody there can correct it",
		team.Name, speaker.Name, goal.Ordinal, goal.ShortLabel))
}
