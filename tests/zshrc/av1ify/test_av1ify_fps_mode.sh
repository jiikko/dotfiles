#!/usr/bin/env zsh
unset CDPATH
# shellcheck shell=bash
# av1ify VFR (可変フレームレート) 正規化テスト (issue 394)
#
# 目的: VFR ソース (r_frame_rate と avg_frame_rate が乖離するソース) を SVT-AV1 で
#       エンコードすると出力の DTS が非単調増加になり、QuickTime 等の厳密なプレイヤーで
#       再生破綻する (VLC/dav1d は寛容なため無症状)。av1ify は常時 -fps_mode cfr を
#       付与してこれを防ぐ。ログ文字列だけを assert すると配線が消えても緑のままになる
#       (test_av1ify_color_tags.sh Test 5 と同じ false green の教訓) ため、
#       TEST_FFMPEG_ARGS_LOG に記録された実 argv を見る。

source "${0:A:h}/test_helper.sh"

printf '\n=== av1ify VFR/CFR fps_mode Tests ===\n\n'

# Test 1: CFR ソース (r_frame_rate と avg_frame_rate が一致) でも -fps_mode cfr は常時付与される
printf '## Test 1: -fps_mode cfr is always passed to ffmpeg, even for CFR sources\n'
TEST_DIR="$TEST_TMP/fpsmode1"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
ARGS_LOG="$TEST_DIR/ffmpeg_args"
output=$(TEST_FFMPEG_ARGS_LOG="$ARGS_LOG" av1ify "$TEST_DIR/input.avi" 2>&1 || true)
ffargs=$(cat "$ARGS_LOG" 2>/dev/null || true)
assert_contains "$ffargs" "-fps_mode cfr" "ffmpeg receives -fps_mode cfr for CFR source"
assert_file_exists "$TEST_DIR/input-enc.mp4" "Encode still succeeds"

# Test 2: CFR ソースでは VFR 検出メッセージが出ない / 明示的な -r は追加されない
printf '\n## Test 2: CFR source does not trigger VFR normalization message or extra -r\n'
TEST_DIR="$TEST_TMP/fpsmode2"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
ARGS_LOG="$TEST_DIR/ffmpeg_args"
output=$(TEST_FFMPEG_ARGS_LOG="$ARGS_LOG" av1ify "$TEST_DIR/input.avi" 2>&1 || true)
ffargs=$(cat "$ARGS_LOG" 2>/dev/null || true)
assert_not_contains "$output" "可変フレームレート (VFR)" "No VFR message for CFR source"
assert_not_contains "$ffargs" " -r " "ffmpeg receives no explicit -r for CFR source without --fps"

# Test 3: VFR ソース (r_frame_rate=29/1, avg_frame_rate≈29.970) を検出し、
#         ffmpeg に -fps_mode cfr と実測平均fpsの -r が渡る
printf '\n## Test 3: VFR source triggers -fps_mode cfr + explicit -r <avg fps>\n'
TEST_DIR="$TEST_TMP/fpsmode3"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
ARGS_LOG="$TEST_DIR/ffmpeg_args"
output=$(MOCK_FPS="29/1" MOCK_AVG_FPS="29970029/1000000" TEST_FFMPEG_ARGS_LOG="$ARGS_LOG" av1ify "$TEST_DIR/input.avi" 2>&1 || true)
ffargs=$(cat "$ARGS_LOG" 2>/dev/null || true)
assert_contains "$output" "可変フレームレート (VFR)" "VFR source logs the normalization message"
assert_contains "$ffargs" "-fps_mode cfr" "ffmpeg receives -fps_mode cfr for VFR source"
assert_contains "$ffargs" "-r 29.970" "ffmpeg receives explicit -r with the measured average fps"
assert_file_exists "$TEST_DIR/input-enc.mp4" "Encode still succeeds for VFR source"

# Test 4: 近接した乖離 (許容%以内) は VFR とみなさない — 誤検出防止
printf '\n## Test 4: A small fps discrepancy within tolerance is NOT treated as VFR\n'
TEST_DIR="$TEST_TMP/fpsmode4"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
ARGS_LOG="$TEST_DIR/ffmpeg_args"
# 29.970 vs 29.980 → 差 0.033% ≪ 0.5% (デフォルト許容) なので VFR 扱いしない
output=$(MOCK_FPS="29970029/1000000" MOCK_AVG_FPS="2998/100" TEST_FFMPEG_ARGS_LOG="$ARGS_LOG" av1ify "$TEST_DIR/input.avi" 2>&1 || true)
ffargs=$(cat "$ARGS_LOG" 2>/dev/null || true)
assert_not_contains "$output" "可変フレームレート (VFR)" "Small fps discrepancy within tolerance is not flagged as VFR"
assert_not_contains "$ffargs" " -r " "No explicit -r added for near-CFR source"

# Test 5: --fps 明示指定が VFR 検出時の自動 -r より優先される
printf '\n## Test 5: Explicit --fps takes priority over VFR-detected -r\n'
TEST_DIR="$TEST_TMP/fpsmode5"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
ARGS_LOG="$TEST_DIR/ffmpeg_args"
# ソースは VFR (29/1 vs 29.970) だが --fps 24 を明示 → -r 24 が優先され、VFR の平均fpsは使われない
output=$(MOCK_FPS="29/1" MOCK_AVG_FPS="29970029/1000000" TEST_FFMPEG_ARGS_LOG="$ARGS_LOG" av1ify --fps 24 "$TEST_DIR/input.avi" 2>&1 || true)
ffargs=$(cat "$ARGS_LOG" 2>/dev/null || true)
assert_contains "$ffargs" "-r 24" "ffmpeg receives the explicit --fps value"
assert_not_contains "$ffargs" "-r 29.970" "The VFR-detected average fps is NOT used when --fps is explicit"

printf '\n=== VFR/CFR fps_mode Tests Completed ===\n\n'
