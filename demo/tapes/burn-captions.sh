#!/bin/sh
# Usage: burn-captions.sh IN.mp4 OUT.mp4 CAPTIONS.txt
# Adds a caption band on top of the video; each caption line is
# start|end|font size|text (seconds; \n in the text breaks the line).
set -eu

in=$1
out=$2
captions=$3
band=220
font=/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Bold.ttf

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

filter="fps=30,pad=iw:ih+$band:0:$band:color=0x1e1e2e"
n=0
while IFS='|' read -r start end size text; do
	[ -n "$start" ] || continue
	n=$((n + 1))
	printf '%b\n' "$text" >"$tmp/$n.txt"
	filter="$filter,drawtext=fontfile=$font:textfile=$tmp/$n.txt:expansion=none:fontcolor=white:fontsize=$size:x=(w-text_w)/2:y=($band-text_h)/2:enable='between(t,$start,$end)'"
done <"$captions"

ffmpeg -v error -y -i "$in" -vf "$filter" -an -c:v libx264 -preset slow -crf 20 -pix_fmt yuv420p -movflags +faststart "$out"
