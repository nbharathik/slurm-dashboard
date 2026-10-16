#!/bin/bash
# shellcheck disable=SC2016 # the --wrap scripts are expanded inside the jobs, not here
# Fill the test cluster with accounts, a reservation and jobs in many states.
# Run as root in the login container. Only for the throwaway cluster.
set -u
cd /tmp || exit 1

as() { local u=$1; shift; runuser -u "$u" -- env HOME="/home/$u" USER="$u" LOGNAME="$u" "$@"; }
sub() {
	local u=$1 id
	shift
	id=$(as "$u" sbatch --parsable -D "/home/$u" -o "/home/$u/runs/%x-%j.out" -e "/home/$u/runs/%x-%j.err" "$@") || {
		echo "sbatch as $u failed: $*" >&2
		return 1
	}
	echo "${id%%;*}"
}
sa() { sacctmgr -i "$@" >/dev/null 2>&1 || true; }

for u in you alice bob carol; do
	install -d -o "$u" -g "$u" "/home/$u" "/home/$u/runs"
done

# --- accounting -----------------------------------------------------------
sa add account ml-lab Description=ml-lab Organization=lab
sa add account nlp Description=nlp Organization=lab
sa add user you,alice,bob,root Account=ml-lab
sa add user carol Account=nlp
sa add qos limited
sa modify qos where name=limited set MaxTRESPerUser=cpu=8,gres/gpu=2
sa modify user where name=you set qos=normal,limited defaultqos=normal
sa modify account where name=ml-lab set GrpTRES=cpu=64,gres/gpu=8

scontrol create reservation ReservationName=maint StartTime=now+2days Duration=06:00:00 \
	Nodes=ALL Users=root Flags=MAINT,IGNORE_JOBS >/dev/null 2>&1 || true
scontrol update NodeName=down1 State=DRAIN Reason="bad DIMM" >/dev/null 2>&1 || true

echo "waiting for the nodes"
for _ in $(seq 180); do
	n=$(sinfo -h -N -o '%N %t' | awk '($1 == "c1" || $1 == "c2" || $1 == "g1") && $2 ~ /^(idle|mix|alloc)$/' | wc -l)
	[ "$n" -ge 3 ] && break
	sleep 1
done
sinfo

scancel -u you,alice,bob,carol 2>/dev/null || true
sleep 2

cat >/home/you/runs/oom.sh <<'EOF'
#!/bin/bash
python3 - <<'PY'
import time
data = b"x" * (300 * 1024 * 1024)
time.sleep(30)
PY
EOF
chown you:you /home/you/runs/oom.sh
chmod 0755 /home/you/runs/oom.sh

# --- finished jobs for "you" (history, efficiency, failure hints) -----------
sub you -J wasteful -p cpu -c 8 -t 5 --wrap 'sleep 60'
sub you -J efficient -p cpu -c 2 -t 5 --wrap 'stress-ng --cpu 2 --timeout 45s'
sub you -J fails -p cpu -t 5 --wrap 'echo "Traceback (most recent call last):" >&2; echo "ValueError: bad input" >&2; exit 3'
FAIL=$(sub you -J will-fail -p cpu -t 5 --wrap 'sleep 3; exit 1')
sub you -J after-fail -p cpu -t 5 --dependency=afterok:"$FAIL" --wrap 'echo never'
sub you -J times-out -p cpu -t 1 --wrap 'sleep 600'
CANCEL=$(sub you -J cancel-me -p cpu -t 5 --wrap 'sleep 600')
sub you -J oom -p cpu -t 5 --mem=100M /home/you/runs/oom.sh

# --- running jobs -------------------------------------------------------------
train='for i in $(seq 100000); do echo "epoch $i loss 0.$((RANDOM % 1000))"; sleep 2; done'
sub you -J my-train -p gpu --gres=gpu:1 -c 2 -t 04:00:00 --qos=limited --wrap "$train"
sub alice -J train-lora -p gpu --gres=gpu:1 -c 2 -t 04:00:00 --wrap "$train"
sub bob -J bob-train -p gpu --gres=gpu:1 -c 2 -t 04:00:00 --wrap 'sleep 14400'
sub alice -J data-prep -p cpu -c 2 -t 04:00:00 --wrap 'sleep 14400'
sub carol -J carol-mpi -p cpu -n 4 -t 05:00:00 --wrap 'sleep 18000'
sub you -J sweep --array=1-8%2 -p cpu -c 1 -t 01:00:00 --wrap 'echo task $SLURM_ARRAY_TASK_ID; sleep 3600'

# --- pending jobs, one per interesting reason ----------------------------------
sub you -J big-gpu -p gpu --gres=gpu:2 -t 01:00:00 --wrap 'sleep 60'
sub carol -J carol-gpu -p gpu --gres=gpu:1 -t 10:00:00 --wrap 'sleep 60'
sub you -J too-long -p debug -t 02:00:00 --wrap 'sleep 60'
sub you -J crosses-maint -p cpu -t 3-00:00:00 --wrap 'sleep 60'
sub you -J on-hold --hold -t 10 --wrap 'sleep 60'
sub you -J later --begin=now+3hours -t 10 --wrap 'sleep 60'

sleep 3
as you scancel "$CANCEL" || true
echo "seeded"
squeue
