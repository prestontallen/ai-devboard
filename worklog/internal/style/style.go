// Package style centralizes Lip Gloss styles so each command renders
// consistently. Honors NO_COLOR via Lip Gloss's built-in detection.
package style

import "github.com/charmbracelet/lipgloss"

var (
	// Heading is the styled `## Section` heading in CLI output.
	Heading = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7AA2F7")) // blue

	// SubHeading is a secondary label.
	SubHeading = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A9B1D6")). // light slate
			Italic(true)

	// Dim is for tertiary/auxiliary info.
	Dim = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#565F89")) // muted blue-gray

	// Active styles a `[~]` ticket.
	Active = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#9ECE6A")) // green

	// Pending styles a `[ ]` ticket.
	Pending = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E0AF68")) // amber

	// Done styles a `[x]` ticket (should be rare on the front page).
	Done = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#7DCFFF")). // cyan
		Strikethrough(true)

	// Epic styles an epic block.
	Epic = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#BB9AF7")) // violet

	// Tag styles inline tag chips.
	Tag = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#73DACA")). // teal
		Background(lipgloss.Color("#1F2335")).
		Padding(0, 1)

	// Bad is used for violations.
	Bad = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#F7768E")) // red

	// Good is used for "all clear" lines.
	Good = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#9ECE6A")) // green

	// Warn is used for non-fatal notices.
	Warn = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E0AF68")) // amber

	// CapOK / CapTight / CapOver style the "N of 5" indicator.
	CapOK    = lipgloss.NewStyle().Foreground(lipgloss.Color("#9ECE6A"))
	CapTight = lipgloss.NewStyle().Foreground(lipgloss.Color("#E0AF68"))
	CapOver  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F7768E"))
)

// CapColor picks a style based on the current Now count.
func CapColor(n, cap int) lipgloss.Style {
	switch {
	case n > cap:
		return CapOver
	case n == cap, n == cap-1:
		return CapTight
	default:
		return CapOK
	}
}
