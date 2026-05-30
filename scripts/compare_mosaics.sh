#!/usr/bin/env bash
#
# compare_mosaics.sh — visual + quantitative diff of two mosaic videos.
#
# Extracts comparable frames and edge crops from two mosaic videos and
# measures their real (non-black) content box, so you can see exactly
# where one smears / drifts and the other doesn't.
#
# No special tooling beyond ffmpeg/ffprobe is required — there is no need
# for an MCP server to "watch" the video; we turn the video into still
# images and inspect those.
#
# Usage:
#   scripts/compare_mosaics.sh OURS.mp4 REFERENCE.mp4 [OUT_DIR]
#
# Output (PNGs written to OUT_DIR, default /tmp/mosaic_cmp):
#   <tag>_first.png  <tag>_mid.png  <tag>_last.png   full frames
#   <tag>_left.png   <tag>_right.png                 full-res edge crops
# plus a printed table of dimensions and detected content boxes.

set -euo pipefail

ours="${1:?usage: compare_mosaics.sh OURS.mp4 REFERENCE.mp4 [OUT_DIR]}"
ref="${2:?usage: compare_mosaics.sh OURS.mp4 REFERENCE.mp4 [OUT_DIR]}"
out="${3:-/tmp/mosaic_cmp}"
mkdir -p "$out"

probe() { # path -> "WxH frames@fps"
  ffprobe -v error -select_streams v:0 \
    -show_entries stream=width,height,nb_frames,r_frame_rate \
    -of csv=p=0 "$1"
}

# cropdetect reports the smallest rectangle that bounds non-black pixels.
# A content box much smaller than the full frame means uniform black
# margins; a content box ~= the full frame while the picture still looks
# black-heavy means the black is *interior* (e.g. diagonal Y-drift wedges).
content_box() { # path -> crop=W:H:X:Y
  ffmpeg -y -loglevel info -ss 1 -i "$1" \
    -vf "cropdetect=limit=10:round=2" -frames:v 5 -f null - 2>&1 |
    grep -o "crop=[0-9:]*" | tail -1
}

dump() { # path tag
  local path="$1" tag="$2"
  local n
  n=$(ffprobe -v error -select_streams v:0 -count_frames \
        -show_entries stream=nb_read_frames -of csv=p=0 "$path")
  local mid=$(( n / 2 )) last=$(( n > 0 ? n - 1 : 0 ))
  local w
  w=$(ffprobe -v error -select_streams v:0 -show_entries stream=width -of csv=p=0 "$path")
  local edge=$(( w / 8 )); [ "$edge" -lt 1 ] && edge=1

  ffmpeg -y -loglevel error -i "$path" -vf "select=eq(n\,0)"        -frames:v 1 "$out/${tag}_first.png"
  ffmpeg -y -loglevel error -i "$path" -vf "select=eq(n\,$mid)"     -frames:v 1 "$out/${tag}_mid.png"
  ffmpeg -y -loglevel error -i "$path" -vf "select=eq(n\,$last)"    -frames:v 1 "$out/${tag}_last.png"
  ffmpeg -y -loglevel error -i "$path" -vf "select=eq(n\,$mid),crop=${edge}:ih:0:0"          -frames:v 1 "$out/${tag}_left.png"
  ffmpeg -y -loglevel error -i "$path" -vf "select=eq(n\,$mid),crop=${edge}:ih:$((w-edge)):0" -frames:v 1 "$out/${tag}_right.png"
}

printf '%-10s %-22s %-22s\n' "" "OURS" "REFERENCE"
printf '%-10s %-22s %-22s\n' "dims"    "$(probe "$ours")"      "$(probe "$ref")"
printf '%-10s %-22s %-22s\n' "content" "$(content_box "$ours")" "$(content_box "$ref")"

dump "$ours" ours
dump "$ref"  ref
echo "frames written to: $out"
