#!/bin/bash
# CLI checks as the "you" user, run in the login container by compat.sh.
# Each command must exit 0, print valid JSON (with --json), contain a known
# seeded value, not panic, and leave no WARN or ERROR lines in the debug log.
set -u
# shellcheck source=lib.sh
. /opt/sdash/lib.sh

CLI=$OUT/cli
LOG=$HOME/.cache/sdash/debug.log
me=$(id -un)
mkdir -p "$CLI"

# failed_checks lists the names of failing checks in a doctor JSON file.
failed_checks() {
	python3 -c 'import json,sys; print(",".join(c["name"] for c in json.load(open(sys.argv[1]))["checks"] if c["status"] == "fail"))' "$1" 2>/dev/null
}

check() {
	local name=$1 want=$2 out err rc msg warns failed
	shift 2
	out=$CLI/$name.out
	err=$CLI/$name.err
	rm -f "$LOG"
	timeout 120 sdash "$@" >"$out" 2>"$err"
	rc=$?
	if [ "$rc" -ne 0 ]; then
		failed=""
		[ "$name" = doctor ] && failed=$(failed_checks "$out")
		result "$name" fail "exit $rc: ${failed:-$(first_line "$err")}"
		return
	fi
	if grep -qiE 'panic|goroutine [0-9]+' "$out" "$err"; then
		result "$name" fail "panic in output"
		return
	fi
	case " $* " in
	*" --json "*)
		if ! python3 -m json.tool "$out" >/dev/null 2>&1; then
			result "$name" fail "invalid JSON"
			return
		fi
		;;
	esac
	if [ "$want" != "-" ] && ! grep -qi -- "$want" "$out"; then
		result "$name" fail "output has no '$want'"
		return
	fi
	warns=$(grep -E 'level=(WARN|ERROR)' "$LOG" 2>/dev/null | head -n 1)
	if [ -n "$warns" ]; then
		msg=$(grep -cE 'level=(WARN|ERROR)' "$LOG")
		result "$name" warn "$msg log warning(s): $warns"
		return
	fi
	result "$name" ok ""
}

id_of() { squeue --me -h -n "$1" -o '%i' | head -n 1; }

check version - version
check doctor - doctor --json
sdash doctor >"$CLI/doctor.txt" 2>&1 || true
check status sdashtest status --json
check jobs my-train jobs --json
check jobs-array sweep jobs --json
check queue alice queue --json
check queue-pending big-gpu queue --json
check nodes g1 nodes --json
check nodes-gpu h100 nodes --gpu --json
check nodes-partitions gpu nodes --partitions --json
check node-g1 h100 nodes g1
check usage - usage --json
check history wasteful history --json
check free g1 free --gpus 1 --json
check quota - quota --json

big=$(id_of big-gpu)
dep=$(id_of after-fail)
held=$(id_of on-hold)
run=$(id_of my-train)
wasteful=$(sacct -X -n -P -u "$me" -S now-2hours -o JobID,JobName | awk -F'|' '$2 == "wasteful" { print $1; exit }')

[ -n "$big" ] && check why-resources - why "$big" --json
[ -n "$big" ] && check why-text - why "$big"
[ -n "$dep" ] && check why-dependency - why "$dep" --json
[ -n "$held" ] && check why-held - why "$held" --json
[ -n "$run" ] && check logs epoch logs "$run"
[ -n "$wasteful" ] && check history-job - history "$wasteful" --json

for v in big dep held run wasteful; do
	if [ -z "${!v}" ]; then
		result "seed-$v" warn "seeded job not found"
	fi
done
