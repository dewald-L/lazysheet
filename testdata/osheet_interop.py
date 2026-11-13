"""Bridge to osheet's own Excel code for lazysheet's interop tests.

Usage (run with python3 -I):
    osheet_interop.py <osheet> read <xlsx>                      entries as JSON
    osheet_interop.py <osheet> append <xlsx> <template> <json>  append entries
    osheet_interop.py <osheet> update <xlsx> <template> <row> [<json>]  replace or delete a row
    osheet_interop.py <osheet> duration <text>...               parse_duration results
    osheet_interop.py <osheet> paths <config.json> <date>...    output_path per date

Entries are JSON objects keyed by column header; "Date" is an ISO date.
"""
import datetime as dt
import importlib.machinery
import importlib.util
import json
import sys


def load(path):
    loader = importlib.machinery.SourceFileLoader("osheet", path)
    spec = importlib.util.spec_from_loader("osheet", loader)
    mod = importlib.util.module_from_spec(spec)
    loader.exec_module(mod)
    return mod


def to_entry(obj):
    e = dict(obj)
    e["Date"] = dt.date.fromisoformat(e["Date"])
    return e


def main(argv):
    o = load(argv[0])
    cmd, args = argv[1], argv[2:]
    if cmd == "read":
        out = []
        for e in o.read_entries(args[0]):
            e["Date"] = e["Date"].isoformat()
            out.append(e)
        print(json.dumps(out))
    elif cmd == "append":
        o.append_rows(args[0], args[1], [to_entry(e) for e in json.loads(args[2])])
    elif cmd == "update":
        entry = to_entry(json.loads(args[3])) if len(args) > 3 else None
        o.update_row(args[0], args[1], int(args[2]), entry)
    elif cmd == "duration":
        print(json.dumps([o.parse_duration(t) for t in args]))
    elif cmd == "paths":
        with open(args[0]) as f:
            cfg = json.load(f)
        print(json.dumps([o.output_path(cfg, dt.date.fromisoformat(d)) for d in args[1:]]))
    else:
        raise SystemExit(f"unknown command {cmd}")


if __name__ == "__main__":
    main(sys.argv[1:])
