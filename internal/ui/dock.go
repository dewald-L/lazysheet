package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/dewald-L/dock/plugin"
)

// Integration with the dock host (github.com/dewald-L/dock). These methods
// implement dock's optional plugin interfaces; standalone runs use them too
// through plugin.RunStandalone.

// commandMsg is delivered when a palette command is picked in dock.
type commandMsg struct{ name string }

// Commands implements plugin.Commander.
func (a *App) Commands() []plugin.Command {
	return []plugin.Command{
		{Name: "new entry", Desc: "log a timesheet entry", Msg: commandMsg{"new"}},
		{Name: "hours today", Desc: "time logged today per project and entry type", Msg: commandMsg{periodDay}},
		{Name: "hours this week", Desc: "time logged this week per project and entry type", Msg: commandMsg{periodWeek}},
		{Name: "hours this month", Desc: "time logged this month per project and entry type", Msg: commandMsg{periodMonth}},
		{Name: "copy folder path", Desc: "copy the current timesheet folder's path", Msg: commandMsg{"copy"}},
		{Name: "help", Desc: "all keys", Msg: commandMsg{"help"}},
	}
}

func (a *App) runCommand(c commandMsg) (tea.Model, tea.Cmd) {
	if c.name == "help" {
		a.showHelp = true
		return a, nil
	}
	if a.cfg == nil {
		a.status = "Run osheet --setup first"
		return a, nil
	}
	a.showHelp = false
	switch c.name {
	case "new":
		if a.form != nil {
			a.status = "Finish or cancel (esc) the open entry first"
			return a, nil
		}
		return a.startNew(nil)
	case periodDay, periodWeek, periodMonth:
		a.setPeriod(c.name)
		if a.form == nil {
			a.focus = panelPane
		}
	case "copy":
		return a, a.copyFolder()
	}
	return a, nil
}

// Help implements plugin.Helper: the same contextual hints as the
// standalone help bar.
func (a *App) Help() []plugin.KeyHelp {
	pairs := a.helpPairs()
	out := make([]plugin.KeyHelp, len(pairs))
	for i, p := range pairs {
		out[i] = plugin.KeyHelp{Key: p.k, Desc: p.v}
	}
	return out
}
