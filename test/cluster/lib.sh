#!/bin/bash
# Helpers shared by the scripts that run inside the login container.
# Each result is one line: name, status (ok|warn|fail), detail.

OUT=${OUT:-/out}

result() {
	local detail=${3:-}
	detail=${detail//$'\t'/ }
	detail=${detail//$'\n'/ }
	printf '%s\t%s\t%s\n' "$1" "$2" "${detail:0:300}" >>"$OUT/results.tsv"
}

first_line() {
	grep -m1 . "$1" 2>/dev/null || true
}
