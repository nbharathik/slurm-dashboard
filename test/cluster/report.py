#!/usr/bin/env python3
"""Merge the per-version compat results into one grid (check x Slurm version).

Usage: report.py [DIR]   (default build/compat)

Reads DIR/<version>/results.tsv, prints Markdown, writes DIR/report.md and
appends to $GITHUB_STEP_SUMMARY when set. Exits 1 when any check failed.
"""
import os
import sys
from pathlib import Path

ICON = {"ok": "✅", "warn": "⚠️", "fail": "❌"}


def version_key(name):
    try:
        return tuple(int(p) for p in name.split("."))
    except ValueError:
        return (10**6, name)


def load(root):
    versions = {}
    for d in root.iterdir():
        f = d / "results.tsv"
        if not d.is_dir() or not f.exists():
            continue
        rows = []
        for line in f.read_text(encoding="utf-8", errors="replace").splitlines():
            parts = line.split("\t", 2)
            if len(parts) >= 2:
                rows.append((parts[0], parts[1], parts[2] if len(parts) > 2 else ""))
        versions[d.name.removeprefix("compat-")] = rows
    return versions


def render(versions):
    names = sorted(versions, key=version_key)
    checks = []
    for v in names:
        for name, _, _ in versions[v]:
            if name not in checks:
                checks.append(name)
    lines = ["# Slurm compatibility", ""]
    lines.append("| check | " + " | ".join(names) + " |")
    lines.append("| --- | " + " | ".join("---" for _ in names) + " |")
    for c in checks:
        cells = []
        for v in names:
            status = next((s for n, s, _ in versions[v] if n == c), "")
            cells.append(ICON.get(status, "-"))
        lines.append(f"| {c} | " + " | ".join(cells) + " |")
    problems = []
    for v in names:
        for name, status, detail in versions[v]:
            if status != "ok":
                problems.append(f"- {ICON.get(status, status)} **{v}** `{name}`: {detail}")
    if problems:
        lines += ["", "## Problems", ""] + problems
    return "\n".join(lines) + "\n", any(s == "fail" for v in versions.values() for _, s, _ in v)


def main():
    root = Path(sys.argv[1] if len(sys.argv) > 1 else "build/compat")
    versions = load(root)
    if not versions:
        print(f"no results.tsv found under {root}", file=sys.stderr)
        return 1
    text, failed = render(versions)
    print(text)
    (root / "report.md").write_text(text, encoding="utf-8")
    summary = os.environ.get("GITHUB_STEP_SUMMARY")
    if summary:
        with open(summary, "a", encoding="utf-8") as f:
            f.write(text)
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
