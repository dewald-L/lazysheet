# lazysheet

Lazygit-style terminal UI (Bubble Tea) for osheet timesheets. See `README.md`
for features and keys. osheet (`~/Projects/osheet/osheet`) is the spec: when
in doubt, do what it does.

## Commands

```sh
go build -o lazysheet .   # build
go vet ./...               # vet
go test ./...              # unit + osheet interop tests (needs python3 and osheet)
LAZYSHEET_TEMPLATE=<copy of the real template> go test ./internal/sheet/
python3 -I e2e/run.py --dock   # pty end-to-end, standalone and in a dock sandbox
```

## Architecture

- Module `github.com/dewald-L/lazysheet`. Depends on the published
  `github.com/dewald-L/dock` (`plugin` package only) at a commit (bump with
  `go get github.com/dewald-L/dock@<commit>`); use a temporary `go.work`, not a
  committed replace, to develop against unpushed dock changes. `dockplugin`'s
  `init()` calls `plugin.Register`; dock compiles it in with
  `dock plugin install [--path ~/Projects/lazysheet] github.com/dewald-L/lazysheet/dockplugin`.
  After changing the plugin, run `dock build`.
- `main.go` runs `dockplugin.Plugin{Standalone: true}` through
  `plugin.RunStandalone`, so standalone and dock share one code path.
- `dockplugin/`: id `lazysheet`, DisplayFull, Persistent, min 90x24. No data
  dir: the files are osheet's. `summary.go` is dock's home screen
  `plugin.Summarizer` (on the Plugin value): today/week hours vs 40h,
  `Widgets` (a text headline and a 7-bar `plugin.BarChart` against the 8h
  target: today `Focus`, days past 8h `LevelWarn`) with `Lines` (headline + the
  `dayBar` text) as the fallback dock shows when the chart doesn't fit, warn
  past 8h today, an `osheet --setup` line without config. It runs
  off the UI goroutine, possibly while lazysheet is open, so it only reads
  (`sheet.LoadConfig`, `EntriesBetween`) and keeps no state. Tests call
  `summary(today)` with a fixed date.
- `internal/sheet/sheettest`: the synthetic template (`WriteTemplate`) used by
  the sheet and dockplugin tests.
- `internal/sheet/`: osheet's config (`config.go`), dates, grouping, paths and
  durations (`dates.go`), the xlsx port (`xlsx.go`) and entries, save, update,
  move, delete and the hours summary (`entries.go`). No UI concerns.
- `internal/ui/`: `model.go` (App, loading, derived data), `update.go` (keys,
  save/delete commands, clipboard, `$EDITOR`), `form.go` (the entry form, a
  port of osheet's `entry_steps`), `view.go` (panels, `helpPairs`, help
  screen), `dock.go` (`Commands()`, `Help()`), `styles.go` (styles from
  `ctx.Theme`, the titled `box`).
- Embedded mode (`App.embedded`, unless Standalone): no status or help bars.
  `Update` wraps `update` and forwards `status`/`errMsg` via
  `plugin.Notify`/`plugin.Error`, and sends `plugin.SetTitle` when the week
  changes. Clipboard tools and `$EDITOR` run through `plugin.Exec`, never
  `tea.ExecProcess`. No file I/O in `Update`/`View`: loads, saves and deletes
  are `tea.Cmd`s returning `loadedMsg`/`savedMsg`/`deletedMsg`.
- Key hints: `helpPairs` (view.go) feeds the standalone help bar and dock's
  `Help()`; keep it at 3–6 keys. The full list is `renderHelp`. Keys are
  dispatched in `handleKey`/`handleFormKey` (update.go). Change all three
  together.

## Excel I/O (keep it osheet-compatible)

- Port, don't redesign: `appendRows`/`updateRow`/`save` mirror osheet's
  `append_rows`/`update_row`/`save_workbook` regex for regex. Never use a
  spreadsheet library or re-serialise XML; rewriting breaks the template's
  validations and table.
- Every write must keep consistent: the entry sheet's rows, `sharedStrings.xml`
  (+ `count`/`uniqueCount`), `<dimension>`, the `sqref="X2:Xn"` validations,
  the table's `ref` (and autoFilter), and dates as serials in a
  `numFmtId="14"` style. Go regexp has no backreferences: the sqref rule
  checks both columns match in code.
- `Entry.Row` is the index in the sheet's `<row>` list (osheet's `_row`), not
  the `r` number. Edits and deletes re-read the file first (`checkUnchanged`)
  so a file osheet changed meanwhile can't get the wrong row edited.
- `TestByteIdenticalToOsheet` compares lazysheet's XML with osheet's for the
  same writes. Keep it passing.

## Testing rules

- Never read or write the real `~/.config/osheet` or `~/timesheets`. Go tests
  set `HOME`/`XDG_CONFIG_HOME` to temp dirs in `TestMain`; e2e copies
  `config.json` and `template.xlsx` into a temp HOME. The real template isn't
  in the repo (it's the employer's file): tests build a synthetic one
  (`template_test.go`).
- The pty harness (`e2e/harness.py`) answers dock's startup queries
  (`ESC]11;?` and `ESC[6n`). Fake `wl-copy` and `$EDITOR` scripts in the
  sandbox's PATH check the clipboard and editor paths.

## Gotchas

- `weeks()` lists whole Monday–Sunday weeks overlapping the month (osheet's
  `--list` clips them to the month); data is loaded from the first Monday to
  the end of the month or the current week, whichever is later.
- `n` in Days uses the highlighted day only if you moved there (`dayPicked`);
  otherwise a new entry starts from the previous one's day, like `--multi`.
- osheet's `ask_flags`: only a JSON `false` skips a question; a missing flag
  asks it, except `sentiment`.
