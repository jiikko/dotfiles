#!/usr/bin/env zsh
unset CDPATH
# shellcheck shell=bash
# av1ify VFR (可変フレームレート) 正規化テスト (issue 394)
#
# 目的: VFR ソース (r_frame_rate と avg_frame_rate が乖離するソース) を素通しすると
#       出力も VFR のままになる (QuickTime でブロックノイズが出た出力はこれ)。av1ify は **検出した
#       ソース (かつ --fps 未指定) にだけ** -fps_mode cfr -r <avg fps> を付ける。
#
# 🚨 守りたい不変条件は 2 つ:
#   1. 検出したソースには -fps_mode cfr と実測平均fpsの -r が渡る
#   2. **それ以外のソースへ渡す引数は改修前と同一** (-fps_mode も -r も足さない)。
#      cfr を常時付与すると従来 OK だったファイルのフレーム数が変わり check_ng になる
#      (理由の実測は __av1ify_detect_vfr の注記)
# ログ文字列だけを assert すると配線が消えても緑のままになる (test_av1ify_color_tags.sh
# Test 5 と同じ false green) ため、TEST_FFMPEG_ARGS_LOG に記録された実 argv を見る。

source "${0:A:h}/test_helper.sh"

printf '\n=== av1ify VFR/CFR fps_mode Tests ===\n\n'

# 引数ログを取りながら av1ify を 1 回走らせる。結果は $output / $ffargs に入る
run_with_args() {  # <test_dir_name> [env assignments...] -- [av1ify args...]
  local name="$1"; shift
  TEST_DIR="$TEST_TMP/$name"
  mkdir -p "$TEST_DIR"
  echo "dummy video" > "$TEST_DIR/input.avi"
  cd "$TEST_DIR" || exit 1
  ARGS_LOG="$TEST_DIR/ffmpeg_args"
  local -a envs=()
  while (( $# )) && [[ "$1" != "--" ]]; do envs+=("$1"); shift; done
  shift
  output=$( (( ${#envs[@]} )) && export "${envs[@]}"; export TEST_FFMPEG_ARGS_LOG="$ARGS_LOG"; av1ify "$@" "$TEST_DIR/input.avi" 2>&1 || true)
  ffargs=$(cat "$ARGS_LOG" 2>/dev/null || true)
}

# Test 1: CFR ソース (r_frame_rate と avg_frame_rate が一致) は改修前と同じ引数
printf '## Test 1: CFR source gets neither -fps_mode nor -r (args unchanged from before)\n'
run_with_args fpsmode1 --
assert_not_contains "$output" "可変フレームレート (VFR)" "No VFR message for CFR source"
assert_not_contains "$ffargs" "-fps_mode" "ffmpeg receives no -fps_mode for CFR source"
assert_not_contains "$ffargs" " -r " "ffmpeg receives no -r for CFR source without --fps"
assert_file_exists "$TEST_DIR/input-enc.mp4" "Encode succeeds for CFR source"

# Test 2: VFR ソース (r_frame_rate=29/1, avg_frame_rate≈29.970) には cfr + 実測平均fps
printf '\n## Test 2: VFR source gets -fps_mode cfr + explicit -r <avg fps>\n'
run_with_args fpsmode2 MOCK_FPS=29/1 MOCK_AVG_FPS=29970029/1000000 --
assert_contains "$output" "可変フレームレート (VFR)" "VFR source logs the normalization message"
assert_contains "$ffargs" "-fps_mode cfr -r 29.970" "ffmpeg receives -fps_mode cfr -r <measured avg fps>"
assert_file_exists "$TEST_DIR/input-enc.mp4" "Encode succeeds for VFR source"

# Test 3: 許容%以内の小さな乖離は VFR とみなさず、改修前と同じ引数
printf '\n## Test 3: A small fps discrepancy within tolerance keeps the old args\n'
# 29.970 vs 29.980 → 差 0.033% ≪ 0.5% (デフォルト許容)
run_with_args fpsmode3 MOCK_FPS=29970029/1000000 MOCK_AVG_FPS=2998/100 --
assert_not_contains "$output" "可変フレームレート (VFR)" "Small discrepancy is not flagged as VFR"
assert_not_contains "$ffargs" "-fps_mode" "No -fps_mode for near-CFR source"
assert_not_contains "$ffargs" " -r " "No -r for near-CFR source"

# Test 4: --fps 明示時は VFR ソースでも改修前と同じ (-r <指定値> だけ。cfr も平均fpsも足さない)
printf '\n## Test 4: Explicit --fps keeps the old args even for VFR sources\n'
run_with_args fpsmode4 MOCK_FPS=29/1 MOCK_AVG_FPS=29970029/1000000 -- --fps 24
assert_contains "$ffargs" "-r 24" "ffmpeg receives the explicit --fps value"
assert_not_contains "$ffargs" "-fps_mode" "No -fps_mode added when --fps is explicit"
assert_not_contains "$ffargs" "-r 29.970" "The VFR-detected average fps is NOT used when --fps is explicit"

# Test 5: avg_frame_rate が 0/0 (不明) のソースを VFR とみなさない
# WMV/ASF は avg_frame_rate=0/0 を返すことがある。VFR と誤判定すると ffmpeg に -r 0.000 が
# 渡り "Invalid framerate value" でエンコード自体が失敗する (実ファイルで rc=234 を確認)。
printf '\n## Test 5: avg_frame_rate=0/0 (unknown) keeps the old args\n'
run_with_args fpsmode5 MOCK_FPS=30/1 MOCK_AVG_FPS=0/0 --
assert_not_contains "$output" "可変フレームレート (VFR)" "avg_frame_rate=0/0 is not flagged as VFR"
assert_not_contains "$ffargs" "-fps_mode" "No -fps_mode for unknown avg_frame_rate"
assert_not_contains "$ffargs" " -r " "No -r (would be -r 0.000) for unknown avg_frame_rate"

printf '\n=== VFR/CFR fps_mode Tests Completed ===\n\n'
