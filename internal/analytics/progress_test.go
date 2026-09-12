package analytics

import (
	"testing"

	"github.com/kayushkin/education-demo/internal/model"
)

func asTruthlike(in []model.Truth) []model.Truthlike {
	out := make([]model.Truthlike, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}

func tr(student, goal string, s model.UnderstandingState) model.Truth {
	return model.Truth{StudentID: student, GoalID: goal, State: s}
}

// TestCoverageGrowthIsNotLearning is the trap this view exists to avoid.
//
// The agent starts with an opinion about one student and later has opinions
// about three. Nobody learned anything — the agent just heard more. Counting
// the two new pairs as progress would report a class improving when the only
// thing that improved was the microphone.
func TestCoverageGrowthIsNotLearning(t *testing.T) {
	goals := []model.Goal{{ID: "g1", Ordinal: 1}}
	students := []model.Student{{ID: "s1"}, {ID: "s2"}, {ID: "s3"}}

	before := []model.Truth{tr("s1", "g1", model.StateUnknown)}
	after := []model.Truth{
		tr("s1", "g1", model.StateUnknown),
		tr("s2", "g1", model.StateUnderstands),
		tr("s3", "g1", model.StateUnderstands),
	}

	view := ComputeProgress("observed", asTruthlike(before), asTruthlike(after), goals, students)
	if view.Class.Pairs != 1 {
		t.Fatalf("pairs = %d, want 1 — only the pair present in BOTH sets is comparable", view.Class.Pairs)
	}
	if view.Class.Improved != 0 {
		t.Errorf("improved = %d, want 0 — nobody learned, the agent only heard more", view.Class.Improved)
	}
	if view.Class.UnderstoodPctAfter != 0 {
		t.Errorf("understood after = %v, want 0", view.Class.UnderstoodPctAfter)
	}
}

// TestLearningIsCountedInBothDirections pins that the view reports real
// movement each way over the same population.
func TestLearningIsCountedInBothDirections(t *testing.T) {
	goals := []model.Goal{{ID: "g1", Ordinal: 1}}
	students := []model.Student{{ID: "s1"}, {ID: "s2"}, {ID: "s3"}, {ID: "s4"}}

	before := []model.Truth{
		tr("s1", "g1", model.StateUnknown),
		tr("s2", "g1", model.StatePartial),
		tr("s3", "g1", model.StateUnderstands),
		tr("s4", "g1", model.StateUnknown),
	}
	after := []model.Truth{
		tr("s1", "g1", model.StateUnderstands),    // improved two rungs
		tr("s2", "g1", model.StateUnderstands),    // improved one
		tr("s3", "g1", model.StateUnderstands),    // unchanged
		tr("s4", "g1", model.StateMisunderstands), // went backwards
	}

	view := ComputeProgress("actual", asTruthlike(before), asTruthlike(after), goals, students)
	if view.Class.Improved != 2 || view.Class.Declined != 1 || view.Class.Unchanged != 1 {
		t.Errorf("improved/declined/unchanged = %d/%d/%d, want 2/1/1",
			view.Class.Improved, view.Class.Declined, view.Class.Unchanged)
	}
	if view.Class.UnderstoodPctBefore != 25 || view.Class.UnderstoodPctAfter != 75 {
		t.Errorf("understood %v%% -> %v%%, want 25 -> 75",
			view.Class.UnderstoodPctBefore, view.Class.UnderstoodPctAfter)
	}
	if view.Class.MisunderstoodPctBefore != 0 || view.Class.MisunderstoodPctAfter != 25 {
		t.Errorf("misunderstood %v%% -> %v%%, want 0 -> 25",
			view.Class.MisunderstoodPctBefore, view.Class.MisunderstoodPctAfter)
	}
	if view.GoalsImproved != 1 || view.GoalsDeclined != 0 {
		t.Errorf("goals improved/declined = %d/%d, want 1/0", view.GoalsImproved, view.GoalsDeclined)
	}
}

// TestUnmeasuredGoalReportsMinusOneNotZero keeps the same rule the rest of the
// dashboard follows: nothing measured is not the same as nobody understanding.
func TestUnmeasuredGoalReportsMinusOneNotZero(t *testing.T) {
	goals := []model.Goal{{ID: "g1", Ordinal: 1}, {ID: "g2", Ordinal: 2}}
	students := []model.Student{{ID: "s1"}}

	before := []model.Truth{tr("s1", "g1", model.StateUnknown)}
	after := []model.Truth{tr("s1", "g1", model.StateUnderstands)}

	view := ComputeProgress("actual", asTruthlike(before), asTruthlike(after), goals, students)
	var untouched *GoalProgress
	for i := range view.Goals {
		if view.Goals[i].GoalID == "g2" {
			untouched = &view.Goals[i]
		}
	}
	if untouched == nil {
		t.Fatal("goal g2 missing from the view; every goal must appear")
	}
	if untouched.Delta.Pairs != 0 {
		t.Fatalf("pairs = %d, want 0", untouched.Delta.Pairs)
	}
	if untouched.Delta.UnderstoodPctBefore != -1 || untouched.Delta.UnderstoodPctAfter != -1 {
		t.Errorf("unmeasured goal reported %v -> %v, want -1 -> -1 (not yet assessed)",
			untouched.Delta.UnderstoodPctBefore, untouched.Delta.UnderstoodPctAfter)
	}
	// A goal nobody has reached must not be counted as flat progress either.
	if view.GoalsImproved != 1 {
		t.Errorf("goals improved = %d, want 1", view.GoalsImproved)
	}
}

// TestEveryStateCellExistsInBothColumns keeps the bars gapless.
func TestEveryStateCellExistsInBothColumns(t *testing.T) {
	view := ComputeProgress("actual", nil, nil, nil, nil)
	for _, s := range model.UnderstandingStates {
		if _, ok := view.Class.Before[s]; !ok {
			t.Errorf("before column missing state %q", s)
		}
		if _, ok := view.Class.After[s]; !ok {
			t.Errorf("after column missing state %q", s)
		}
	}
}
