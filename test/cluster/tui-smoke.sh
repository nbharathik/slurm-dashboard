#!/bin/bash
# Drive the dashboard in tmux as the "you" user at three terminal setups and
# save every screen. Fails on a crash, a parse error or an empty table where
# the seeded cluster has rows.
set -u
# shellcheck source=lib.sh
. /opt/sdash/lib.sh

S=sdtest
D=$OUT/tui
mkdir -p "$D"
empty=$(mktemp)
ascii=$(mktemp)
printf 'ascii = true\n' >"$ascii"

snap() { tmux capture-pane -p -t "$S" >"$D/$1.txt" 2>/dev/null || true; }
key() { tmux send-keys -t "$S" "$1"; }

# label cols rows config
run_mode() {
	local label=$1 cols=$2 rows=$3 conf=$4 bad="" step want file
	tmux kill-session -t "$S" 2>/dev/null
	tmux new-session -d -s "$S" -x "$cols" -y "$rows" "SDASH_CONFIG=$conf sdash"
	sleep 8
	snap "$label-1-overview"
	for k in 2 3 4 5 6; do
		key "$k"
		sleep 2
		snap "$label-$k"
	done
	key 2
	sleep 1
	key Enter
	sleep 2
	snap "$label-detail"
	key Escape
	sleep 1
	key '?'
	sleep 1
	snap "$label-help"
	key Escape
	sleep 1
	key q
	sleep 2
	if tmux has-session -t "$S" 2>/dev/null; then
		bad="did not quit on q"
		tmux kill-session -t "$S" 2>/dev/null
	fi

	for step in 1-overview:gpu 2:sweep 3:alice 4:g1; do
		file=$D/$label-${step%%:*}.txt
		want=${step##*:}
		if ! grep -qi -- "$want" "$file" 2>/dev/null; then
			bad="${bad:+$bad; }${step%%:*} has no '$want'"
		fi
	done
	if grep -qiE 'panic|parse error|unparseable|goroutine [0-9]+' "$D/$label"-*.txt 2>/dev/null; then
		bad="${bad:+$bad; }crash or parse error on screen"
	fi
	if [ -n "$bad" ]; then
		result "tui-$label" fail "$bad"
	else
		result "tui-$label" ok ""
	fi
}

run_mode 160x45 160 45 "$empty"
run_mode 80x24 80 24 "$empty"
run_mode ascii80x24 80 24 "$ascii"
