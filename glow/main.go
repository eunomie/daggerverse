// Glow is a Dagger module to help render markdown.

package main

import (
	"github.com/charmbracelet/glamour"
)

type Glow struct{}

// Render a markdown input string to be displayed on a terminal.
func (m *Glow) DisplayMarkdown(str string) (string, error) {
	return glamour.Render(str, "dark")
}
