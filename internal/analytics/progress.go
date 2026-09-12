package analytics

import (
	"github.com/kayushkin/education-demo/internal/model"
)

// Progress answers "what changed over the lesson" — for the class, for each
// goal, and for each student.
//
// It is reported TWICE, from two different sources, and the two must never be
// merged:
//
//   - Observed: the agent's first recorded opinion against its current one.
//     This is what the tool would report in a real classroom, and it is the
//     honest headline, because it is the only half that exists when the
//     students are real.
//   - Actual: the simulation's ground truth in the first phase against the
//     latest. Exact, and available only because the students are synthetic.
//
// Showing both is the point: the gap between them is how much to trust the
// first one.
type Progress struct {
	Observed ProgressView  `json:"observed"`
	Actual   *ProgressView `json:"actual,omitempty"`
	// PhasesElapsed is how far through the lesson the comparison reaches.
	// An end-of-session number taken at phase 1 is not an end-of-session
	// number, and the UI must be able to say so.
	PhasesElapsed int `json:"phases_elapsed"`
	PhaseCount    int `json:"phase_count"`
}

// ProgressView is one before-and-after comparison.
type ProgressView struct {
	// Source is "observed" or "actual", so a caller cannot mix them up by
	// position.
	Source string `json:"source"`
	// Class is the whole cohort, every student-goal pair pooled.
	Class StateDelta `json:"class"`
	// Goals is per learning goal, which is the view the brief asks for:
	// did most topics get more understood by the end?
	Goals []GoalProgress `json:"goals"`
	// Students is per student, pooled across their goals.
	Students []StudentProgress `json:"students"`
	// GoalsImproved counts goals whose understood share rose; GoalsDeclined
	// those where it fell. Flat goals are the remainder and are neither.
	GoalsImproved int `json:"goals_improved"`
	GoalsDeclined int `json:"goals_declined"`
	GoalsFlat     int `json:"goals_flat"`
}

// StateDelta is a before and after over one population of student-goal pairs.
type StateDelta struct {
	Before map[model.UnderstandingState]int `json:"before"`
	After  map[model.UnderstandingState]int `json:"after"`
	// Pairs is how many student-goal pairs this covers. Before and After are
	// over the SAME pairs, so the two columns are comparable — a pair the
	// agent has an opinion about now but had none about at the start is
	// excluded, because counting it would read as a student who went from
	// nothing to something when really the agent went from silent to speaking.
	Pairs int `json:"pairs"`
	// UnderstoodPctBefore/After are the headline percentages. -1 when there
	// are no pairs, never 0 — nothing measured is not the same as nobody
	// understanding.
	UnderstoodPctBefore    float64 `json:"understood_pct_before"`
	UnderstoodPctAfter     float64 `json:"understood_pct_after"`
	MisunderstoodPctBefore float64 `json:"misunderstood_pct_before"`
	MisunderstoodPctAfter  float64 `json:"misunderstood_pct_after"`
	// Improved / Declined / Unchanged count individual pairs by which way they
	// moved along the understanding scale.
	Improved  int `json:"improved"`
	Declined  int `json:"declined"`
	Unchanged int `json:"unchanged"`
}

type GoalProgress struct {
	GoalID     string     `json:"goal_id"`
	Ordinal    int        `json:"ordinal"`
	ShortLabel string     `json:"short_label"`
	Delta      StateDelta `json:"delta"`
}

type StudentProgress struct {
	StudentID string     `json:"student_id"`
	Name      string     `json:"name"`
	TeamID    string     `json:"team_id"`
	IsHuman   bool       `json:"is_human"`
	Delta     StateDelta `json:"delta"`
}

// pairKey identifies one student's standing on one goal.
type pairKey struct{ student, goal string }

func newStateCounts() map[model.UnderstandingState]int {
	m := map[model.UnderstandingState]int{}
	for _, s := range model.UnderstandingStates {
		m[s] = 0
	}
	return m
}

// ComputeProgress builds one view from a before-set and an after-set.
//
// Only pairs present in BOTH are counted. That is what keeps the two columns
// honest: a pair the agent has an opinion about now but had none about at the
// start would otherwise appear as a student who improved, when what actually
// improved was the agent's coverage.
func ComputeProgress(source string, before, after []model.Truthlike,
	goals []model.Goal, students []model.Student) ProgressView {

	beforeOf := make(map[pairKey]model.UnderstandingState, len(before))
	for _, b := range before {
		beforeOf[pairKey{b.Student(), b.Goal()}] = b.Understanding()
	}
	afterOf := make(map[pairKey]model.UnderstandingState, len(after))
	for _, a := range after {
		afterOf[pairKey{a.Student(), a.Goal()}] = a.Understanding()
	}

	classDelta := newDelta()
	goalDeltas := map[string]*StateDelta{}
	for _, g := range goals {
		d := newDelta()
		goalDeltas[g.ID] = &d
	}
	studentDeltas := map[string]*StateDelta{}
	for _, st := range students {
		d := newDelta()
		studentDeltas[st.ID] = &d
	}

	for key, wasState := range beforeOf {
		nowState, ok := afterOf[key]
		if !ok {
			continue
		}
		accumulate(&classDelta, wasState, nowState)
		if d, ok := goalDeltas[key.goal]; ok {
			accumulate(d, wasState, nowState)
		}
		if d, ok := studentDeltas[key.student]; ok {
			accumulate(d, wasState, nowState)
		}
	}

	finish(&classDelta)
	view := ProgressView{Source: source, Class: classDelta}

	for _, g := range goals {
		d := goalDeltas[g.ID]
		finish(d)
		view.Goals = append(view.Goals, GoalProgress{
			GoalID: g.ID, Ordinal: g.Ordinal, ShortLabel: g.ShortLabel, Delta: *d,
		})
		switch {
		case d.Pairs == 0:
			view.GoalsFlat++
		case d.UnderstoodPctAfter > d.UnderstoodPctBefore:
			view.GoalsImproved++
		case d.UnderstoodPctAfter < d.UnderstoodPctBefore:
			view.GoalsDeclined++
		default:
			view.GoalsFlat++
		}
	}
	for _, st := range students {
		d := studentDeltas[st.ID]
		finish(d)
		view.Students = append(view.Students, StudentProgress{
			StudentID: st.ID, Name: st.Name, TeamID: st.TeamID,
			IsHuman: st.IsHuman, Delta: *d,
		})
	}
	return view
}

func newDelta() StateDelta {
	return StateDelta{
		Before: newStateCounts(), After: newStateCounts(),
		UnderstoodPctBefore: -1, UnderstoodPctAfter: -1,
		MisunderstoodPctBefore: -1, MisunderstoodPctAfter: -1,
	}
}

func accumulate(d *StateDelta, was, now model.UnderstandingState) {
	d.Pairs++
	d.Before[was]++
	d.After[now]++
	switch {
	case stateRank[now] > stateRank[was]:
		d.Improved++
	case stateRank[now] < stateRank[was]:
		d.Declined++
	default:
		d.Unchanged++
	}
}

func finish(d *StateDelta) {
	if d.Pairs == 0 {
		return
	}
	f := float64(d.Pairs)
	d.UnderstoodPctBefore = float64(d.Before[model.StateUnderstands]) / f * 100
	d.UnderstoodPctAfter = float64(d.After[model.StateUnderstands]) / f * 100
	d.MisunderstoodPctBefore = float64(d.Before[model.StateMisunderstands]) / f * 100
	d.MisunderstoodPctAfter = float64(d.After[model.StateMisunderstands]) / f * 100
}
