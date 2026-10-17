"""Print a tag's section of CHANGELOG.md as release notes (v0.2.0-rc.1 uses 0.2.0)."""

import argparse
import re
from pathlib import Path


def unwrap(lines):
    """Join wrapped lines: GitHub release pages keep every line break."""
    out, fence = [], False
    for line in lines:
        s = line.strip()
        if s.startswith("```"):
            fence = not fence
        prev = out[-1].strip() if out else ""
        if (not fence and s and prev and not prev.startswith(("#", "```"))
                and not s.startswith(("- ", "#", "|", "```"))):
            out[-1] += " " + s
        else:
            out.append(line.rstrip())
    return out


def notes(changelog, tag):
    version = re.sub(r"^v", "", tag).split("-")[0]
    heading = f"## [{version}]"
    lines = changelog.read_text(encoding="utf-8").splitlines()
    start = next((i for i, l in enumerate(lines) if l.startswith(heading)), None)
    if start is None:
        raise SystemExit(f"{changelog} has no section {heading}")
    body = []
    for line in lines[start + 1:]:
        if line.startswith("## [") or re.match(r"^\[[^\]]+\]: ", line):
            break
        body.append(line)
    text = "\n".join(unwrap(body)).strip()
    if not text:
        raise SystemExit(f"{changelog}: section {heading} is empty")
    if "-" not in tag and "unreleased" in lines[start].lower():
        print(f"::warning::{changelog}: {heading} has no release date", flush=True)
    return text + "\n"


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("changelog", type=Path)
    parser.add_argument("tag")
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    args.out.write_text(notes(args.changelog, args.tag), encoding="utf-8")
