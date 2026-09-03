#!/usr/bin/env zsh
unset CDPATH
# shellcheck shell=bash
# av1ify --color-tags オプションテスト
# 目的: h264_nvenc 等が誤って埋め込む matrix_coefficient=0 (Identity, ffprobe表記 "gbr") を
#       auto モードでのみ bt709 に補正すること、bt709/off モードの明示挙動、
#       無効値のバリデーションを検証する。

source "${0:A:h}/test_helper.sh"

printf '\n=== av1ify --color-tags Tests ===\n\n'

# Test 1: デフォルト (未指定) は dry-run 上で auto と表示される
printf '## Test 1: Default color-tags is auto (dry-run)\n'
TEST_DIR="$TEST_TMP/test1"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
output=$(av1ify --dry-run "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "color-tags=auto" "Dry-run shows color-tags=auto by default"

# Test 2: --color-tags bt709 (dry-run)
printf '\n## Test 2: --color-tags bt709 (dry-run)\n'
TEST_DIR="$TEST_TMP/test2"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
output=$(av1ify --dry-run --color-tags bt709 "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "color-tags=bt709" "Dry-run shows color-tags=bt709"

# Test 3: --color-tags off (dry-run)
printf '\n## Test 3: --color-tags off (dry-run)\n'
TEST_DIR="$TEST_TMP/test3"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
output=$(av1ify --dry-run --color-tags off "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "color-tags=off" "Dry-run shows color-tags=off"

# Test 4: 無効な --color-tags 値はエラー終了
printf '\n## Test 4: Invalid --color-tags value (error exit)\n'
TEST_DIR="$TEST_TMP/test4"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(av1ify --dry-run --color-tags bogus "$TEST_DIR/input.avi" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "無効なcolor-tags指定" "Reports invalid color-tags value"
(( exit_code != 0 )) && printf '✓ Exit code is non-zero for invalid --color-tags (%d)\n' "$exit_code" || printf '✗ Exit code should be non-zero (got %d)\n' "$exit_code"

# --- ここから先は「決定ロジック」ではなく「決定が実際に ffmpeg へ渡ること」を検証する。
# ログ文字列だけを assert していると、-colorspace の配線を削除しても全部緑のままになる
# (実際にその false green が発生していた)。TEST_FFMPEG_ARGS_LOG に記録された実 argv を見る。

# Test 5: auto モード + ソースが Identity (gbr) → ffmpeg に -colorspace bt709 が渡る
printf '\n## Test 5: auto mode passes -colorspace bt709 to ffmpeg for Identity source\n'
TEST_DIR="$TEST_TMP/test5"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
ARGS_LOG="$TEST_DIR/ffmpeg_args"
output=$(MOCK_COLOR_SPACE=gbr TEST_FFMPEG_ARGS_LOG="$ARGS_LOG" av1ify "$TEST_DIR/input.avi" 2>&1 || true)
ffargs=$(cat "$ARGS_LOG" 2>/dev/null || true)
assert_contains "$output" "bt709 へ補正" "Auto mode logs correction message for Identity source"
assert_contains "$ffargs" "-colorspace bt709" "ffmpeg actually receives -colorspace bt709"
# primaries/trc は ffmpeg 8.0 実測で出力に反映されないため意図的に渡さない
# (詳細は __av1ify_decide_color_tags の注記)。復活したらこの assert が落ちる。
assert_not_contains "$ffargs" "-color_primaries" "ffmpeg does not receive the inert -color_primaries flag"
assert_not_contains "$ffargs" "-color_trc" "ffmpeg does not receive the inert -color_trc flag"
assert_file_exists "$TEST_DIR/input-enc.mp4" "Encode succeeds after color tag correction"

# Test 6: auto モード + ソースが bt709 → 上書きせず ffmpeg にも渡らない
printf '\n## Test 6: auto mode leaves already-correct source untouched\n'
TEST_DIR="$TEST_TMP/test6"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
ARGS_LOG="$TEST_DIR/ffmpeg_args"
output=$(MOCK_COLOR_SPACE=bt709 TEST_FFMPEG_ARGS_LOG="$ARGS_LOG" av1ify "$TEST_DIR/input.avi" 2>&1 || true)
ffargs=$(cat "$ARGS_LOG" 2>/dev/null || true)
assert_not_contains "$output" "bt709 へ補正" "Auto mode does not log correction for already-correct source"
assert_not_contains "$ffargs" "-colorspace" "ffmpeg receives no -colorspace for already-correct source"

# Test 7: bt709 モードはソースの値によらず常に上書き
printf '\n## Test 7: --color-tags bt709 always overrides regardless of source\n'
TEST_DIR="$TEST_TMP/test7"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
ARGS_LOG="$TEST_DIR/ffmpeg_args"
output=$(MOCK_COLOR_SPACE=smpte170m TEST_FFMPEG_ARGS_LOG="$ARGS_LOG" av1ify --color-tags bt709 "$TEST_DIR/input.avi" 2>&1 || true)
ffargs=$(cat "$ARGS_LOG" 2>/dev/null || true)
assert_contains "$output" "強制上書き" "bt709 mode always logs forced override"
assert_contains "$ffargs" "-colorspace bt709" "ffmpeg receives -colorspace bt709 even for non-Identity source"

# Test 8: off モードは Identity ソースでも一切上書きしない
printf '\n## Test 8: --color-tags off never overrides, even for Identity source\n'
TEST_DIR="$TEST_TMP/test8"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
ARGS_LOG="$TEST_DIR/ffmpeg_args"
output=$(MOCK_COLOR_SPACE=gbr TEST_FFMPEG_ARGS_LOG="$ARGS_LOG" av1ify --color-tags off "$TEST_DIR/input.avi" 2>&1 || true)
ffargs=$(cat "$ARGS_LOG" 2>/dev/null || true)
assert_not_contains "$output" "bt709 へ補正" "off mode does not correct Identity source"
assert_not_contains "$ffargs" "-colorspace" "ffmpeg receives no -colorspace in off mode"

# Test 9: AV1_COLOR_TAGS 環境変数が CLI 未指定時に適用される
printf '\n## Test 9: AV1_COLOR_TAGS env var applies when CLI option omitted\n'
TEST_DIR="$TEST_TMP/test9"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
output=$(AV1_COLOR_TAGS=off av1ify --dry-run "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "color-tags=off" "AV1_COLOR_TAGS env var reflected in dry-run plan"

# Test 10: CLI オプションが環境変数より優先される
printf '\n## Test 10: CLI --color-tags takes precedence over AV1_COLOR_TAGS\n'
TEST_DIR="$TEST_TMP/test10"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
output=$(AV1_COLOR_TAGS=off av1ify --dry-run --color-tags bt709 "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "color-tags=bt709" "CLI option overrides env var in dry-run plan"

# Test 11: 補正されていない Identity ソースの失敗を「音声copy失敗」と誤診断しない
# 背景: 失敗時の retry は「失敗 = 音声 copy が原因」と決め打つため、映像エンコーダが
# Identity matrix を拒否したケースでも音声を疑うメッセージが出て、同じ理由で必ず落ちる
# 2 回目を空振りさせていた。原因と対処 (--color-tags) に辿り着ける必要がある。
printf '\n## Test 11: Identity failure is not misdiagnosed as an audio-copy failure\n'
TEST_DIR="$TEST_TMP/test11"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
ARGS_LOG="$TEST_DIR/ffmpeg_args"
output=$(MOCK_COLOR_SPACE=gbr MOCK_FFMPEG_FAIL=1 TEST_FFMPEG_ARGS_LOG="$ARGS_LOG" \
  av1ify --color-tags off "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "--color-tags bt709" "Failure message points at the color-tags remedy"
assert_contains "$output" "Identity" "Failure message names the actual cause (Identity matrix)"
assert_not_contains "$output" "音声copy失敗" "Does not blame the audio copy for a video-side failure"
# 空振りする 2 回目を出さない = ffmpeg 呼び出しは 1 回だけ
attempts=$(grep -c . "$ARGS_LOG" 2>/dev/null || echo 0)
if [[ "$attempts" == "1" ]]; then
  printf '✓ Does not burn a second doomed ffmpeg attempt (attempts=%s)\n' "$attempts"
else
  printf '✗ Expected exactly 1 ffmpeg attempt, got %s\n' "$attempts"
fi

# Test 12: 補正済みなのに失敗したケースは従来どおり音声リトライへ流す (Test 11 の過剰適用防止)
printf '\n## Test 12: Corrected source still falls through to the normal audio retry\n'
TEST_DIR="$TEST_TMP/test12"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
output=$(MOCK_COLOR_SPACE=gbr MOCK_FFMPEG_FAIL=1 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "音声copy失敗" "Already-corrected failure still uses the audio retry path"
assert_not_contains "$output" "--color-tags bt709" "Does not suggest a remedy that is already applied"

printf '\n=== av1ify --color-tags Tests Complete ===\n\n'
