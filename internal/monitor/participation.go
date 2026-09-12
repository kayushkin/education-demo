package monitor

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/kayushkin/education-demo/internal/model"
)

// participationAlerts is everything the monitor can report with no model at
// all: who has spoken and who has not.
//
// It deliberately produces NO understanding assessments. Participation is
// countable; comprehension is not, and deriving "understands" from message
// volume would fabricate the exact number this tool exists to measure. When
// llm-bridge is unreachable the grid stays empty and says so, rather than
// filling with keyword guesses that look like real assessment.
func participationAlerts(sessionID string, team model.Team,
	students []model.Student, transcript []model.Message) []model.Alert {

	if len(transcript) < 8 {
		// Too early to call anyone quiet.
		return nil
	}
	counts := map[string]int{}
	for _, m := range transcript {
		counts[m.StudentID]++
	}
	now := time.Now().UTC()
	var out []model.Alert
	for _, st := range students {
		if counts[st.ID] > 0 {
			continue
		}
		out = append(out, model.Alert{
			ID: uuid.NewString(), SessionID: sessionID, TeamID: team.ID,
			StudentID: st.ID, Kind: model.AlertDisengaged, Severity: model.SeverityWarn,
			Title: fmt.Sprintf("%s has not spoken", st.Name),
			Detail: fmt.Sprintf(
				"%s has contributed nothing in the last %d messages from this group.",
				st.Name, len(transcript)),
			At: now,
		})
	}
	return out
}
