#!/usr/bin/env bash
# Run sdash against a throwaway Slurm cluster in Docker.
#   test/cluster/compat.sh 24.11
# Needs Docker with Compose, and Go unless build/ already holds sdash,
# integ.test and ui-integ.test (SDASH_PREBUILT=1). Results go to
# build/compat/<version>/: results.tsv, raw output, captures and fixtures.
set -uo pipefail

ver=${1:?usage: compat.sh <slurm-version>}
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
base=$(awk -v v="$ver" '$1 == v { print $2 }' "$here/matrix.txt")
[ -n "$base" ] || { echo "unknown Slurm version $ver (see test/cluster/matrix.txt)" >&2; exit 2; }

export SLURM=$ver BASE=$base SDASH_ROOT=$root
build=$root/build
out=$build/compat/$ver
proj=sdash-${ver//./-}

dc() { docker compose -p "$proj" -f "$here/compose.yaml" "$@"; }
as_root() { dc exec -T -e "OUT=/out/$ver" login "$@"; }
as_you() { dc exec -T -u you -e HOME=/home/you -e USER=you -e LOGNAME=you -e "OUT=/out/$ver" login "$@"; }

result() {
	local detail=${3:-}
	detail=$(printf '%s' "$detail" | tr '\t\n' '  ')
	printf '%s\t%s\t%s\n' "$1" "$2" "${detail:0:300}" >>"$out/results.tsv"
}

collect() {
	dc logs --no-color >"$out/containers.log" 2>&1
	as_root sh -c 'sinfo; echo; squeue; echo; scontrol show node -o; echo; scontrol show partition -o; echo; sacctmgr -n show assoc' >"$out/cluster-state.txt" 2>&1
	dc exec -T ctld tail -n 300 /var/log/slurm/slurmctld.log >"$out/slurmctld.log" 2>&1
	dc exec -T ctld tail -n 300 /var/log/slurm/slurmdbd.log >"$out/slurmdbd.log" 2>&1
	dc exec -T g1 tail -n 200 /var/log/slurm/slurmd.log >"$out/slurmd-g1.log" 2>&1
	as_root chmod -R a+rwX "/out/$ver" >/dev/null 2>&1
}

finish() {
	collect
	[ "${KEEP:-}" = 1 ] || dc down -v --remove-orphans >/dev/null 2>&1
	echo "== Slurm $ver ($base)"
	column -t -s $'\t' "$out/results.tsv" 2>/dev/null || cat "$out/results.tsv"
	if awk -F'\t' '$2 == "fail" { f = 1 } END { exit !f }' "$out/results.tsv"; then
		exit 1
	fi
	exit 0
}

build_binaries() {
	(
		cd "$root" || exit 1
		export CGO_ENABLED=0 GOOS=linux
		go build -trimpath -o build/sdash ./cmd/sdash &&
			go test -c -tags integration -o build/integ.test ./internal/integration &&
			go test -c -tags integration -o build/ui-integ.test ./internal/ui
	) || { result build fail "go build failed"; return 1; }
}

# wait_nodes waits until c1, c2 and g1 have registered and are usable.
wait_nodes() {
	local deadline=$((SECONDS + 150)) n
	while [ "$SECONDS" -lt "$deadline" ]; do
		n=$(as_root sinfo -h -N -o '%N %t' 2>/dev/null | awk '($1 == "c1" || $1 == "c2" || $1 == "g1") && $2 ~ /^(idle|mix|alloc)$/' | wc -l)
		[ "$n" -ge 3 ] && return 0
		sleep 3
	done
	return 1
}

# wait_states waits until the seeded jobs have reached the states the checks need.
wait_states() {
	local deadline=$((SECONDS + 240)) missing hist queue s
	while [ "$SECONDS" -lt "$deadline" ]; do
		missing=""
		hist=$(as_you sacct -X -n -P -u you -S now-2hours -o State 2>/dev/null)
		queue=$(as_you squeue --me -h -o '%T' 2>/dev/null)
		for s in COMPLETED FAILED TIMEOUT CANCELLED; do
			grep -q "^$s" <<<"$hist" || missing="$missing $s"
		done
		for s in RUNNING PENDING; do
			grep -q "^$s" <<<"$queue" || missing="$missing $s"
		done
		[ -z "$missing" ] && return 0
		sleep 5
	done
	result seed-states warn "missing after 240s:$missing"
}

# integ NAME DIR BINARY [PATTERN] runs a Go test binary as root in the login container.
integ() {
	local name=$1 dir=$2 bin=$3 pattern=${4:-} log pass failed skip rc
	local -a args=(-test.v -test.count=1 -test.timeout=10m)
	[ -n "$pattern" ] && args+=("-test.run=$pattern")
	log=$out/$name.log
	dc exec -T -e SDASH_INTEGRATION=local -e TMPDIR=/home/tmp -w "$dir" login "$bin" "${args[@]}" >"$log" 2>&1
	rc=$?
	pass=$(grep -c '^--- PASS' "$log")
	failed=$(grep -c '^--- FAIL' "$log")
	skip=$(grep -c '^--- SKIP' "$log")
	if [ "$rc" -ne 0 ] || [ "$failed" -gt 0 ]; then
		result "$name" fail "$pass passed, $failed failed: $(grep -m1 -e '--- FAIL' "$log")"
		echo "== $name failures" >&2
		grep -B60 -A2 -e '--- FAIL' "$log" | head -n 250 >&2
	elif [ "$pass" -eq 0 ]; then
		result "$name" fail "no test ran ($skip skipped)"
	else
		result "$name" ok "$pass passed, $skip skipped"
	fi
}

rm -rf "$out"
mkdir -p "$out"
chmod 777 "$out"
: >"$out/results.tsv"
chmod 666 "$out/results.tsv"
trap 'trap - INT TERM; result aborted fail "interrupted"; finish' INT TERM

# CI builds once and sets SDASH_PREBUILT=1; elsewhere rebuild so the binaries match the code.
if [ "${SDASH_PREBUILT:-}" != 1 ] && command -v go >/dev/null; then
	build_binaries || finish
fi
for f in sdash integ.test ui-integ.test; do
	[ -f "$build/$f" ] && chmod +x "$build/$f"
done
if [ ! -x "$build/sdash" ] || [ ! -x "$build/integ.test" ] || [ ! -x "$build/ui-integ.test" ]; then
	result build fail "build/ lacks sdash, integ.test or ui-integ.test and Go is not installed"
	finish
fi

dc down -v --remove-orphans >/dev/null 2>&1
if ! dc build login >"$out/docker-build.log" 2>&1; then
	result cluster-build fail "$(tail -n 3 "$out/docker-build.log")"
	finish
fi
if ! dc up -d >"$out/docker-up.log" 2>&1; then
	result cluster-up fail "$(tail -n 3 "$out/docker-up.log")"
	finish
fi

sv=$(as_root sinfo --version 2>&1)
pkg=$(as_root cat /etc/sdash-slurm-package 2>&1)
if [[ $sv == *"$ver"* ]]; then
	result slurm-version ok "$sv, package $pkg"
else
	result slurm-version fail "$base installs $sv (package $pkg), expected $ver"
fi

if ! wait_nodes; then
	result nodes-up fail "c1, c2 or g1 never became usable: $(as_root sinfo -h -N -o '%N %t' 2>&1)"
	dc ps -a >&2
	dc logs --no-color --tail 80 c1 g1 >&2
	finish
fi
result nodes-up ok ""

if as_root /opt/sdash/seed.sh >"$out/seed.log" 2>&1 && ! grep -q 'sbatch as .* failed' "$out/seed.log"; then
	result seed ok ""
else
	result seed fail "$(grep -m1 -e 'failed' -e 'rror' "$out/seed.log")"
fi
wait_states
result nodes-cgroup ok "slurmd cgroup mode: $(dc exec -T g1 cat /var/log/slurm/cgroup-mode 2>&1)"

as_you /opt/sdash/cli-checks.sh >"$out/cli-checks.log" 2>&1 || result cli-checks fail "script exited with an error"
as_you /opt/sdash/tui-smoke.sh >"$out/tui-smoke.log" 2>&1 || result tui-smoke fail "script exited with an error"
as_you /opt/sdash/io-latency.sh stamped >"$out/io-latency.log" 2>&1 || result job-output-latency fail "script exited with an error"
as_root /opt/sdash/io-latency.sh plain /home/tmp >"$out/io-timeline.log" 2>&1 || result job-output-timeline fail "script exited with an error"

if as_you sdash record --out "/out/$ver/fixtures" >"$out/record.log" 2>&1; then
	if grep -q '^!' "$out/record.log"; then
		result record warn "$(grep -m1 '^!' "$out/record.log")"
	else
		result record ok ""
	fi
else
	result record fail "$(head -n 1 "$out/record.log")"
fi

integ integration /src/internal/integration /opt/build/integ.test
integ ui-integration /src/internal/ui /opt/build/ui-integ.test 'RealJobs|FollowedLog|RealCluster'

finish
