// Command lazysheet is a lazygit-style terminal UI for osheet timesheets:
// log entries, browse the current week and month, edit and delete entries,
// and see hours per project and entry type.
//
// It shares osheet's config ($XDG_CONFIG_HOME/osheet/config.json and the
// stored template.xlsx) and writes into the same week or month files, so the
// two tools can be used side by side. OSHEET_FILE overrides the output file,
// as it does for osheet.
//
// The same UI also runs as a plugin inside the dock terminal host; see the
// dockplugin package.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/dewald-L/dock/plugin"

	"github.com/dewald-L/lazysheet/dockplugin"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: lazysheet\n\nLog, browse, edit and total osheet timesheet entries. Run osheet --setup first.")
	}
	flag.Parse()
	if flag.NArg() > 0 {
		flag.Usage()
		os.Exit(2)
	}
	if err := plugin.RunStandalone(dockplugin.Plugin{Standalone: true}, plugin.Context{}); err != nil {
		fmt.Fprintf(os.Stderr, "lazysheet: %v\n", err)
		os.Exit(1)
	}
}
