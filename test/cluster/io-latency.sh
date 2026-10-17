#!/bin/bash
# shellcheck disable=SC2016 # the job scripts are expanded inside the jobs, not here
# Measure how fast the output of a running job reaches the login node.
#   io-latency.sh stamped   as "you": every line carries the time the job wrote it
#   io-latency.sh plain DIR as root: the loop the follow test runs, with the arrival
#                           time of each line, to show whether output comes in bursts
set -u
# shellcheck source=lib.sh
. /opt/sdash/lib.sh

mode=${1:-stamped}
if [ "$mode" = plain ]; then
	name=job-output-timeline
	f=${2:-/home/tmp}/probe.out
	script='for i in $(seq 1 120); do echo tick $i; sleep 1; done'
	want=14
else
	name=job-output-latency
	f=/home/you/runs/latency.out
	script='for i in $(seq 1 12); do echo "$i $(date +%s.%N)"; sleep 1; done'
	want=12
fi
log=$OUT/io-latency-$mode.txt
rm -f "$f" "$log"

id=$(sbatch --parsable -J latency -p cpu -t 2 -o "$f" --wrap "$script") || {
	result "$name" fail "sbatch failed"
	exit 0
}
id=${id%%;*}

for _ in $(seq 120); do
	[ -s "$f" ] && break
	sleep 0.5
done
if [ ! -s "$f" ]; then
	scancel "$id" >/dev/null 2>&1
	result "$name" fail "job $id wrote nothing in 60s"
	exit 0
fi

last=0
end=$((SECONDS + 30))
while [ "$SECONDS" -lt "$end" ]; do
	n=$(wc -l <"$f" 2>/dev/null)
	n=$((n))
	if [ "$n" -ne "$last" ]; then
		echo "$n $(date +%s.%N)" >>"$log"
		last=$n
	fi
	[ "$n" -ge "$want" ] && break
	sleep 0.1
done
scancel "$id" >/dev/null 2>&1
if [ "$last" -lt "$want" ]; then
	result "$name" fail "only $last of $want lines arrived in 30s"
	exit 0
fi

if [ "$mode" = plain ]; then
	gaps=$(awk 'NR == 1 { t0 = $2 } { for (k = seen + 1; k <= $1; k++) printf "%.1f ", $2 - t0; seen = $1 }' "$log")
	result "$name" ok "seconds after the first line each line arrived: $gaps"
	exit 0
fi

lag=$(awk 'NR == FNR { st[NR] = $2; next } { for (k = seen + 1; k <= $1; k++) { d = $2 - st[k]; if (d > m) m = d } seen = $1 } END { printf "%.1f", m }' "$f" "$log")
lines=$(wc -l <"$log")
if awk -v g="$lag" 'BEGIN { exit !(g > 1.5) }'; then
	result "$name" warn "$lines updates, a line reached the login node up to ${lag}s after it was written"
else
	result "$name" ok "$lines updates, a line reached the login node within ${lag}s"
fi
