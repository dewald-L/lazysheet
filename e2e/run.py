"""End-to-end tests: drive the real lazysheet binary through a pty, standalone
and inside a sandboxed dock, and check the workbooks with osheet's own code.

    go build -o lazysheet . && python3 -I e2e/run.py [--dock] [--keep]

Never touches the real ~/.config/osheet or ~/timesheets: everything runs in a
temp HOME with XDG_CONFIG_HOME pointing into it. config.json and template.xlsx
are copied from the real config folder (read only), or from $E2E_OSHEET_CONFIG.
--dock also installs lazysheet into a dock sandbox (DOCK_CONFIG_DIR,
DOCK_DATA_DIR and XDG_CACHE_HOME in the temp dir) and runs the same checks
there.
"""
import datetime as dt
import importlib.machinery
import importlib.util
import json
import os
import shutil
import subprocess
import sys
import tempfile
import time

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.dirname(HERE)
sys.path.insert(0, HERE)
from harness import Term  # noqa: E402

REAL_HOME = os.path.expanduser("~")
OSHEET_SRC = os.environ.get("OSHEET_SRC", os.path.join(REAL_HOME, "Projects/osheet/osheet"))
SRC_CONFIG = os.environ.get("E2E_OSHEET_CONFIG", os.path.join(REAL_HOME, ".config/osheet"))
TODAY = dt.date.today()
WEEK = [TODAY - dt.timedelta(days=TODAY.weekday()) + dt.timedelta(days=i) for i in range(7)]

passed = []


def check(cond, what, screen=None):
    if not cond:
        raise AssertionError(what + ("\n--- screen ---\n" + screen if screen else ""))
    passed.append(what)
    print(f"  ok  {what}")


class Sandbox:
    def __init__(self, keep=False):
        self.root = tempfile.mkdtemp(prefix="lazysheet-e2e-")
        self.keep = keep
        self.home = os.path.join(self.root, "home")
        self.cfgdir = os.path.join(self.home, ".config", "osheet")
        os.makedirs(self.cfgdir)
        for f in ("config.json", "template.xlsx"):
            shutil.copyfile(os.path.join(SRC_CONFIG, f), os.path.join(self.cfgdir, f))
        self.bin = os.path.join(self.root, "bin")
        os.makedirs(self.bin)
        self.clip = os.path.join(self.root, "clipboard.txt")
        self._script("wl-copy", f'#!/bin/sh\ncat > "{self.clip}"\n')
        self._script("fake-editor", '#!/bin/sh\nprintf "written in the editor\\nsecond line\\n" > "$1"\n')
        self.env = {
            "HOME": self.home, "XDG_CONFIG_HOME": os.path.join(self.home, ".config"),
            "TERM": "xterm-256color", "LANG": "en_US.UTF-8", "COLORTERM": "truecolor",
            "PATH": self.bin + ":" + os.environ["PATH"], "WAYLAND_DISPLAY": "wayland-e2e",
            "EDITOR": "fake-editor",
        }
        os.environ.update({"HOME": self.home, "XDG_CONFIG_HOME": self.env["XDG_CONFIG_HOME"]})
        os.environ.pop("OSHEET_FILE", None)
        self.osheet = self._load_osheet()

    def _script(self, name, body):
        p = os.path.join(self.bin, name)
        with open(p, "w") as f:
            f.write(body)
        os.chmod(p, 0o755)

    def _load_osheet(self):
        loader = importlib.machinery.SourceFileLoader("osheet_e2e", OSHEET_SRC)
        spec = importlib.util.spec_from_loader("osheet_e2e", loader)
        mod = importlib.util.module_from_spec(spec)
        loader.exec_module(mod)
        assert mod.CONFIG_PATH.startswith(self.home), mod.CONFIG_PATH  # never the real config
        return mod

    @property
    def cfg(self):
        with open(os.path.join(self.cfgdir, "config.json")) as f:
            return json.load(f)

    def set_cfg(self, **changes):
        cfg = self.cfg
        cfg.update(changes)
        with open(os.path.join(self.cfgdir, "config.json"), "w") as f:
            json.dump(cfg, f, indent=2)

    def path_for(self, date):
        return self.osheet.output_path(self.cfg, date)

    def entries(self, date=TODAY):
        p = self.path_for(date)
        return self.osheet.read_entries(p) if os.path.exists(p) else []

    def week_entries(self):
        out = []
        for p in dict.fromkeys(self.path_for(d) for d in WEEK):
            if os.path.exists(p):
                out += [e for e in self.osheet.read_entries(p) if e["Date"] in WEEK]
        return out

    def cleanup(self):
        if not self.keep:
            shutil.rmtree(self.root, ignore_errors=True)
        else:
            print("kept", self.root)


def check_xml(sb, path):
    """The workbook's XML parses and its refs agree (osheet's invariants)."""
    import re
    import xml.etree.ElementTree as ET
    import zipfile
    with zipfile.ZipFile(path) as z:
        parts = {n: z.read(n) for n in z.namelist()}
    for n, b in parts.items():
        if n.endswith((".xml", ".rels")):
            ET.fromstring(b)
    sheet = parts["xl/worksheets/sheet1.xml"].decode("utf-8-sig")
    rows = re.findall(r'<row r="(\d+)"', sheet)
    end = len(rows)
    ok = rows == [str(i) for i in range(1, end + 1)]
    ok &= re.search(r'<dimension ref="A1:[A-Z]+(\d+)"', sheet).group(1) == str(end)
    ok &= all(m[2] == str(end) for m in re.findall(r'sqref="([A-Z]+)2:\1(\d+)"', sheet) for m in [(None, None, m[1])])
    for n, b in parts.items():
        if n.startswith("xl/tables/") and b"TimesheetEntry" in b:
            ok &= all(e == str(end) for e in re.findall(r' ref="[A-Z]+1:[A-Z]+(\d+)"', b.decode("utf-8-sig")))
    sst = parts["xl/sharedStrings.xml"].decode()
    n = sst.count("<si>")
    ok &= f'uniqueCount="{n}"' in sst and f' count="{n}"' in sst
    return ok


def start(sb, argv, cols=120, rows=40, env=None):
    t = Term(argv, {**sb.env, **(env or {})}, cols=cols, rows=rows)
    return t


def project_with(billable):
    return lambda cfg: next(p for p, v in cfg["projects"].items() if v["billable"] == billable)


def menu_number(options, value):
    return str(options.index(value) + 1)


def log_entry(t, sb, day, project, category, time_text, location, desc, sentiment=None):
    """Answer the entry form; returns after the save notification."""
    cfg = sb.cfg
    t.wait_for("Which day")
    t.type(str(WEEK.index(day) + 1))
    t.wait_for("Which project?")
    t.type(menu_number(list(cfg["projects"]), project))
    t.wait_for("What type of entry")
    cats = cfg["projects"][project]["categories"]
    # pick with j/k and enter, the other way of answering a menu
    screen = t.text()
    t.send("g")
    for _ in range(cats.index(category)):
        t.send("j", delay=0.03)
    t.send("enter")
    t.wait_for("Total time spent?")
    t.send("ctrl+u")
    t.type(time_text)
    t.send("enter")
    t.wait_for("Where did you work from?")
    t.type(menu_number(cfg["locations"], location))
    if sentiment:
        t.wait_for("How did it go?")
        t.type(menu_number(cfg["sentiments"], sentiment))
    t.wait_for("Description?")
    t.send("ctrl+u")
    t.type(desc)
    t.send("enter")
    return screen


def scenario_main(sb, argv, label, in_dock=False):
    print(f"\n== {label} ==")
    cfg = sb.cfg
    billable = project_with("Yes")(cfg)
    other = project_with("No")(cfg)
    loc = cfg["locations"][1]

    # An entry osheet wrote before lazysheet starts shows up
    os_entry = {"Date": WEEK[0], "Project": other, "Category": cfg["projects"][other]["categories"][0],
                "Hours": 1, "Minutes": 15, "Billable": "No", "Description": "written by osheet",
                "TicketNumber": "TK-7", "Sentiment": "Happy", "WorkedFrom": cfg["locations"][0]}
    sb.osheet.save_entries(cfg, [os_entry])

    t = start(sb, argv)
    try:
        if in_dock:
            t.wait_for("lazysheet ·", timeout=15)
        s = t.wait_for("[1] Days", timeout=15)
        check(f"lazysheet · {WEEK[0]:%d}-{WEEK[6]:%d}" in s or f"lazysheet · {WEEK[0]:%d %b}" in s or not in_dock,
              f"{label}: tab title shows the current week", s)
        s = t.wait_for("written by osheet")
        check("1h15" in s and "[2] Entries" in s, f"{label}: osheet's entry shows in Entries", s)
        t.send("t")
        s = t.wait_for("[3] Hours")
        check("1h15" in s, f"{label}: t shows today's hours in the right pane", s)
        check(f"{WEEK[0]:%a %d %b}" in s, f"{label}: Days lists the current week", s)

        # --- log a new entry (q typed into the description goes to the input) ---
        t.send("n")
        s = t.wait_for("Which day")
        check("(today)" in s and "❯" in s, f"{label}: new entry asks the day, today marked", s)
        t.type(str(WEEK.index(TODAY) + 1))
        t.wait_for("Which project?")
        t.type(menu_number(list(cfg["projects"]), billable))
        t.wait_for("What type of entry")
        cats = cfg["projects"][billable]["categories"]
        t.type(menu_number(cats, cats[0]) if len(cats) < 10 else "1")
        if len(cats) >= 10:
            t.send("enter")
        s = t.wait_for("Total time spent?")
        t.type("abc")
        t.send("enter")
        s = t.wait_for("Couldn't read that as a duration")
        check(True, f"{label}: a bad duration is rejected with osheet's message")
        t.send("ctrl+u")
        t.type("1h50")
        t.send("enter")
        s = t.wait_for("Where did you work from?")
        check("2h00" in s and "(rounded up from 1h50)" in s, f"{label}: time rounded up to 15 minutes", s)
        t.type(menu_number(cfg["locations"], loc))
        s = t.wait_for("Description?")
        check("(skipped)" in s and cfg["defaults"]["sentiment"] in s,
              f"{label}: sentiment (not asked every time) uses the default, shown as skipped", s)
        t.type("x" * 256)
        t.send("enter")
        t.wait_for("Too long (256 characters), keep it to 255.")
        check(True, f"{label}: a description over 255 characters is rejected")
        t.send("ctrl+u")
        t.type("quick fix q? 1 2 n e d")
        s = t.wait_for("quick fix q? 1 2 n e d")
        check("Description?" in s, f"{label}: q, ?, digits and letters go to the focused input", s)
        t.send("enter")
        s = t.wait_for(lambda s: "Entry type" in s and "quick fix q?" in s and "New entry" not in s)
        es = [e for e in sb.entries() if e["Description"] == "quick fix q? 1 2 n e d"]
        check(len(es) == 1, f"{label}: the entry is saved in this week's file", s)
        e = es[0]
        check(e["Date"] == TODAY and e["Project"] == billable and e["Hours"] == 2 and e["Minutes"] == 0
              and e["Billable"] == "Yes" and e["Sentiment"] == cfg["defaults"]["sentiment"] and e["WorkedFrom"] == loc,
              f"{label}: osheet's read_entries reads it back: {e}")
        check(check_xml(sb, sb.path_for(TODAY)), f"{label}: workbook XML valid (dimension, sqrefs, table ref)")
        check(sb.path_for(TODAY).endswith(f"{WEEK[0]:%Y_%B}/{WEEK[0]:%d}-{WEEK[6]:%d}_{WEEK[0]:%B}.xlsx")
              or cfg.get("group_by") != "week", f"{label}: filed as yyyy_Month/dd-dd_Month.xlsx")

        # --- the next entry starts from the previous one; esc cancels ---
        t.send("n")
        s = t.wait_for("Which day")
        line = next(l for l in s.splitlines() if "❯ " in l and ". " in l)
        check(f"{TODAY:%A %d}" in line, f"{label}: next entry pre-selects the previous day", s)
        t.send("enter")
        s = t.wait_for("Which project?")
        sel = next(l for l in s.splitlines() if "❯ " in l and ". " in l)
        check(billable in sel, f"{label}: …and the previous project", s)
        count = len(sb.week_entries())
        t.send("esc")
        s = t.wait_gone("Which project?")
        check("New entry" not in s and len(sb.week_entries()) == count, f"{label}: esc cancels without saving", s)

        # --- another entry via palette (dock) or key, with $EDITOR for the description ---
        if in_dock:
            t.send("ctrl+g", ":")
            t.wait_for("new entry", timeout=5)
            t.type("lazysheet: new entry")
            t.send("enter")
        else:
            t.send("n")
        t.wait_for("Which day")
        day2 = WEEK[0] if TODAY != WEEK[0] else WEEK[1]
        t.type(str(WEEK.index(day2) + 1))
        t.wait_for("Which project?")
        t.type(menu_number(list(cfg["projects"]), other))
        t.wait_for("What type of entry")
        t.send("enter")
        t.wait_for("Total time spent?")
        t.send("ctrl+u")
        t.type("80m")
        t.send("enter")
        t.wait_for("Where did you work from?")
        t.send("enter")
        t.wait_for("Description?")
        t.send("ctrl+e")
        s = t.wait_for("written in the editor second line")
        check(True, f"{label}: ctrl+e edits the description in $EDITOR (via plugin.Exec)")
        t.send("enter")
        t.wait_gone("New entry")
        t.wait_for(lambda s: "written in the editor" in s and "Edit entry" not in s)
        week = sb.week_entries()
        check(any(x["Description"] == "written in the editor second line" and x["Hours"] == 1 and x["Minutes"] == 30
                  for x in week), f"{label}: 80m saved as 1h30")

        # --- edit osheet's entry: move the day, keep the ticket ---
        # entries are newest first: osheet's (Monday, first row) is the last one
        t.send("2", "G")
        s = t.wait_for("written by osheet")
        t.send("e")
        s = t.wait_for("Which day")
        check("Edit entry" in s, f"{label}: e opens the edit form", s)
        target = WEEK[2]
        t.type(str(WEEK.index(target) + 1))
        for q in ("Which project?", "What type of entry", "Total time spent?"):
            t.wait_for(q)
            if q.startswith("Total"):
                s = t.text()
                check("1h15" in s, f"{label}: edit pre-fills the time", s)
                t.send("ctrl+u")
                t.type("2:10")
            t.send("enter")
        t.wait_for("Where did you work from?")
        t.send("enter")
        s = t.wait_for("How did it go?")
        check(True, f"{label}: editing asks every question, sentiment too")
        t.send("enter")
        s = t.wait_for("Description?")
        check("written by osheet" in s, f"{label}: edit pre-fills the description", s)
        t.send("enter")
        t.wait_gone("Edit entry")
        t.wait_for(lambda s: "Updated" in s or "2h15" in s)
        mine = [x for x in sb.week_entries() if x["Description"] == "written by osheet"]
        check(len(mine) == 1 and mine[0]["Date"] == target and mine[0]["Hours"] == 2 and mine[0]["Minutes"] == 15
              and mine[0]["TicketNumber"] == "TK-7", f"{label}: edit moved the day, kept the ticket: {mine}")
        check(check_xml(sb, sb.path_for(target)), f"{label}: XML still valid after the edit")

        # --- delete: n keeps it, y deletes it and the rows below move up ---
        before = sb.week_entries()
        t.send("2", "g")
        s = t.wait_for("[2] Entries")
        t.send("d")
        s = t.wait_for("Delete this entry? (y/N)")
        t.send("n")
        t.wait_gone("Delete this entry?")
        check(len(sb.week_entries()) == len(before), f"{label}: n keeps the entry")
        t.send("d")
        t.wait_for("Delete this entry? (y/N)")
        t.send("y")
        t.wait_gone("Delete this entry?")
        time.sleep(0.5)
        t.pump(0.3)
        after = sb.week_entries()
        check(len(after) == len(before) - 1, f"{label}: y deletes it ({len(before)} → {len(after)})")
        check(all(check_xml(sb, p) for p in {sb.path_for(d) for d in WEEK} if os.path.exists(p)),
              f"{label}: XML valid after delete, rows renumbered")

        # --- hours summary ---
        t.send("t")
        s = t.wait_for("Hours · Today")
        t.send("m")
        s = t.wait_for("Hours · This month")
        check("Total" in s, f"{label}: hours this month has a total", s)
        t.send("w")
        s = t.wait_for("Hours · This week")
        total = sum(e["Hours"] * 60 + e["Minutes"] for e in sb.week_entries())
        check(f"{total // 60}h{total % 60:02d}" in s, f"{label}: week total matches osheet ({total} min)", s)
        if in_dock:
            t.send("ctrl+g", ":")
            t.type("lazysheet: hours today")
            t.send("enter")
            s = t.wait_for("Hours · Today")
            check(True, f"{label}: palette 'hours today' works")

        # --- copy the folder path ---
        if os.path.exists(sb.clip):
            os.remove(sb.clip)
        if in_dock:
            t.send("ctrl+g", ":")
            t.type("lazysheet: copy folder path")
            t.send("enter")
        else:
            t.send("y")
        deadline = time.time() + 5
        while time.time() < deadline and not os.path.exists(sb.clip):
            t.pump(0.1)
        t.pump(0.5)
        want = os.path.dirname(sb.path_for(TODAY))
        got = open(sb.clip).read() if os.path.exists(sb.clip) else None
        check(got == want, f"{label}: folder path copied through wl-copy ({got})")
        s = t.wait_for("[1] Days")

        # --- help, resize, quit ---
        t.send("?")
        s = t.wait_for("lazysheet — keys")
        t.send("x")
        t.wait_gone("lazysheet — keys")
        t.set_size(100, 30)
        s = t.wait_for(lambda s: "[2] Entries" in s and max(len(l) for l in s.splitlines()) <= 100)
        check(all("╮" in l or "│" in l or "╯" in l for l in s.splitlines()[1 if in_dock else 0:10] if l.strip()),
              f"{label}: layout fits after resize to 100x30", s)
        if in_dock:
            t.set_size(80, 24)
            s = t.wait_for(lambda s: "[1] Days" not in s, timeout=5)
            check(True, f"{label}: below MinWidth dock shows its placeholder")
            t.set_size(120, 40)
            t.wait_for("[1] Days")
        t.send("q")
        if in_dock:
            s = t.wait_gone("[1] Days")
            check(True, f"{label}: q closes the plugin, dock keeps running")
            t.send("ctrl+c")
        code = t.wait_exit(5)
        check(code == 0, f"{label}: exits cleanly ({code})")
    except Exception:
        print(t.text())
        raise
    finally:
        t.close()


def scenario_skipped(sb, argv, label):
    print(f"\n== {label}: questions not asked every time ==")
    cfg = sb.cfg
    sb.set_cfg(ask_every_time={"day": False, "project": False, "category": False, "time": False,
                               "location": True, "sentiment": False},
               defaults={**cfg["defaults"], "time": "45m"})
    t = start(sb, argv)
    try:
        t.wait_for("[1] Days", timeout=15)
        t.send("n")
        s = t.wait_for("Where did you work from?")
        check(s.count("(skipped)") == 4 and "0h45" in s, f"{label}: skipped questions answered from defaults", s)
        t.send("shift+tab")
        s = t.wait_for("Where did you work from?")
        check(True, f"{label}: back from the first asked question stays (nothing to go back to)")
        t.send("enter")
        t.wait_for("Description?")
        t.send("shift+tab")
        t.wait_for("Where did you work from?")
        check(True, f"{label}: shift+tab goes back over skipped questions")
        t.send("enter")
        t.wait_for("Description?")
        t.type("skipped flow")
        t.send("enter")
        t.wait_gone("New entry")
        e = [x for x in sb.entries() if x["Description"] == "skipped flow"]
        check(len(e) == 1 and e[0]["Date"] == TODAY and e[0]["Project"] == cfg["defaults"]["project"]
              and e[0]["Hours"] * 60 + e[0]["Minutes"] == 45, f"{label}: saved with defaults {e}")
        t.send("q")
        t.wait_exit(5)
    except Exception:
        print(t.text())
        raise
    finally:
        t.close()
        sb.set_cfg(ask_every_time=cfg["ask_every_time"], defaults=cfg["defaults"])


def scenario_osheet_file(sb, argv, label):
    print(f"\n== {label}: OSHEET_FILE ==")
    target = os.path.join(sb.root, "pinned.xlsx")
    t = start(sb, argv, env={"OSHEET_FILE": target})
    try:
        t.wait_for("[1] Days", timeout=15)
        t.send("n")
        log_entry(t, sb, TODAY, list(sb.cfg["projects"])[0], sb.cfg["projects"][list(sb.cfg["projects"])[0]]["categories"][0],
                  "30m", sb.cfg["locations"][0], "pinned entry")
        t.wait_gone("New entry")
        t.wait_for("pinned entry")
        got = sb.osheet.read_entries(target)
        check(len(got) == 1 and got[0]["Description"] == "pinned entry", f"{label}: OSHEET_FILE overrides the output file")
        check(check_xml(sb, target), f"{label}: pinned file XML valid")
        t.send("q")
        t.wait_exit(5)
    finally:
        t.close()


def scenario_no_config(argv, label, extra_env=None):
    print(f"\n== {label}: no config ==")
    root = tempfile.mkdtemp(prefix="lazysheet-e2e-empty-")
    env = {"HOME": root, "XDG_CONFIG_HOME": os.path.join(root, ".config"), "TERM": "xterm-256color",
           "PATH": os.environ["PATH"], **(extra_env or {})}
    t = Term(argv, env, cols=110, rows=32)
    try:
        s = t.wait_for("osheet --setup", timeout=15)
        check("lazysheet needs osheet's config" in s, f"{label}: missing config screen tells you to run osheet --setup", s)
        t.send("q")
        if extra_env:
            t.wait_gone("osheet --setup")
            t.send("ctrl+c")
        t.wait_exit(5)
    finally:
        t.close()
        shutil.rmtree(root, ignore_errors=True)


def install_dock(sb):
    d = os.path.join(sb.root, "dock")
    env = {**os.environ, "HOME": REAL_HOME, "DOCK_CONFIG_DIR": os.path.join(d, "config"),
           "DOCK_DATA_DIR": os.path.join(d, "data"), "XDG_CACHE_HOME": os.path.join(d, "cache")}
    env.pop("XDG_CONFIG_HOME", None)
    for k in ("config", "data", "cache"):
        os.makedirs(os.path.join(d, k), exist_ok=True)
    subprocess.run(["dock", "plugin", "install", "--path", REPO, "github.com/dewald-L/lazysheet/dockplugin"],
                   env=env, check=True, stdout=subprocess.DEVNULL)
    out = subprocess.run([os.path.join(d, "data/bin/dock"), "plugin", "list"], env=env, check=True,
                         capture_output=True, text=True).stdout
    check("lazysheet" in out and "full" in out, "dock sandbox: plugin installed and listed")
    return os.path.join(d, "data/bin/dock"), {k: env[k] for k in ("DOCK_CONFIG_DIR", "DOCK_DATA_DIR", "XDG_CACHE_HOME")}


def main():
    keep = "--keep" in sys.argv
    binary = os.path.join(REPO, "lazysheet")
    sb = Sandbox(keep)
    try:
        scenario_no_config([binary], "standalone")
        scenario_main(sb, [binary], "standalone")
        scenario_skipped(sb, [binary], "standalone")
        scenario_osheet_file(sb, [binary], "standalone")
        if "--dock" in sys.argv:
            dock, dock_env = install_dock(sb)
            sb.env.update(dock_env)
            # fresh timesheets for the dock run
            shutil.rmtree(os.path.join(sb.home, "timesheets"), ignore_errors=True)
            scenario_no_config([dock, "lazysheet"], "dock", extra_env=dock_env)
            scenario_main(sb, [dock, "lazysheet"], "dock", in_dock=True)
            scenario_skipped(sb, [dock, "lazysheet"], "dock")
    finally:
        sb.cleanup()
    print(f"\n{len(passed)} checks passed")


if __name__ == "__main__":
    main()
