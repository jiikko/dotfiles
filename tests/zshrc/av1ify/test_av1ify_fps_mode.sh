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
# 🚨 -r は ffprobe の生の有理数がそのまま渡ること (10 進へ丸めない。issue 397)
assert_contains "$ffargs" "-fps_mode cfr -r 29970029/1000000" "ffmpeg receives -fps_mode cfr -r <raw avg_frame_rate>"
assert_not_contains "$ffargs" "-r 29.970" "The rounded decimal is NOT passed to ffmpeg"
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
assert_not_contains "$ffargs" "-r 29970029/1000000" "The VFR-detected average fps is NOT used when --fps is explicit"

# Test 5: avg_frame_rate が 0/0 (不明) のソースを VFR とみなさない
# WMV/ASF は avg_frame_rate=0/0 を返すことがある。VFR と誤判定すると ffmpeg に -r 0.000 が
# 渡り "Invalid framerate value" でエンコード自体が失敗する (実ファイルで rc=234 を確認)。
printf '\n## Test 5: avg_frame_rate=0/0 (unknown) keeps the old args\n'
run_with_args fpsmode5 MOCK_FPS=30/1 MOCK_AVG_FPS=0/0 --
assert_not_contains "$output" "可変フレームレート (VFR)" "avg_frame_rate=0/0 is not flagged as VFR"
assert_not_contains "$ffargs" "-fps_mode" "No -fps_mode for unknown avg_frame_rate"
assert_not_contains "$ffargs" " -r " "No -r (would be -r 0.000) for unknown avg_frame_rate"

# Test 6: avg_frame_rate の分母が 0 (24/0 = 不明) のソースも VFR とみなさない
# 🚨 fixture を 0/0 だけにしないこと (issue 399): ガードは `a <= 0 || b <= 0` なので
# 0/0 は「分子が 0」で捕まり、分母だけ 0 の 24/0 は素通りしていた (issue 397 の b)。
printf '\n## Test 6: avg_frame_rate with zero denominator (24/0) keeps the old args\n'
run_with_args fpsmode6 MOCK_FPS=30/1 MOCK_AVG_FPS=24/0 --
assert_not_contains "$output" "可変フレームレート (VFR)" "avg_frame_rate=24/0 is not flagged as VFR"
assert_not_contains "$ffargs" "-fps_mode" "No -fps_mode for zero-denominator avg_frame_rate"
assert_not_contains "$ffargs" " -r 24" "The numerator is NOT fabricated into an fps (would be -r 24.000)"

# Test 7: --fps のキャップ判定が VFR ソースで迂回されない (issue 397 の c)
# r=20 だけを見ると「20 <= 30 なので変更不要」と誤ってスキップし、target_fps が空のまま
# VFR 分岐へ落ちて **指定より高い** avg=40 で出力されていた。
printf '\n## Test 7: --fps cap is honoured for VFR sources whose avg exceeds it\n'
run_with_args fpsmode7 MOCK_FPS=20/1 MOCK_AVG_FPS=40/1 -- --fps 30
assert_contains "$ffargs" "-r 30" "ffmpeg receives the explicit --fps value"
assert_not_contains "$ffargs" "-r 40" "The higher avg fps is NOT used when --fps caps it"

# Test 8: VFR 検出閾値は postcheck のフレーム数許容% に相乗りしない (issue 398)
# AV1IFY_FRAME_TOLERANCE_PCT を両端へ振っても検出結果が変わらないこと。
printf '\n## Test 8: VFR detection is independent of AV1IFY_FRAME_TOLERANCE_PCT\n'
run_with_args fpsmode8a AV1IFY_FRAME_TOLERANCE_PCT=0 MOCK_FPS=30/1 MOCK_AVG_FPS=30000/1001 --
assert_not_contains "$output" "可変フレームレート (VFR)" "NTSC metadata is not flagged as VFR even with PCT=0"
run_with_args fpsmode8b AV1IFY_FRAME_TOLERANCE_PCT=5 MOCK_FPS=29/1 MOCK_AVG_FPS=29970029/1000000 --
assert_contains "$output" "可変フレームレート (VFR)" "A real VFR source is still detected with PCT=5"

# Test 9: 検出閾値そのもの (AV1IFY_VFR_DETECT_PCT) は効く (issue 398)
printf '\n## Test 9: AV1IFY_VFR_DETECT_PCT controls the detection\n'
run_with_args fpsmode9 AV1IFY_VFR_DETECT_PCT=10 MOCK_FPS=29/1 MOCK_AVG_FPS=29970029/1000000 --
assert_not_contains "$output" "可変フレームレート (VFR)" "Raising AV1IFY_VFR_DETECT_PCT suppresses detection"

# Test 10: 小数点がカンマのロケールでも検出と引数が壊れない (issue 397 の a)
# 🚨 awk の printf はロケールの小数点を使うため、LC_ALL=C を外すと "29,970" が生まれる。
# -r へは生の有理数を渡すので引数自体は無傷だが、**検出の比較**が壊れて VFR を取り逃す。
printf '\n## Test 10: Detection and args survive a comma-decimal locale\n'
# 🚨 `locale -a | grep -q` と書かないこと: grep -q が先に抜けて locale が SIGPIPE で死に、
# setopt pipe_fail の下では条件が常に偽 = **無言でスキップ**し続ける (実測 2026-09-19)。
_locales="$(locale -a 2>/dev/null || true)"
if [[ "$_locales" == *de_DE.UTF-8* ]]; then
  run_with_args fpsmode10 LC_ALL=de_DE.UTF-8 MOCK_FPS=29/1 MOCK_AVG_FPS=29970029/1000000 --
  assert_contains "$output" "可変フレームレート (VFR)" "VFR is still detected under a comma-decimal locale"
  assert_contains "$ffargs" "-fps_mode cfr -r 29970029/1000000" "ffmpeg still receives the raw rational -r"
  assert_not_contains "$ffargs" "," "No comma-decimal leaks into the ffmpeg args"
else
  printf '… de_DE.UTF-8 ロケールが無いのでスキップ\n'
fi

printf '\n=== VFR/CFR fps_mode Tests Completed ===\n\n'
