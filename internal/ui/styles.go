package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/dewald-L/dock/plugin"
)

// styles are built from the host theme (ctx.Theme), so lazysheet follows
// dock's colours and, standalone, the terminal's ANSI palette.
type styles struct {
	th plugin.Theme

	border, borderFocus lipgloss.Style
	title, titleFocus   lipgloss.Style
	text, bold, muted   lipgloss.Style
	accent, accentBold  lipgloss.Style
	cursor, cursorDim   lipgloss.Style
	ok, warn, err       lipgloss.Style
	today               lipgloss.Style
	question            lipgloss.Style
	helpKey             lipgloss.Style
	statusBar           lipgloss.Style
	modal               lipgloss.Style
}

func newStyles(th plugin.Theme) styles {
	if th == (plugin.Theme{}) {
		th = plugin.DefaultTheme()
	}
	s := lipgloss.NewStyle()
	return styles{
		th:          th,
		border:      s.Foreground(th.Border),
		borderFocus: s.Foreground(th.Accent),
		title:       s.Foreground(th.Muted).Bold(true),
		titleFocus:  s.Foreground(th.Accent).Bold(true),
		text:        s,
		bold:        s.Bold(true),
		muted:       s.Foreground(th.Muted),
		accent:      s.Foreground(th.Accent),
		accentBold:  s.Foreground(th.Accent).Bold(true),
		cursor:      s.Reverse(true).Bold(true),
		cursorDim:   s.Bold(true).Underline(true),
		ok:          s.Foreground(th.Ok),
		warn:        s.Foreground(th.Warn),
		err:         s.Foreground(th.Err).Bold(true),
		today:       s.Foreground(th.Accent).Bold(true),
		question:    s.Foreground(th.Accent).Bold(true),
		helpKey:     s.Foreground(th.Accent).Bold(true),
		statusBar:   s.Foreground(th.Muted),
		modal:       s.Border(lipgloss.RoundedBorder()).BorderForeground(th.Accent).Padding(1, 2),
	}
}

// box draws a panel of exactly w×h cells with the title in its top border,
// lazygit-style. Body lines are clipped to the inner width and height.
func (s styles) box(title string, body []string, w, h int, focused bool) string {
	if w < 4 || h < 2 {
		return ""
	}
	bs, ts := s.border, s.title
	if focused {
		bs, ts = s.borderFocus, s.titleFocus
	}
	inner := w - 4
	title = ansi.Truncate(title, w-5, "…")
	fill := w - 3 - ansi.StringWidth(title)
	var b strings.Builder
	b.WriteString(bs.Render("╭─") + ts.Render(title) + bs.Render(strings.Repeat("─", max(fill, 0))+"╮"))
	side := bs.Render("│")
	for i := 0; i < h-2; i++ {
		line := ""
		if i < len(body) {
			line = ansi.Truncate(body[i], inner, "…")
		}
		pad := inner - ansi.StringWidth(line)
		b.WriteString("\n" + side + " " + line + strings.Repeat(" ", max(pad, 0)) + " " + side)
	}
	b.WriteString("\n" + bs.Render("╰"+strings.Repeat("─", w-2)+"╯"))
	return b.String()
}

// fit pads or clips a styled line to exactly w cells.
func fit(line string, w int) string {
	line = ansi.Truncate(line, w, "…")
	return line + strings.Repeat(" ", max(w-ansi.StringWidth(line), 0))
}

const (
	dayMinutes = 8 * 60 // a full day, 100% on the progress bar
	barCells   = 16     // half an hour per cell
)

// dayBar is a progress bar of mins against a full day: green, or the warning
// colour past 8 hours (osheet's day_bar), cells wide.
func (s styles) dayBar(mins, cells int) string {
	n := min(mins, dayMinutes) * cells * 2 / dayMinutes
	full, half := n/2, n%2
	bar := strings.Repeat("█", full) + strings.Repeat("▌", half)
	st := s.ok
	if mins > dayMinutes {
		st = s.warn
	}
	return st.Render(bar) + s.muted.Render(strings.Repeat("░", cells-full-half))
}

// wrapText word-wraps plain text to width, splitting words longer than a line.
func wrapText(text string, width int) []string {
	if width < 1 {
		return []string{text}
	}
	var lines []string
	for _, para := range strings.Split(text, "\n") {
		words := strings.Fields(para)
		line := ""
		for _, w := range words {
			for ansi.StringWidth(w) > width {
				if line != "" {
					lines = append(lines, line)
					line = ""
				}
				r := []rune(w)
				lines = append(lines, string(r[:width]))
				w = string(r[width:])
			}
			switch {
			case line == "":
				line = w
			case ansi.StringWidth(line)+1+ansi.StringWidth(w) <= width:
				line += " " + w
			default:
				lines = append(lines, line)
				line = w
			}
		}
		lines = append(lines, line)
	}
	return lines
}
