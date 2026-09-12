// Package analytics turns the raw assessment rows into the two things the
// teacher's dashboard asks for: how the class is doing per goal, and how much
// the agent's reading of it can be trusted.
package analytics

import (
	"github.com/kayushkin/education-demo/internal/model"
)

// GoalRollup is one goal's standing across every group.
type GoalRollup struct {
	GoalID     string `json:"goal_id"`
	Ordinal    int    `json:"ordinal"`
	ShortLabel string `json:"short_label"`
	// Counts is keyed by understanding state. Every state in the vocabulary is
	// present, including the zeroes, so a bar chart has no gaps to guess at.
	Counts map[model.UnderstandingState]int `json:"counts"`
	// Assessed is how many students the agent has an opinion about; Roster is
	// how many there are. The difference is coverage, not ignorance, and the
	// UI must not read a low Assessed as a low score.
	Assessed int `json:"assessed"`
	Roster   int `json:"roster"`
	// UnderstoodPct is understands / assessed, as a percentage. -1 when
	// nothing has been assessed yet: a goal nobody has reached is not a goal
	// scoring zero, and rendering it as 0% would tell the teacher to
	// intervene on a topic the class has not opened.
	UnderstoodPct float64 `json:"understood_pct"`
	// TeamsWithNoUnderstanding lists groups where not one member understands
	// this goal — the flag the teacher acts on first.
	TeamsWithNoUnderstanding []string `json:"teams_with_no_understanding"`
}

// TeamGoalCell is one group's standing on one goal, for the heatmap.
type TeamGoalCell struct {
	TeamID   string                           `json:"team_id"`
	GoalID   string                           `json:"goal_id"`
	Counts   map[model.UnderstandingState]int `json:"counts"`
	Assessed int                              `json:"assessed"`
	Roster   int                              `json:"roster"`
	// AnyUnderstands drives the "this group cannot dig itself out" flag.
	AnyUnderstands bool `json:"any_understands"`
}

// Rollup computes the per-goal and per-team-per-goal views in one pass.
func Rollup(goals []model.Goal, teams []model.Team, students []model.Student,
	assessments []model.Assessment) ([]GoalRollup, []TeamGoalCell) {

	teamOf := map[string]string{}
	rosterByTeam := map[string]int{}
	for _, s := range students {
		teamOf[s.ID] = s.TeamID
		rosterByTeam[s.TeamID]++
	}
	teamName := map[string]string{}
	for _, t := range teams {
		teamName[t.ID] = t.Name
	}

	type key struct{ team, goal string }
	cellCounts := map[key]map[model.UnderstandingState]int{}
	goalCounts := map[string]map[model.UnderstandingState]int{}

	newCounts := func() map[model.UnderstandingState]int {
		m := map[model.UnderstandingState]int{}
		for _, s := range model.UnderstandingStates {
			m[s] = 0
		}
		return m
	}
	for _, g := range goals {
		goalCounts[g.ID] = newCounts()
		for _, t := range teams {
			cellCounts[key{t.ID, g.ID}] = newCounts()
		}
	}

	for _, a := range assessments {
		tid, ok := teamOf[a.StudentID]
		if !ok {
			continue
		}
		if gc, ok := goalCounts[a.GoalID]; ok {
			gc[a.State]++
		}
		if cc, ok := cellCounts[key{tid, a.GoalID}]; ok {
			cc[a.State]++
		}
	}

	cells := make([]TeamGoalCell, 0, len(teams)*len(goals))
	for _, t := range teams {
		for _, g := range goals {
			c := cellCounts[key{t.ID, g.ID}]
			assessed := 0
			for _, n := range c {
				assessed += n
			}
			cells = append(cells, TeamGoalCell{
				TeamID: t.ID, GoalID: g.ID, Counts: c,
				Assessed: assessed, Roster: rosterByTeam[t.ID],
				AnyUnderstands: c[model.StateUnderstands] > 0,
			})
		}
	}

	rollups := make([]GoalRollup, 0, len(goals))
	for _, g := range goals {
		c := goalCounts[g.ID]
		assessed := 0
		for _, n := range c {
			assessed += n
		}
		pct := -1.0
		if assessed > 0 {
			pct = float64(c[model.StateUnderstands]) / float64(assessed) * 100
		}
		stuck := []string{}
		for _, t := range teams {
			cc := cellCounts[key{t.ID, g.ID}]
			engaged := 0
			for _, n := range cc {
				engaged += n
			}
			// Only a group that has engaged with the goal can be stuck on it.
			// A group that has not reached it yet is not a problem to flag.
			if engaged > 0 && cc[model.StateUnderstands] == 0 {
				stuck = append(stuck, teamName[t.ID])
			}
		}
		rollups = append(rollups, GoalRollup{
			GoalID: g.ID, Ordinal: g.Ordinal, ShortLabel: g.ShortLabel,
			Counts: c, Assessed: assessed, Roster: len(students),
			UnderstoodPct: pct, TeamsWithNoUnderstanding: stuck,
		})
	}
	return rollups, cells
}

// Accuracy scores the monitoring agent against the simulation's ground truth.
//
// This exists because the classroom is synthetic. In a real one none of it is
// computable, and the honest thing is to say so rather than ship a number
// derived from nothing.
type Accuracy struct {
	// Scored is the number of (student, goal) pairs where both a ground truth
	// and an assessment exist. Only simulated students have ground truth, so
	// a human who joins is excluded — correctly, since nobody knows what they
	// actually understand.
	Scored int `json:"scored"`
	// TruthPairs is how many ground-truth pairs exist at all; Scored over
	// this is the agent's coverage so far.
	TruthPairs int     `json:"truth_pairs"`
	Correct    int     `json:"correct"`
	ExactPct   float64 `json:"exact_pct"`
	// AdjacentPct counts a neighbouring state as near-enough (partial for
	// understands). It is the kinder number and is reported beside the strict
	// one, never instead of it.
	AdjacentPct float64 `json:"adjacent_pct"`
	// Confusion[truth][inferred] is the full matrix.
	Confusion map[model.UnderstandingState]map[model.UnderstandingState]int `json:"confusion"`
	// PerState holds recall for each true state. Recall on misunderstands is
	// the headline: it is the fraction of genuinely confused students the
	// agent actually caught.
	PerState map[model.UnderstandingState]StateScore `json:"per_state"`
}

type StateScore struct {
	TruthCount   int     `json:"truth_count"`
	Caught       int     `json:"caught"`
	Recall       float64 `json:"recall"`
	PredictCount int     `json:"predict_count"`
	Precision    float64 `json:"precision"`
}

// stateRank orders the vocabulary so "adjacent" has a meaning.
var stateRank = map[model.UnderstandingState]int{
	model.StateMisunderstands: 0,
	model.StateUnknown:        1,
	model.StatePartial:        2,
	model.StateUnderstands:    3,
}

// Score compares every assessed pair against its ground truth.
func Score(truths []model.Truth, assessments []model.Assessment) Accuracy {
	acc := Accuracy{
		TruthPairs: len(truths),
		Confusion:  map[model.UnderstandingState]map[model.UnderstandingState]int{},
		PerState:   map[model.UnderstandingState]StateScore{},
	}
	for _, t := range model.UnderstandingStates {
		acc.Confusion[t] = map[model.UnderstandingState]int{}
		for _, p := range model.UnderstandingStates {
			acc.Confusion[t][p] = 0
		}
	}

	type key struct{ student, goal string }
	truthOf := make(map[key]model.UnderstandingState, len(truths))
	for _, t := range truths {
		truthOf[key{t.StudentID, t.GoalID}] = t.State
	}

	adjacent := 0
	for _, a := range assessments {
		truth, ok := truthOf[key{a.StudentID, a.GoalID}]
		if !ok {
			// No ground truth: a human's seat, or a stale row. Not scoreable,
			// and not counted as a miss.
			continue
		}
		acc.Scored++
		acc.Confusion[truth][a.State]++
		if truth == a.State {
			acc.Correct++
			adjacent++
		} else if abs(stateRank[truth]-stateRank[a.State]) == 1 {
			adjacent++
		}
	}

	if acc.Scored > 0 {
		acc.ExactPct = float64(acc.Correct) / float64(acc.Scored) * 100
		acc.AdjacentPct = float64(adjacent) / float64(acc.Scored) * 100
	}

	for _, truthState := range model.UnderstandingStates {
		row := acc.Confusion[truthState]
		truthTotal, caught := 0, row[truthState]
		for _, n := range row {
			truthTotal += n
		}
		predicted := 0
		for _, other := range model.UnderstandingStates {
			predicted += acc.Confusion[other][truthState]
		}
		sc := StateScore{TruthCount: truthTotal, Caught: caught, PredictCount: predicted}
		if truthTotal > 0 {
			sc.Recall = float64(caught) / float64(truthTotal) * 100
		}
		if predicted > 0 {
			sc.Precision = float64(caught) / float64(predicted) * 100
		}
		acc.PerState[truthState] = sc
	}
	return acc
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
