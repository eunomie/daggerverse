// Glow is a Dagger module to help render markdown.

package main

import (
	"context"
	"fmt"

	"dagger/glow/internal/dagger"
	"github.com/charmbracelet/glamour"
)

type Glow struct{}

// Render a markdown input string to be displayed on a terminal.
func (m *Glow) DisplayMarkdown(str string) (string, error) {
	return glamour.Render(str, "dark")
}

// Print readme file in the terminal
func (m *Glow) ReadMe(
	ctx context.Context,
// +defaultPath="README.md"
	file dagger.File,
) (string, error) {
	c, err := file.Contents(ctx)
	if err != nil {
		return "", fmt.Errorf("could not read README.md: %w", err)
	}
	return m.DisplayMarkdown(c)
}
