package ui

import "net/http"

// Render exposes the unexported render method for testing.
func (ui *UI) Render(w http.ResponseWriter, page string, data interface{}) {
	ui.render(w, page, data)
}
