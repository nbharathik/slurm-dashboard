"""Enforce the installed application budget before publishing any artifacts."""

import argparse
from pathlib import Path

LIMIT = 20 * 1024 * 1024


def check(binary, completion=None):
    size = binary.stat().st_size
    extra = completion.stat().st_size if completion else 0
    print(f"{binary}: {size} bytes ({size / 1048576:.2f} MiB); "
          f"with bash completion: {size + extra} bytes", flush=True)
    if not 0 < size < LIMIT or size + extra >= LIMIT:
        raise SystemExit("Installed application must be smaller than 20 MiB")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binaries", nargs="+", type=Path)
    parser.add_argument("--completion", type=Path)
    args = parser.parse_args()
    for binary in args.binaries:
        check(binary, args.completion)
