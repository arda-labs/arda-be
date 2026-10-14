package handler

import (
	"fmt"
	"time"
)

// dateContextPrompt tells the model what "today" is in the request's business
// timezone, with the report periods users usually mean, so relative phrases
// such as "tháng này", "tháng trước" or "quý này" resolve to a periodCode
// without a clarifying question. It changes every run, so it is its own system
// message after the static prompt.
func dateContextPrompt(now time.Time) string {
	firstOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	previous := firstOfMonth.AddDate(0, -1, 0)
	quarter := (int(now.Month())-1)/3 + 1
	return fmt.Sprintf(
		"Current date (business timezone %s): %s.\n"+
			"- \"tháng này\" = periodCode %s; \"tháng trước\" = %s; \"quý này\" = Q%d/%d; \"năm nay\" = %d.\n"+
			"- Resolve relative periods from this date yourself; do not ask the user for a period they already implied.",
		now.Location().String(),
		now.Format("2006-01-02"),
		now.Format("2006-01"),
		previous.Format("2006-01"),
		quarter, now.Year(),
		now.Year(),
	)
}
