// Package dockplugin exposes lazysheet as a plugin for the dock terminal host
// (github.com/dewald-L/dock). The same plugin also runs standalone: see
// main.go, which uses plugin.RunStandalone.
//
// lazysheet keeps no data of its own: it reads osheet's config
// ($XDG_CONFIG_HOME/osheet) and reads and writes osheet's timesheet files, so
// dock's plugin data directory is not used.
package dockplugin

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/dewald-L/dock/plugin"

	"github.com/dewald-L/lazysheet/internal/ui"
)

// Importing this package registers lazysheet with dock:
//
//	dock plugin install github.com/dewald-L/lazysheet/dockplugin
func init() { plugin.Register(Plugin{}) }

// Plugin is lazysheet as a dock plugin.
type Plugin struct {
	// Standalone keeps lazysheet's own status and help bars (used by main).
	Standalone bool
}

func (Plugin) Manifest() plugin.Manifest {
	return plugin.Manifest{
		ID:          "lazysheet",
		Name:        "lazysheet",
		Description: "Timesheets: log, browse, edit and total osheet entries",
		Version:     "0.1.0",
		Display:     plugin.DisplayFull,
		Lifecycle:   plugin.Persistent,
		MinWidth:    70,
		MinHeight:   18,
	}
}

func (p Plugin) New(ctx plugin.Context) (tea.Model, error) {
	return ui.New(ctx.Theme, !p.Standalone), nil
}
