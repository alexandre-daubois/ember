package ui

import (
	"fmt"

	"github.com/alexandredaubois/ember/internal/model"
)

func renderHelp(sortBy model.SortField, paused bool, leakEnabled bool) string {
	pauseLabel := "p pause"
	if paused {
		pauseLabel = "p resume"
	}

	leakLabel := "l leak:on"
	if !leakEnabled {
		leakLabel = "l leak:off"
	}

	return helpStyle.Render(fmt.Sprintf(
		" ↑/↓ navigate · s/S sort (%s) · %s · %s · r restart · / filter · q quit",
		sortBy, pauseLabel, leakLabel,
	))
}
