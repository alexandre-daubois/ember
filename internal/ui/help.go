package ui

import (
	"fmt"

	"github.com/alexandredaubois/frankentop/internal/model"
)

func renderHelp(sortBy model.SortField, paused bool) string {
	pauseLabel := "p pause"
	if paused {
		pauseLabel = "p resume"
	}

	return helpStyle.Render(fmt.Sprintf(
		" ↑/↓ navigate · s sort (%s) · %s · r restart workers · q quit",
		sortBy, pauseLabel,
	))
}
