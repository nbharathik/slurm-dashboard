#!/bin/bash
# Measure how fast the output of a running job reaches the login node, as "you".
# Warns when lines arrive in bursts, which slows any log viewer down.
set -u
# shellcheck source=lib.sh
. /opt/sdash/lib.sh

f=/home/you/runs/latency.out
log=$OUT/io-latency.txt
rm -f "$f" "$log"

id=$(sbatch --parsable -J latency -p cpu -t 2 -o "$f" --wrap 'for i in $(seq 1 12); do echo tick $i; sleep 1; done') || {
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
	sleep 0.2
done
scancel "$id" >/dev/null 2>&1

gap=$(awk 'NR > 1 { d = $2 - prev; if (d > m) m = d } { prev = $2 } END { printf "%.1f", m }' "$log")
lines=$(wc -l <"$log")
if awk -v g="$gap" 'BEGIN { exit !(g > 2.2) }'; then
	result job-output-latency warn "$lines updates, longest wait for a new line ${gap}s"
else
	result job-output-latency ok "$lines updates, longest wait ${gap}s"
fi
