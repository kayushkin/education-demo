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

// DefaultStateWeights describes a class that has been taught the material once
// and mostly followed it. Misunderstanding is rarer than not-knowing, which is
// what makes it worth alerting on.
var DefaultStateWeights = StateWeights{
	model.StateUnderstands:    40,
	model.StatePartial:        30,
	model.StateUnknown:        18,
	model.StateMisunderstands: 12,
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
}

// Built is the seeded classroom, returned for the caller to persist.
type Built struct {
	Session  model.Session
	Goals    []model.Goal
	Teams    []model.Team
	Students []model.Student
	Truths   []model.Truth
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

	truth := map[string]map[string]model.UnderstandingState{}
	for _, st := range out.Students {
		truth[st.ID] = map[string]model.UnderstandingState{}
		for _, g := range out.Goals {
			truth[st.ID][g.ID] = weights.draw(rng)
		}
	}

	if bp.PlantDrama {
		plantScenarios(rng, out, truth)
	}

	for _, st := range out.Students {
		for _, g := range out.Goals {
			out.Truths = append(out.Truths, model.Truth{
				StudentID: st.ID, GoalID: g.ID, State: truth[st.ID][g.ID],
			})
		}
	}
	return out, nil
}

// plantScenarios forces the three situations the teacher most needs to catch,
// so a short demo reliably contains all of them.
func plantScenarios(rng *rand.Rand, b *Built, truth map[string]map[string]model.UnderstandingState) {
	byTeam := map[string][]model.Student{}
	for _, st := range b.Students {
		byTeam[st.TeamID] = append(byTeam[st.TeamID], st)
	}

	// 1. One team where nobody understands one goal: the team cannot teach
	//    itself out of it, which is exactly when the teacher must intervene.
	stuckTeam := b.Teams[rng.Intn(len(b.Teams))]
	stuckGoal := b.Goals[rng.Intn(len(b.Goals))]
	for _, st := range byTeam[stuckTeam.ID] {
		if truth[st.ID][stuckGoal.ID] == model.StateUnderstands {
			truth[st.ID][stuckGoal.ID] = model.StateUnknown
		}
	}
	b.Planted = append(b.Planted, fmt.Sprintf(
		"%s: nobody understands goal %d (%s)", stuckTeam.Name, stuckGoal.Ordinal, stuckGoal.ShortLabel))

	// 2. A confidently wrong explainer on a different team: one student who
	//    misunderstands a goal their teammates have not got either, so the
	//    error spreads instead of being corrected.
	var loudTeam model.Team
	for {
		loudTeam = b.Teams[rng.Intn(len(b.Teams))]
		if loudTeam.ID != stuckTeam.ID || len(b.Teams) == 1 {
			break
		}
	}
	loudGoal := b.Goals[rng.Intn(len(b.Goals))]
	members := byTeam[loudTeam.ID]
	if len(members) > 0 {
		explainer := members[rng.Intn(len(members))]
		truth[explainer.ID][loudGoal.ID] = model.StateMisunderstands
		for _, st := range members {
			if st.ID != explainer.ID && truth[st.ID][loudGoal.ID] == model.StateUnderstands {
				truth[st.ID][loudGoal.ID] = model.StatePartial
			}
		}
		b.Planted = append(b.Planted, fmt.Sprintf(
			"%s: %s confidently wrong about goal %d (%s)",
			loudTeam.Name, explainer.Name, loudGoal.Ordinal, loudGoal.ShortLabel))
	}
}
