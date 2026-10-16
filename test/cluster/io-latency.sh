#!/bin/bash
# Measure how long a running job's output takes to reach the login node, as "you".
# Each line carries the time the job wrote it; the arrival time is read here.
set -u
# shellcheck source=lib.sh
. /opt/sdash/lib.sh

f=/home/you/runs/latency.out
log=$OUT/io-latency.txt
rm -f "$f" "$log"

id=$(sbatch --parsable -J latency -p cpu -t 2 -o "$f" --wrap 'for i in $(seq 1 12); do echo "$i $(date +%s.%N)"; sleep 1; done') || {
	result job-output-latency warn "sbatch failed"
	exit 0
}
id=${id%%;*}

for _ in $(seq 120); do
	[ -s "$f" ] && break
	sleep 0.5
done

last=0
end=$((SECONDS + 25))
while [ "$SECONDS" -lt "$end" ]; do
	n=$(wc -l <"$f" 2>/dev/null)
	n=$((n))
	if [ "$n" -ne "$last" ]; then
		echo "$n $(date +%s.%N)" >>"$log"
		last=$n
	fi
	[ "$n" -ge 12 ] && break
	sleep 0.1
done
scancel "$id" >/dev/null 2>&1

lag=$(awk 'NR == FNR { st[NR] = $2; next } { for (k = seen + 1; k <= $1; k++) { d = $2 - st[k]; if (d > m) m = d } seen = $1 } END { printf "%.1f", m }' "$f" "$log")
lines=$(wc -l <"$log")
if awk -v g="$lag" 'BEGIN { exit !(g > 1.5) }'; then
	result job-output-latency warn "$lines updates, a line reached the login node up to ${lag}s after it was written"
else
	result job-output-latency ok "$lines updates, a line reached the login node within ${lag}s"
fi
