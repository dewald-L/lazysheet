# lazysheet

A lazygit-style terminal UI for [osheet](https://github.com/dewald-L/osheet)
timesheets. Log an entry, see what you've logged on each day of the week and
each week of the month, edit or delete entries, and total your hours per
project and entry type, all from keyboard-driven panels.

lazysheet is a port of osheet's behaviour, not a replacement for it. Both use
the same config, template and timesheet files, so you can log with either one
and see the result in the other.

## Shared with osheet

lazysheet keeps no data of its own. It reads osheet's config and writes
straight into osheet's timesheet files:

| | Path |
|---|---|
| Config | `$XDG_CONFIG_HOME/osheet/config.json` (default `~/.config/osheet/config.json`) |
| Template | `template.xlsx` next to the config (osheet's stored copy) |
| Timesheets | the folder set in osheet's setup (default `~/timesheets`) |

It uses osheet's projects, entry types, billable flags, locations and
sentiments, the `defaults`, the `ask_every_time` flags and the grouping:

| Grouping | File |
|---|---|
| Week (Mon–Sun) | `~/timesheets/2026_November/09-15_November.xlsx` |
| Month | `~/timesheets/2026_November/2026-11.xlsx` |

A week that spans two months is filed under the month of its Monday. A new
file starts as a copy of the template. `OSHEET_FILE=/path/to/file.xlsx`
writes everything to one file instead, as it does for osheet.

There is no setup in lazysheet. If osheet isn't configured yet, lazysheet
says so and asks you to run `osheet --setup` first. It also skips osheet's
one-off migrations (old CSV and ungrouped files), so run osheet once after
upgrading it.

### The Excel files

Like osheet, lazysheet edits the `.xlsx` as a zip of XML with Go's standard
library (`archive/zip` plus text edits), never through a spreadsheet library,
so the template's dropdowns, validations and `TimesheetEntry` table survive.
Every write keeps the sheet's rows, `sharedStrings.xml`, the `<dimension>`,
the data-validation ranges and the table's range consistent, and stores dates
as Excel serial numbers in a date style. The tests check that lazysheet and
osheet produce byte-identical XML for the same changes.

## Running it

```sh
go build -o lazysheet .
./lazysheet
```

Go 1.24+ is required. `$EDITOR` (or `$VISUAL`) is used to write long
descriptions, and `wl-copy`, `xclip` or `xsel` to copy the folder path.

### Inside dock

lazysheet is also a plugin for the [dock](https://github.com/dewald-L/dock)
terminal host (plugin id `lazysheet`, full-screen tab, at least 70×18).
Install it into dock with:

```sh
dock plugin install github.com/dewald-L/lazysheet/dockplugin
# or from this checkout:
dock plugin install --path . github.com/dewald-L/lazysheet/dockplugin
```

Then run `dock lazysheet`, or open it from dock's home screen (its number key,
or select its block and press enter) or palette. On dock's home screen its
block shows today's and this week's hours against 40h
(`Today 1h15 · this week 18h15 / 40h`) and a bar chart of hours per day against
the 8h target, with today highlighted and days past 8h in the warning colour. The
block gets a warning dot once today passes 8h. On a screen too short or narrow for the
chart it falls back to one text line of bars (`Mo█ Tu▆ We▂ Th· Fr· Sa· Su·`). Without an osheet config it says to run `osheet --setup`. Inside
dock:

- It reads and writes the same osheet files as standalone. dock's plugin
  data directory isn't used.
- dock's bottom bar replaces lazysheet's status and help bars. It shows the
  same key hints and messages.
- `q` closes the plugin (dock keeps running). `ctrl+c` quits dock.
- The tab title shows the current week (`lazysheet · 09-15 Nov`).
- Palette (`ctrl+g :`) commands: new entry, hours today, hours this week,
  hours this month, copy folder path, help.
- Entries are saved as soon as the form is done, so there's nothing to flush
  when you switch away or close it. An unfinished form stays open in the
  background tab.

## Layout

```
╭─ [1] Days ──────────╮╭─ [3] Entry / Hours ──────────────────╮
│ the selected week,  ││ the selected entry's details, or the │
│ Mon–Sun, with a bar ││ Hours table (t/w/m)                  │
│ per day             ││                                      │
╰─────────────────────╯│                                      │
╭─ [2] Entries ───────╮│                                      │
│ the week's entries  ││                                      │
│ newest first        ││                                      │
╰─────────────────────╯╰──────────────────────────────────────╯
```

- **Days**: each day of the selected week (this week to start with), with the
  time logged and a progress bar towards 8 hours (green, then the theme's
  warning colour past 8 hours). The title shows the week and its total. `[` and
  `]` page to the previous and next week of the month.
- **Entries**: the selected week's entries, newest first.
- **Entry / Hours**: the selected entry in full (day, project, entry type,
  time, location, sentiment, description, billable flag, ticket number and
  file, then the current period's timesheet folder), or, after `t`, `w` or `m`,
  osheet's `--hours` table for today, this week or this month, busiest project
  first. Moving in Entries or Days, or `esc` in Hours, brings the entry back.
  The entry form (`n`, `e`) pops up as a modal over all three panels.

## Logging an entry

`n` opens the form as a popup. It asks osheet's questions in order and
fills in the fields above as you answer:

1. **Day**: any day of the current week, each shown with its logged time and
   bar. Defaults to today.
2. **Project**.
3. **Entry type**, from the chosen project's list. Billable comes from the
   project.
4. **Time**: `1:20`, `1h20`, `80m`, `1.5` or `90` (minutes when over 24),
   rounded **up** to the next 15 minutes.
5. **Location** (WorkedFrom) and 6. **Sentiment**, from the template.
7. **Description**: required, at most 255 characters. `ctrl+e` writes it in
   `$EDITOR`.

Questions whose `ask_every_time` flag is `false` answer themselves with the
default and show `(skipped)`, with osheet's fallbacks: the day is today, the
time is `defaults.time`, and a default that isn't valid (no default time, or
an entry type that isn't in the project's list) is asked anyway.

Each new entry starts from the previous one's day, project, entry type,
location and sentiment, like `osheet --multi`. Press `n` in the Days panel
after picking a day to start on that day instead.

## Editing and deleting

`e` (or `enter` in Entries) edits the selected entry. Every question is asked,
starting from the entry's values. The day can move within the entry's own
week: the row is updated in place, or moved to another file when the new day
belongs to another week's or month's file. The ticket number is kept.

`d` asks `Delete this entry? (y/N)`. `y` deletes it and the rows below move
up. Any other key keeps it.

Before an edit or delete, lazysheet checks that the row still holds what it
loaded, so it never changes the wrong row if osheet edited the file in the
meantime. Data is reloaded when the plugin regains focus, with `r`, and at
midnight.

## Keybindings

| Key | Action |
|---|---|
| `1` `2` `3`, `tab` / `shift+tab` | jump to / cycle Days, Entries, Entry (or Hours) |
| `[` / `]` | previous / next week |
| `j`/`k`, `↑`/`↓`, `g`/`G` | move, top, bottom |
| `enter` / `l` | Days: show in Entries. Entries: edit |
| `h` / `esc` | Entries: back to Days. Hours: back to the entry |
| `n` | new entry |
| `e` | edit the selected entry |
| `d` | delete the selected entry (asks first) |
| `t` / `w` / `m` | show hours today, this week, this month in the right pane (`←`/`→` to change) |
| `y` | copy the current timesheet folder's path |
| `r` | reload from disk |
| `?` | all keys |
| `q` | quit (close, inside dock) |

In the form:

| Key | Action |
|---|---|
| `j`/`k`, `enter`/`space` | choose an option |
| `1`–`9` | pick an option by number (`1` then `2` for 12) |
| `enter` | next question; the last one saves |
| `shift+tab` | back to the previous question (skipped ones are passed over) |
| `ctrl+e` | edit the description in `$EDITOR` |
| `esc` | cancel, nothing is saved |

Typed keys, `q` and `?` included, go to the focused text input.

## Testing

```sh
go test ./...                                         # unit + osheet interop tests
LAZYSHEET_TEMPLATE=~/.config/osheet/template.xlsx go test ./internal/sheet/  # also against your real template (read only)
go build -o lazysheet . && python3 -I e2e/run.py --dock   # drive the binary in a pty, standalone and in dock
```

The tests never touch the real osheet config or timesheets. The Go tests set
`HOME` and `XDG_CONFIG_HOME` to a temp folder and use a synthetic template
with the same structure as the real one. The interop tests run osheet's own
Python code (`~/Projects/osheet/osheet`, or `OSHEET_SRC`) through
`testdata/osheet_interop.py`. `e2e/run.py` copies `config.json` and
`template.xlsx` into a temp HOME, and with `--dock` installs lazysheet into a
sandboxed dock (`DOCK_CONFIG_DIR`, `DOCK_DATA_DIR`, `XDG_CACHE_HOME`).

## Project layout

```
main.go              entry point (runs the dock plugin standalone via
                       plugin.RunStandalone)
dockplugin/          the dock plugin: manifest, embedding, home screen Summary
internal/sheet/      osheet's config, paths, durations and Excel I/O, ported
                       from osheet (zip + XML text edits, standard library only)
internal/sheet/sheettest/  synthetic template for tests
internal/ui/         Bubble Tea model: panels, entry form, hours summary
e2e/                 pty harness and end-to-end scenarios
testdata/            bridge to osheet's Python for the interop tests
```
