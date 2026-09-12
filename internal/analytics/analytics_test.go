package analytics

import (
	"testing"

	"github.com/kayushkin/education-demo/internal/model"
)

func goals(n int) []model.Goal {
	out := make([]model.Goal, n)
	for i := range out {
		out[i] = model.Goal{ID: string(rune('a' + i)), Ordinal: i + 1}
	}
	return out
}

// TestUnassessedGoalIsNotZeroPercent pins the distinction the dashboard turns
// on: a goal the class has not reached scores -1, not 0. Rendering it as 0%
// would send the teacher to intervene on a topic nobody has opened.
func TestUnassessedGoalIsNotZeroPercent(t *testing.T) {
	g := goals(2)
	teams := []model.Team{{ID: "t1", Name: "Team 1"}}
	students := []model.Student{{ID: "s1", TeamID: "t1"}}
	// An assessment on goal "a" only; goal "b" has been seen by nobody.
	assessments := []model.Assessment{
		{StudentID: "s1", GoalID: "a", State: model.StateUnderstands},
	}

	rollups, _ := Rollup(g, teams, students, assessments)
	byGoal := map[string]GoalRollup{}
	for _, r := range rollups {
		byGoal[r.GoalID] = r
	}

	if got := byGoal["a"].UnderstoodPct; got != 100 {
		t.Errorf("assessed goal: understood_pct = %v, want 100", got)
	}
	if got := byGoal["b"].UnderstoodPct; got != -1 {
		t.Errorf("unassessed goal: understood_pct = %v, want -1 (not yet assessed)", got)
	}
	if got := byGoal["b"].Assessed; got != 0 {
		t.Errorf("unassessed goal: assessed = %d, want 0", got)
	}
}

// TestStuckTeamRequiresEngagement checks that a team is only reported stuck on
// a goal it has actually engaged with. A team that has not reached a goal is
// not a team failing it.
func TestStuckTeamRequiresEngagement(t *testing.T) {
	g := goals(2)
	teams := []model.Team{{ID: "t1", Name: "Team 1"}, {ID: "t2", Name: "Team 2"}}
	students := []model.Student{
		{ID: "s1", TeamID: "t1"}, {ID: "s2", TeamID: "t1"},
		{ID: "s3", TeamID: "t2"},
	}
	assessments := []model.Assessment{
		// Team 1 engaged goal "a" and nobody understands it: genuinely stuck.
		{StudentID: "s1", GoalID: "a", State: model.StateUnknown},
		{StudentID: "s2", GoalID: "a", State: model.StateMisunderstands},
		// Team 2 has said nothing about goal "a" at all.
	}

	rollups, _ := Rollup(g, teams, students, assessments)
	var stuck []string
	for _, r := range rollups {
		if r.GoalID == "a" {
			stuck = r.TeamsWithNoUnderstanding
		}
	}
	if len(stuck) != 1 || stuck[0] != "Team 1" {
		t.Errorf("teams_with_no_understanding = %v, want exactly [Team 1]", stuck)
	}
}

// TestScoreIgnoresPairsWithoutTruth pins that a human who joined — who has no
// ground truth — is excluded from scoring rather than counted as a miss.
func TestScoreIgnoresPairsWithoutTruth(t *testing.T) {
	truths := []model.Truth{
		{StudentID: "sim", GoalID: "a", State: model.StateUnderstands},
	}
	assessments := []model.Assessment{
		{StudentID: "sim", GoalID: "a", State: model.StateUnderstands},
		// A real person's seat: assessed, but unscoreable.
		{StudentID: "human", GoalID: "a", State: model.StateMisunderstands},
	}

	acc := Score(truths, assessments)
	if acc.Scored != 1 {
		t.Errorf("scored = %d, want 1 (the human is not scoreable)", acc.Scored)
	}
	if acc.Correct != 1 || acc.ExactPct != 100 {
		t.Errorf("correct = %d exact = %v, want 1 and 100", acc.Correct, acc.ExactPct)
	}
}

// TestAdjacentCountsNeighbouringStates checks the kinder metric: being one
// rung off is counted as adjacent, being two rungs off is not.
func TestAdjacentCountsNeighbouringStates(t *testing.T) {
	truths := []model.Truth{
		{StudentID: "s1", GoalID: "a", State: model.StateUnderstands},
		{StudentID: "s2", GoalID: "a", State: model.StateUnderstands},
	}
	assessments := []model.Assessment{
		// partial is adjacent to understands.
		{StudentID: "s1", GoalID: "a", State: model.StatePartial},
		// misunderstands is three rungs from understands: not adjacent.
		{StudentID: "s2", GoalID: "a", State: model.StateMisunderstands},
	}

	acc := Score(truths, assessments)
	if acc.ExactPct != 0 {
		t.Errorf("exact_pct = %v, want 0", acc.ExactPct)
	}
	if acc.AdjacentPct != 50 {
		t.Errorf("adjacent_pct = %v, want 50", acc.AdjacentPct)
	}
}

// TestMisunderstandsRecall is the headline number of the accuracy panel: of
// the students who genuinely hold a wrong idea, how many did the agent catch?
func TestMisunderstandsRecall(t *testing.T) {
	truths := []model.Truth{
		{StudentID: "s1", GoalID: "a", State: model.StateMisunderstands},
		{StudentID: "s2", GoalID: "a", State: model.StateMisunderstands},
		{StudentID: "s3", GoalID: "a", State: model.StateMisunderstands},
		{StudentID: "s4", GoalID: "a", State: model.StateUnderstands},
	}
	assessments := []model.Assessment{
		{StudentID: "s1", GoalID: "a", State: model.StateMisunderstands}, // caught
		{StudentID: "s2", GoalID: "a", State: model.StateMisunderstands}, // caught
		{StudentID: "s3", GoalID: "a", State: model.StateUnknown},        // missed
		{StudentID: "s4", GoalID: "a", State: model.StateMisunderstands}, // false alarm
	}

	acc := Score(truths, assessments)
	got := acc.PerState[model.StateMisunderstands]
	if got.TruthCount != 3 || got.Caught != 2 {
		t.Fatalf("truth_count = %d caught = %d, want 3 and 2", got.TruthCount, got.Caught)
	}
	if !closeTo(got.Recall, 66.667) {
		t.Errorf("recall = %v, want ~66.667", got.Recall)
	}
	// Three predictions of misunderstands, two of them right.
	if got.PredictCount != 3 {
		t.Errorf("predict_count = %d, want 3", got.PredictCount)
	}
	if !closeTo(got.Precision, 66.667) {
		t.Errorf("precision = %v, want ~66.667", got.Precision)
	}
}

// TestConfusionMatrixIsFullyPopulated checks every cell exists even at zero,
// so a heatmap has no holes to guess at.
func TestConfusionMatrixIsFullyPopulated(t *testing.T) {
	acc := Score(nil, nil)
	for _, truth := range model.UnderstandingStates {
		row, ok := acc.Confusion[truth]
		if !ok {
			t.Fatalf("confusion matrix missing row %q", truth)
		}
		for _, inferred := range model.UnderstandingStates {
			if _, ok := row[inferred]; !ok {
				t.Errorf("confusion matrix missing cell [%q][%q]", truth, inferred)
			}
		}
	}
}

// closeTo compares percentages within a thousandth. Comparing these exactly is
// a trap: Go folds a constant like 2.0/3.0*100 at arbitrary precision and
// rounds once, while the implementation divides and multiplies at float64 and
// rounds twice, so the two differ in the last bit.
func closeTo(got, want float64) bool {
	d := got - want
	if d < 0 {
		d = -d
	}
	return d < 0.001
}
