#!/usr/bin/env bash
set -euo pipefail

SCRIPT_NAME="$(basename "$0")"

usage() {
  cat <<EOF2
Usage:
  ${SCRIPT_NAME} <input.avi> [output.avi]

Description:
  Repair broken AVI audio streams by:
    1) extracting raw audio payload from AVI
    2) rebuilding a clean Ogg logical stream from valid page serials
    3) remuxing original video + repaired audio into AVI (MP3 audio)
    4) running an audio decode smoke test

Notes:
  - Intended for AVI files with malformed Vorbis-in-AVI style audio.
  - Video is copied without re-encoding.

Requirements:
  ffmpeg, ffprobe, python3
EOF2
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

if [[ $# -lt 1 || $# -gt 2 ]]; then
  usage >&2
  exit 1
fi

INPUT="$1"
if [[ ! -f "$INPUT" ]]; then
  echo "Input file not found: $INPUT" >&2
  exit 1
fi

if [[ $# -eq 2 ]]; then
  OUTPUT="$2"
else
  DIRNAME="$(cd "$(dirname "$INPUT")" && pwd)"
  BASENAME="$(basename "$INPUT")"
  STEM="${BASENAME%.*}"
  OUTPUT="${DIRNAME}/${STEM}_audio_fixed.avi"
fi

for cmd in ffmpeg ffprobe python3; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "Required command not found: $cmd" >&2
    exit 1
  fi
done

TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/avi-audio-repair.XXXXXX")"
cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

AUDIO_BIN="$TMP_DIR/audio_stream.bin"
CLEAN_OGG="$TMP_DIR/audio_stream_clean.ogg"

echo "[1/5] Probing input streams..."
ffprobe -hide_banner -v error \
  -show_entries stream=index,codec_type,codec_name,codec_tag_string,sample_rate,channels,duration \
  -of default=noprint_wrappers=1 "$INPUT" || true

echo "[2/5] Extracting raw audio payload..."
ffmpeg -hide_banner -v error -i "$INPUT" -map 0:a:0 -c copy -f data "$AUDIO_BIN"

echo "[3/5] Rebuilding clean Ogg stream from valid page serial..."
python3 - "$AUDIO_BIN" "$CLEAN_OGG" <<'PY'
from pathlib import Path
import sys

src = Path(sys.argv[1])
dst = Path(sys.argv[2])
data = src.read_bytes()

pages = []
counts = {}
pos = 0

while True:
    i = data.find(b"OggS", pos)
    if i < 0:
        break
    if i + 27 > len(data):
        break
    if data[i + 4] != 0:
        pos = i + 1
        continue

    seg_count = data[i + 26]
    hdr_end = i + 27 + seg_count
    if hdr_end > len(data):
        pos = i + 1
        continue

    body_len = sum(data[i + 27:hdr_end])
    end = hdr_end + body_len
    if end > len(data):
        pos = i + 1
        continue

    serial = int.from_bytes(data[i + 14:i + 18], "little")
    flags = data[i + 5]
    pages.append((i, end, serial, flags))
    counts[serial] = counts.get(serial, 0) + 1
    pos = end

if not pages:
    raise SystemExit("No Ogg pages found in extracted audio payload.")

non_junk_serials = [s for s in counts if s != 0xFFFFFFFF]
if not non_junk_serials:
    raise SystemExit("Only invalid serial (0xffffffff) pages found; cannot recover.")

bos_serial = None
for _, _, serial, flags in pages:
    if (flags & 0x02) and serial != 0xFFFFFFFF:
        bos_serial = serial
        break

if bos_serial is not None:
    target_serial = bos_serial
else:
    target_serial = max(non_junk_serials, key=lambda s: counts[s])

kept = 0
dropped = 0
with dst.open("wb") as f:
    for start, end, serial, _ in pages:
        if serial == target_serial:
            f.write(data[start:end])
            kept += 1
        else:
            dropped += 1

serial_report = ", ".join(f"0x{k:08x}:{v}" for k, v in sorted(counts.items()))
print(f"serial_counts={serial_report}")
print(f"target_serial=0x{target_serial:08x} kept_pages={kept} dropped_pages={dropped}")
PY

echo "[4/5] Verifying repaired audio bitstream..."
ffprobe -hide_banner -v error \
  -show_entries stream=codec_name,sample_rate,channels,duration \
  -of default=noprint_wrappers=1 "$CLEAN_OGG"

echo "[5/5] Remuxing original video + repaired audio..."
ffmpeg -hide_banner -y -v warning \
  -i "$INPUT" \
  -i "$CLEAN_OGG" \
  -map 0:v:0 -map 1:a:0 \
  -c:v copy \
  -c:a libmp3lame -b:a 128k \
  "$OUTPUT"

echo "Running audio decode smoke test..."
ffmpeg -hide_banner -v error -i "$OUTPUT" -map 0:a:0 -f null -

echo "Done: $OUTPUT"
