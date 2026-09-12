package simulation

import (
	"math/rand"
	"testing"

	"github.com/kayushkin/education-demo/internal/model"
)

func states(ss ...model.UnderstandingState) []model.UnderstandingState { return ss }

// TestGroupWithAnExplainerTeachesItself pins the premise of the whole design:
// one person who can explain is enough to pull a group forward.
func TestGroupWithAnExplainerTeachesItself(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	improved := 0
	const trials = 400
	for i := 0; i < trials; i++ {
		in := states(model.StateUnderstands, model.StateUnknown, model.StateUnknown)
		out := AdvanceTeamGoal(rng, in)
		for j := range out {
			if stateBetter(out[j], in[j]) {
				improved++
			}
		}
	}
	// Two learners per trial, each with a good chance of moving up.
	if got := float64(improved) / float64(trials*2); got < 0.6 {
		t.Errorf("only %.0f%% of learners moved up with an explainer present, want >60%%", got*100)
	}
}

// TestExplainerFixesAWrongIdea is the counterpart: a misunderstanding in a
// group that contains someone who understands is usually corrected. This is
// WHY misunderstanding is rare as an outcome, and it is the mechanism the
// brief asked for.
func TestExplainerFixesAWrongIdea(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	corrected := 0
	const trials = 400
	for i := 0; i < trials; i++ {
		in := states(model.StateUnderstands, model.StateMisunderstands, model.StatePartial)
		out := AdvanceTeamGoal(rng, in)
		if out[1] != model.StateMisunderstands {
			corrected++
		}
	}
	if got := float64(corrected) / trials; got < 0.6 {
		t.Errorf("wrong idea corrected only %.0f%% of the time with an explainer present, want >60%%", got*100)
	}
}

// TestMisinformationNeedsNobodyAbleToCorrect pins that the bad case is
// structural, not a dice roll: it requires no explainer AND fewer than two
// partial holders AND a confident wrong voice.
func TestMisinformationNeedsNobodyAbleToCorrect(t *testing.T) {
	cases := []struct {
		name   string
		in     []model.UnderstandingState
		spread bool
	}{
		{"one explainer present", states(model.StateUnderstands, model.StateMisunderstands, model.StateUnknown), false},
		{"two partial holders can push back", states(model.StatePartial, model.StatePartial, model.StateMisunderstands), false},
		{"nobody wrong, just stuck", states(model.StateUnknown, model.StateUnknown, model.StateUnknown), false},
		{"lone misinformer", states(model.StateMisunderstands, model.StateUnknown, model.StateUnknown), true},
		{"lone misinformer with one partial", states(model.StateMisunderstands, model.StatePartial, model.StateUnknown), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ClimateOf(c.in).WillSpreadMisinformation(); got != c.spread {
				t.Errorf("WillSpreadMisinformation = %v, want %v", got, c.spread)
			}
		})
	}
}

// TestMisinformationActuallySpreads checks the lone-misinformer climate does
// what it says: the wrong idea travels to people who had no opinion.
func TestMisinformationActuallySpreads(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	infected := 0
	const trials = 400
	for i := 0; i < trials; i++ {
		in := states(model.StateMisunderstands, model.StateUnknown, model.StateUnknown)
		out := AdvanceTeamGoal(rng, in)
		for _, s := range out[1:] {
			if s == model.StateMisunderstands {
				infected++
			}
		}
	}
	if got := float64(infected) / float64(trials*2); got < 0.25 {
		t.Errorf("the wrong idea reached only %.0f%% of the uninformed, want >25%%", got*100)
	}
}

// TestUnderstandingIsNeverLost pins that a student who can explain a goal does
// not stop being able to inside one lesson. Modelling that would turn the arc
// into noise and would make the progress view unreadable.
func TestUnderstandingIsNeverLost(t *testing.T) {
	rng := rand.New(rand.NewSource(13))
	for i := 0; i < 500; i++ {
		in := states(model.StateUnderstands, model.StateMisunderstands, model.StateMisunderstands)
		out := AdvanceTeamGoal(rng, in)
		if out[0] != model.StateUnderstands {
			t.Fatalf("a student who understood the goal became %q", out[0])
		}
	}
}

// TestMostGoalsImproveOverASession is the brief's headline, measured end to
// end over many simulated classes rather than asserted.
func TestMostGoalsImproveOverASession(t *testing.T) {
	const classes, teams, perTeam, phases = 200, 10, 3, DefaultPhaseCount
	rng := rand.New(rand.NewSource(17))
	improvedGoals, totalGoals := 0, 0
	startU, endU, startW, endW, cells := 0, 0, 0, 0, 0

	for c := 0; c < classes; c++ {
		before, after := 0, 0
		for tm := 0; tm < teams; tm++ {
			team := make([]model.UnderstandingState, perTeam)
			for i := range team {
				team[i] = DefaultStateWeights.draw(rng)
			}
			for _, s := range team {
				cells++
				if s == model.StateUnderstands {
					startU++
					before++
				}
				if s == model.StateMisunderstands {
					startW++
				}
			}
			for p := 1; p < phases; p++ {
				team = AdvanceTeamGoal(rng, team)
			}
			for _, s := range team {
				if s == model.StateUnderstands {
					endU++
					after++
				}
				if s == model.StateMisunderstands {
					endW++
				}
			}
		}
		totalGoals++
		if after > before {
			improvedGoals++
		}
	}

	improvedShare := float64(improvedGoals) / float64(totalGoals)
	if improvedShare < 0.85 {
		t.Errorf("only %.0f%% of goals improved across the class, want >=85%%", improvedShare*100)
	}
	startPct := float64(startU) / float64(cells) * 100
	endPct := float64(endU) / float64(cells) * 100
	if endPct <= startPct {
		t.Errorf("understanding did not rise: %.1f%% -> %.1f%%", startPct, endPct)
	}
	// Misunderstanding must be rare at the start and rarer at the end, which
	// is the specific behaviour asked for.
	startWrong := float64(startW) / float64(cells) * 100
	endWrong := float64(endW) / float64(cells) * 100
	if startWrong > 12 {
		t.Errorf("misunderstanding starts at %.1f%%, wanted it uncommon (<=12%%)", startWrong)
	}
	if endWrong >= startWrong {
		t.Errorf("misunderstanding did not fall over the session: %.1f%% -> %.1f%%", startWrong, endWrong)
	}
	t.Logf("understood %.1f%% -> %.1f%%, misunderstands %.1f%% -> %.1f%%, %.0f%% of goals improved",
		startPct, endPct, startWrong, endWrong, improvedShare*100)
}

func stateBetter(now, was model.UnderstandingState) bool {
	rank := map[model.UnderstandingState]int{
		model.StateMisunderstands: 0, model.StateUnknown: 1,
		model.StatePartial: 2, model.StateUnderstands: 3,
	}
	return rank[now] > rank[was]
}
