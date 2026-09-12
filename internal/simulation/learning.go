package simulation

import (
	"math/rand"

	"github.com/kayushkin/education-demo/internal/model"
)

// A lesson runs in phases. Ground truth is drawn for the first one and then
// ADVANCED between them by the rule below, so a student's understanding at the
// end of the session is not the understanding they started with. Without this
// the simulation had no learning in it at all: truth was drawn once and frozen,
// and any "improvement" on the dashboard was only the agent's coverage growing.
//
// The rule is deliberately about the GROUP, not the individual. What decides
// whether you learn a goal is not your own starting state but whether anybody
// sitting with you can explain it — which is the entire argument for putting
// students in groups, and the thing a teacher is trying to manage when she
// decides which table to walk to.

// DefaultPhaseCount is how many segments a lesson runs in. Three gives a
// beginning, a middle and an end — enough to show a direction of travel
// without making the transcript so long nobody reads it.
const DefaultPhaseCount = 3

// GroupClimate is the situation a team is in on one goal: who can explain it,
// who half-grasps it, and whether anyone is confidently wrong. Every outcome
// below follows from these three counts.
type GroupClimate struct {
	Explainers     int // members who understand it and can say why
	PartialHolders int // members who half-grasp it
	WrongVoices    int // members who hold it wrongly and say so
	Members        int
}

// ClimateOf counts a team's standing on one goal.
func ClimateOf(states []model.UnderstandingState) GroupClimate {
	c := GroupClimate{Members: len(states)}
	for _, s := range states {
		switch s {
		case model.StateUnderstands:
			c.Explainers++
		case model.StatePartial:
			c.PartialHolders++
		case model.StateMisunderstands:
			c.WrongVoices++
		}
	}
	return c
}

// CanTeachItself reports whether the group has the means to close the gap on
// its own — either somebody who understands, or two half-understandings that
// can be pieced together.
//
// This is the line the teacher's `goal_unmastered` alert is really drawing:
// below it, nothing improves until an adult arrives.
func (c GroupClimate) CanTeachItself() bool {
	return c.Explainers > 0 || c.PartialHolders >= 2
}

// WillSpreadMisinformation reports the bad case the whole tool exists to catch:
// nobody can explain the goal, and somebody confidently wrong is filling the
// silence. Their version is the only version in the room.
//
// This is rare BY CONSTRUCTION rather than by tuning a probability down — it
// needs no explainer, fewer than two partial holders, and a wrong voice, all in
// the same group on the same goal.
func (c GroupClimate) WillSpreadMisinformation() bool {
	return !c.CanTeachItself() && c.WrongVoices > 0
}

// learningOdds is the chance of each upgrade, per phase, in each climate.
// Named rather than inlined so the numbers can be read and argued with in one
// place instead of being scattered through the branches below.
type learningOdds struct {
	unknownToPartial     float64
	unknownToUnderstands float64
	partialToUnderstands float64
	wrongCorrected       float64 // to partial, having seen why they were wrong
	unknownToWrong       float64 // only when misinformation is spreading
	partialToWrong       float64
}

var (
	// taughtByPeer: somebody in the group understands and is explaining.
	// Strong, and it fixes wrong ideas rather than leaving them standing.
	taughtByPeer = learningOdds{
		unknownToPartial:     0.65,
		unknownToUnderstands: 0.15,
		partialToUnderstands: 0.60,
		wrongCorrected:       0.70,
	}
	// piecedTogether: no one understands it, but two or more half-grasp it and
	// argue their way forward. Real, slower, and much worse at catching an
	// error — nobody is sure enough to overrule it.
	piecedTogether = learningOdds{
		unknownToPartial:     0.35,
		unknownToUnderstands: 0.05,
		partialToUnderstands: 0.30,
		wrongCorrected:       0.25,
	}
	// stuck: nobody can explain it and nobody is confidently wrong either. The
	// group mostly stalls; occasionally someone reasons a step forward alone.
	stuck = learningOdds{
		unknownToPartial:     0.10,
		partialToUnderstands: 0.08,
		wrongCorrected:       0.05,
	}
	// misinformed: nobody can explain it and a confident wrong voice is the
	// only account on offer. The error travels.
	misinformed = learningOdds{
		unknownToPartial: 0.05,
		wrongCorrected:   0.02,
		unknownToWrong:   0.45,
		partialToWrong:   0.20,
	}
)

func (c GroupClimate) odds() learningOdds {
	switch {
	case c.Explainers > 0:
		return taughtByPeer
	case c.PartialHolders >= 2:
		return piecedTogether
	case c.WrongVoices > 0:
		return misinformed
	default:
		return stuck
	}
}

// AdvanceTeamGoal advances one team's understanding of one goal by one phase.
//
// in and the returned slice are parallel to the team's roster. The function is
// pure given rng, which is what lets the whole learning model be tested
// without a database or a model call.
func AdvanceTeamGoal(rng *rand.Rand, in []model.UnderstandingState) []model.UnderstandingState {
	climate := ClimateOf(in)
	o := climate.odds()

	out := make([]model.UnderstandingState, len(in))
	for i, s := range in {
		out[i] = s
		switch s {
		case model.StateUnderstands:
			// Understanding is not lost inside one lesson. A student who can
			// explain it does not stop being able to because a neighbour is
			// loud — modelling that would make the arc noise rather than
			// learning.
		case model.StatePartial:
			if rng.Float64() < o.partialToUnderstands {
				out[i] = model.StateUnderstands
			} else if rng.Float64() < o.partialToWrong {
				out[i] = model.StateMisunderstands
			}
		case model.StateUnknown:
			switch r := rng.Float64(); {
			case r < o.unknownToUnderstands:
				out[i] = model.StateUnderstands
			case r < o.unknownToUnderstands+o.unknownToPartial:
				out[i] = model.StatePartial
			default:
				if rng.Float64() < o.unknownToWrong {
					out[i] = model.StateMisunderstands
				}
			}
		case model.StateMisunderstands:
			if rng.Float64() < o.wrongCorrected {
				// Corrected lands on partial, not understands: being shown you
				// were wrong is not the same as grasping the right answer.
				out[i] = model.StatePartial
			}
		}
	}
	return out
}
