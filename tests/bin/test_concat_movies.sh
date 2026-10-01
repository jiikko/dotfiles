#!/usr/bin/env bash
# bin/concat_movies が ffmpeg へ渡す fade の開始位置を、偽の ffmpeg / ffprobe で固定する (本物の ffmpeg は呼ばない)。
#
# 守るもの (issue 610): 1 未満の開始位置を `0.700` の形で渡す (bc は `.7` を出し、ffmpeg が拒否する) /
# stream の duration が N/A ならコンテナの長さへ落とす (bc は `N/A - 0.7` を黙って 1.3 にする) /
# 長さが数値でない・フェードアウトより短いときは ffmpeg を呼ばずに非 0 で止まる。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SUT="$ROOT_DIR/bin/concat_movies"
fails=0
ok()   { printf '✓ %s\n' "$1"; }
fail() { printf '✗ %s\n' "$1" >&2; fails=$((fails + 1)); }

WORK="$(mktemp -d "${TMPDIR:-/tmp}/test-concat-movies.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

# 偽の ffprobe: 解像度の問い合わせと、stream / format の duration を環境変数で返す
mkdir -p "$WORK/fakebin"
cat > "$WORK/fakebin/ffprobe" <<'SH'
#!/bin/sh
case "$*" in
  *stream=width*) echo "1920,1080,30/1" ;;
  *stream=duration*) echo "${FAKE_STREAM_DUR:-}" ;;
  *format=duration*) echo "${FAKE_FORMAT_DUR:-}" ;;
esac
SH
# 偽の ffmpeg: 引数を 1 行ずつ記録し、最後の引数 (出力) を作る
cat > "$WORK/fakebin/ffmpeg" <<'SH'
#!/bin/sh
for a in "$@"; do printf '%s\n' "$a"; done >> "$FFMPEG_LOG"
echo "---" >> "$FFMPEG_LOG"
for last in "$@"; do :; done
: > "$last"
SH
chmod +x "$WORK/fakebin/ffprobe" "$WORK/fakebin/ffmpeg"

: > "$WORK/thumb.png"
: > "$WORK/clip.mp4"

# $1=名前 残り=concat_movies の引数。rc を RC に、ffmpeg の記録を LOG に置く
run_case() {
  local name="$1"; shift
  LOG="$WORK/$name.ffmpeg.log"; : > "$LOG"
  rm -f "$WORK/out-$name.mp4"
  RC=0
  FFMPEG_LOG="$LOG" PATH="$WORK/fakebin:$PATH" \
    "$SUT" --thumbnail "$WORK/thumb.png" -o "$WORK/out-$name.mp4" "$@" "$WORK/clip.mp4" \
    > "$WORK/$name.out" 2> "$WORK/$name.err" || RC=$?
}

# 1. サムネイル 1 秒 - フェード 0.3 秒 → st=0.700 (先頭の 0 が落ちない)
FAKE_STREAM_DUR=5.0 run_case thumb_lt1 --thumbnail-duration 1 --fadeout 0.3
if [[ $RC -eq 0 ]] && grep -qxF '[0:v]scale=1920:1080:flags=lanczos,setsar=1,format=yuv420p,fade=t=out:st=0.700:d=0.3[v]' "$LOG"; then
  ok "1 未満の開始位置を 0.700 で渡す"
else
  fail "1 未満の開始位置: rc=$RC $(grep 'fade=' "$LOG" | head -1) $(cat "$WORK/thumb_lt1.err")"
fi

# 2. クリップの長さ 0.9 秒 - 0.3 秒 → st=0.600 (映像と音声の両方)
FAKE_STREAM_DUR=0.9 run_case clip_lt1 --fadeout 0.3
if [[ $RC -eq 0 ]] && grep -qx 'fade=t=out:st=0.600:d=0.3' "$LOG" && grep -qx 'afade=t=out:st=0.600:d=0.3' "$LOG"; then
  ok "短いクリップの開始位置を 0.600 で渡す"
else
  fail "短いクリップ: rc=$RC $(grep -c 'st=' "$LOG") $(cat "$WORK/clip_lt1.err")"
fi

# 3. stream の duration が N/A → format の duration (3.0) を使う → st=2.300
FAKE_STREAM_DUR=N/A FAKE_FORMAT_DUR=3.000000 run_case stream_na --fadeout 0.7
if [[ $RC -eq 0 ]] && grep -qx 'fade=t=out:st=2.300:d=0.7' "$LOG"; then
  ok "stream の N/A をコンテナの長さへ落とす"
else
  fail "N/A の落とし先: rc=$RC $(grep 'fade=' "$LOG" | head -2 | tr '\n' ' ') $(cat "$WORK/stream_na.err")"
fi

# 4. どちらも N/A → クリップの ffmpeg を呼ばずに非 0 (サムネイルの 1 回だけ)
FAKE_STREAM_DUR=N/A FAKE_FORMAT_DUR=N/A run_case both_na --fadeout 0.7
if [[ $RC -ne 0 ]] && [[ "$(grep -c -- '---' "$LOG")" -eq 1 ]] && grep -q '数値ではありません' "$WORK/both_na.err"; then
  ok "長さが取れなければ止まる"
else
  fail "長さが取れない: rc=$RC ffmpeg=$(grep -c -- '---' "$LOG")回 $(cat "$WORK/both_na.err")"
fi

# 5. フェードアウトがクリップより長い → 非 0
FAKE_STREAM_DUR=0.5 run_case too_long --fadeout 0.7
if [[ $RC -ne 0 ]] && grep -q 'より短い' "$WORK/too_long.err"; then
  ok "フェードアウトより短いクリップで止まる"
else
  fail "フェードアウトより短い: rc=$RC $(cat "$WORK/too_long.err")"
fi

# 6. フェードアウト無しなら長さを問い合わせない (従来どおり)
FAKE_STREAM_DUR=N/A run_case no_fade
if [[ $RC -eq 0 ]] && ! grep -q 'fade=' "$LOG"; then
  ok "フェードアウト無しは fade を渡さない"
else
  fail "フェードアウト無し: rc=$RC $(cat "$WORK/no_fade.err")"
fi

if (( fails )); then
  echo "FAIL: $fails 件" >&2
  exit 1
fi
echo "OK concat_movies (6 件)"
