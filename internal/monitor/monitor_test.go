package monitor

import (
	"testing"

	"github.com/kayushkin/education-demo/internal/model"
)

// TestNormalizeMisconceptionFoldsPunctuationAndCase pins what the collector
// treats as the same wrong idea. Genuinely different wordings still land
// apart — clustering those needs embeddings — but casing and punctuation must
// never split one idea in two.
func TestNormalizeMisconceptionFoldsPunctuationAndCase(t *testing.T) {
	same := []string{
		"Heavier objects fall faster!",
		"heavier objects fall faster",
		"  Heavier   objects fall faster.  ",
		"HEAVIER OBJECTS FALL FASTER",
	}
	want := normalizeMisconception(same[0])
	for _, v := range same[1:] {
		if got := normalizeMisconception(v); got != want {
			t.Errorf("normalize(%q) = %q, want %q", v, got, want)
		}
	}
	if normalizeMisconception("lighter objects fall faster") == want {
		t.Error("two different claims normalized to the same key")
	}
}

// TestDedupeKeySeparatesDistinctProblems checks the key distinguishes the
// things a teacher would act on separately, and collapses the ones she would
// not. Two students confidently wrong about the same goal are two problems;
// the same student re-flagged next round is one.
func TestDedupeKeySeparatesDistinctProblems(t *testing.T) {
	base := model.Alert{
		Kind: model.AlertConfidentlyWrong, TeamID: "t1", StudentID: "s1", GoalID: "g1",
	}
	repeat := base
	repeat.Title = "different wording next round"
	if dedupeKey(base) != dedupeKey(repeat) {
		t.Error("the same standing problem produced two different keys")
	}

	otherStudent := base
	otherStudent.StudentID = "s2"
	if dedupeKey(base) == dedupeKey(otherStudent) {
		t.Error("two different students collapsed to one key")
	}

	otherGoal := base
	otherGoal.GoalID = "g2"
	if dedupeKey(base) == dedupeKey(otherGoal) {
		t.Error("two different goals collapsed to one key")
	}

	otherKind := base
	otherKind.Kind = model.AlertDisengaged
	if dedupeKey(base) == dedupeKey(otherKind) {
		t.Error("two different alert kinds collapsed to one key")
	}
}

// TestParticipationAlertsNeverAssertUnderstanding is the guard on the
// degraded path: with no model available the monitor may report who was
// silent, and must not invent what anybody understands.
func TestParticipationAlertsNeverAssertUnderstanding(t *testing.T) {
	team := model.Team{ID: "t1", Name: "Team 1"}
	students := []model.Student{
		{ID: "s1", Name: "Amara", TeamID: "t1"},
		{ID: "s2", Name: "Ben", TeamID: "t1"},
	}
	transcript := make([]model.Message, 0, 10)
	for i := 0; i < 10; i++ {
		transcript = append(transcript, model.Message{StudentID: "s1", Body: "talking"})
	}

	alerts := participationAlerts("sess", team, students, transcript)
	if len(alerts) != 1 {
		t.Fatalf("got %d alerts, want 1 for the silent student", len(alerts))
	}
	if alerts[0].StudentID != "s2" {
		t.Errorf("flagged %q, want the student who said nothing", alerts[0].StudentID)
	}
	for _, a := range alerts {
		if a.Kind != model.AlertDisengaged {
			t.Errorf("participation fallback raised %q; it may only report disengagement", a.Kind)
		}
	}
}

// TestParticipationStaysQuietEarly checks nobody is called disengaged before
// the group has said enough for silence to mean anything.
func TestParticipationStaysQuietEarly(t *testing.T) {
	team := model.Team{ID: "t1", Name: "Team 1"}
	students := []model.Student{{ID: "s1", Name: "Amara", TeamID: "t1"}}
	short := []model.Message{{StudentID: "other", Body: "hi"}}

	if alerts := participationAlerts("sess", team, students, short); len(alerts) != 0 {
		t.Errorf("got %d alerts from a 1-message transcript, want none", len(alerts))
	}
}
