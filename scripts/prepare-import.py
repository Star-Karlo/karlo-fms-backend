#!/usr/bin/env python3
"""Turn a Mongo-shaped truck/driver export into files the importers accept.

The exports differ from what cmd/import and cmd/import-drivers expect in three
ways, and two of them fail SILENTLY — the import reports "done" and writes
nothing, which is far worse than an error:

  1. TRUCKS HAVE NO `_id`. It is the dedupe key (stored as external_ref), and
     cmd/import skips any row without one. A 200-row file imports zero vehicles
     and only says "skipped=212" in passing. The plate is used instead: unique
     per truck, stable across re-exports, so re-running updates rather than
     duplicating.

  2. DRIVERS SPELL THE NAME COLUMN `driverName`. The importer wants the export's
     own historical typo, `diverName`. This one at least fails loudly.

  3. Mongo extended JSON wrappers ({"$date": ...} / {"$oid": ...}) are not
     valid RFC3339 / ObjectId. Unwrapped here when present. A wrapped date is
     dropped silently, leaving a document with no expiry that never appears in
     licence reminders.

Nothing is invented: no row is dropped, no value is rewritten beyond unwrapping
and the two renames. Rows that cannot import cleanly are REPORTED, not fixed,
because guessing a plate or an IMEI would put a real truck under the wrong
identity.

    python3 scripts/prepare-import.py --trucks truck-brt.csv --drivers driver-brt.csv --out-dir .
"""
import argparse
import csv
import re
import sys
from collections import Counter

IMEI_RE = re.compile(r"\d{15}")
OID_RE = re.compile(r"[0-9a-f]{24}")


def unwrap(v):
    """Flatten a Mongo extended-JSON scalar; pass anything else through."""
    if isinstance(v, str) and v.startswith("{") and ("$date" in v or "$oid" in v):
        m = re.search(r'"\$(?:date|oid)"\s*:\s*"([^"]+)"', v)
        return m.group(1) if m else v
    return v


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--trucks", required=True)
    ap.add_argument("--drivers")
    ap.add_argument("--out-dir", default=".")
    args = ap.parse_args()

    # ---- trucks ----
    with open(args.trucks, newline="", encoding="utf-8-sig") as f:
        trucks = [{k: unwrap(v) for k, v in r.items()} for r in csv.DictReader(f)]
    if not trucks:
        print("no truck rows", file=sys.stderr)
        return 1

    plates = [(r.get("policeNumber") or "").strip() for r in trucks]
    dupes = [p for p, n in Counter(p for p in plates if p).items() if n > 1]
    blanks = [i for i, p in enumerate(plates, 2) if not p]
    if dupes or blanks:
        # Refuse rather than guess: a duplicate plate as external_ref would make
        # two trucks collapse into one on import.
        print(f"ERROR: plate is used as the dedupe key but is not unique.", file=sys.stderr)
        if dupes:
            print(f"  duplicated: {dupes[:10]}", file=sys.stderr)
        if blanks:
            print(f"  blank on rows: {blanks[:10]}", file=sys.stderr)
        return 1

    for r, p in zip(trucks, plates):
        r.setdefault("_id", "")
        if not r["_id"]:
            r["_id"] = p

    tf = f"{args.out_dir.rstrip('/')}/import-trucks.csv"
    cols = ["_id"] + [c for c in trucks[0] if c != "_id"]
    with open(tf, "w", newline="", encoding="utf-8") as f:
        w = csv.DictWriter(f, fieldnames=cols)
        w.writeheader()
        w.writerows(trucks)

    # Soft-deleted rows in the Mongo export carry "deletedTruck" in the plate.
    # cmd/import drops them, which is right — but it reports only a skip COUNT,
    # so without this the operator sees "212 rows, 162 imported" and reasonably
    # wonders which 50 went missing.
    def soft_deleted(row, plate):
        return "deletedTruck" in plate or "deletedTruck" in (row.get("truckNumber") or "")

    deleted = [r for r, p in zip(trucks, plates) if soft_deleted(r, p)]
    live = [r for r, p in zip(trucks, plates) if not soft_deleted(r, p)]
    tracked = {(r.get("imeiTracker") or "").strip() for r in live
               if IMEI_RE.fullmatch((r.get("imeiTracker") or "").strip())}
    print(f"trucks  -> {tf}")
    print(f"  {len(trucks)} rows, _id filled from the plate")
    if deleted:
        print(f"  {len(deleted)} are soft-deleted in the export and will be skipped "
              f"(expected — pass -include-deleted to import them anyway)")
    print(f"  {len(live)} real trucks will import")
    print(f"  {len(tracked)} will link a tracker; {len(live) - len(tracked)} have no usable IMEI "
          f"(imported, but they will never move on the map)")

    # ---- drivers ----
    if not args.drivers:
        return 0
    with open(args.drivers, newline="", encoding="utf-8-sig") as f:
        drivers = [{k: unwrap(v) for k, v in r.items()} for r in csv.DictReader(f)]
    if not drivers:
        print("no driver rows", file=sys.stderr)
        return 1

    for r in drivers:
        if "diverName" not in r:  # the importer matches the export's own typo
            r["diverName"] = r.get("driverName", "")

    bad_oid = [(i, r.get("driverId", "")) for i, r in enumerate(drivers, 2)
               if not OID_RE.fullmatch((r.get("driverId") or "").strip())]
    if bad_oid:
        print(f"ERROR: {len(bad_oid)} rows have a driverId that is not a 24-hex ObjectId; "
              f"the importer skips these. First: {bad_oid[:5]}", file=sys.stderr)
        return 1

    df = f"{args.out_dir.rstrip('/')}/import-drivers.csv"
    dcols = ["imeiTracker", "driverId", "diverName", "DriverPhone"]
    with open(df, "w", newline="", encoding="utf-8") as f:
        w = csv.DictWriter(f, fieldnames=dcols, extrasaction="ignore")
        w.writeheader()
        w.writerows(drivers)

    attach = [r for r in drivers if (r.get("imeiTracker") or "").strip() in tracked]
    orphan = [r for r in drivers if r not in attach]
    undialable = [r for r in drivers
                  if len(re.sub(r"\D", "", r.get("DriverPhone") or "")) < 10]
    print(f"drivers -> {df}")
    print(f"  {len(drivers)} rows, name column renamed to diverName")
    print(f"  {len(attach)} will attach to a truck")
    if orphan:
        # Named, not silently dropped: each is a real person who will not appear.
        print(f"  {len(orphan)} will be SKIPPED — their IMEI matches no tracked truck:")
        for r in orphan[:10]:
            print(f"      {r.get('diverName')} ({r.get('DriverPhone')}) imei={r.get('imeiTracker')!r}")
    if undialable:
        print(f"  {len(undialable)} have a phone too short to call — imported, "
              f"but the call button will be hidden:")
        for r in undialable[:10]:
            print(f"      {r.get('diverName')} ({r.get('DriverPhone')})")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
