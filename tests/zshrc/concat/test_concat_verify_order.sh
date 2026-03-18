#!/usr/bin/env zsh
# shellcheck shell=bash
# concat フレーム順序検証テスト

source "${0:A:h}/test_helper.sh"

printf '\n=== concat Frame Order Verification Tests ===\n\n'

# __concat_frame_hash と __concat_get_duration をモックでオーバーライド

# テスト用のハッシュマップ（ファイル:タイムスタンプ → ハッシュ）
typeset -A MOCK_FRAME_HASHES
typeset -A MOCK_DURATIONS

__concat_frame_hash() {
  local file="$1" timestamp="$2"
  local key="${file}:${timestamp}"
  if [[ -n "${MOCK_FRAME_HASHES[$key]}" ]]; then
    echo "${MOCK_FRAME_HASHES[$key]}"
  else
    echo "unknown_hash_${file:t}_${timestamp}"
  fi
}

__concat_get_duration() {
  local file="$1"
  if [[ -n "${MOCK_DURATIONS[$file]}" ]]; then
    echo "${MOCK_DURATIONS[$file]}"
  else
    echo ""
  fi
}

# ============================================================
# Test 1: 正常ケース — 2ファイルが正しい順序で結合
# ============================================================
printf '## Test 1: Two files in correct order\n'
TEST_DIR="$TEST_TMP/verify_order_1"
mkdir -p "$TEST_DIR"
touch "$TEST_DIR/video_001.mp4" "$TEST_DIR/video_002.mp4" "$TEST_DIR/output.mp4"

MOCK_DURATIONS=()
MOCK_DURATIONS[$TEST_DIR/video_001.mp4]="10.0"
MOCK_DURATIONS[$TEST_DIR/video_002.mp4]="10.0"

# sample_t for dur=10: min(10*0.3, 10) = 3.0, max(0.5, 3.0) = 3.0, min(3.0, 10*0.8=8.0) = 3.0
MOCK_FRAME_HASHES=()
MOCK_FRAME_HASHES[$TEST_DIR/video_001.mp4:3.000]="hash_a"
MOCK_FRAME_HASHES[$TEST_DIR/output.mp4:3.000]="hash_a"      # cumulative=0 + 3.0 = 3.0
MOCK_FRAME_HASHES[$TEST_DIR/video_002.mp4:3.000]="hash_b"
MOCK_FRAME_HASHES[$TEST_DIR/output.mp4:13.000]="hash_b"     # cumulative=10.0 + 3.0 = 13.0

unsetopt err_exit
__concat_verify_frame_order "$TEST_DIR/output.mp4" "$TEST_DIR/video_001.mp4" "$TEST_DIR/video_002.mp4"
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Correct order returns success"

# ============================================================
# Test 2: 不正ケース — フレームハッシュ不一致
# ============================================================
printf '\n## Test 2: Frame hash mismatch (wrong order)\n'
TEST_DIR="$TEST_TMP/verify_order_2"
mkdir -p "$TEST_DIR"
touch "$TEST_DIR/video_001.mp4" "$TEST_DIR/video_002.mp4" "$TEST_DIR/output.mp4"

MOCK_DURATIONS=()
MOCK_DURATIONS[$TEST_DIR/video_001.mp4]="10.0"
MOCK_DURATIONS[$TEST_DIR/video_002.mp4]="10.0"

MOCK_FRAME_HASHES=()
MOCK_FRAME_HASHES[$TEST_DIR/video_001.mp4:3.000]="hash_a"
MOCK_FRAME_HASHES[$TEST_DIR/output.mp4:3.000]="hash_WRONG"  # 不一致

unsetopt err_exit
__concat_verify_frame_order "$TEST_DIR/output.mp4" "$TEST_DIR/video_001.mp4" "$TEST_DIR/video_002.mp4"
exit_code=$?
setopt err_exit
assert_exit_code "1" "$exit_code" "Mismatch returns error"
assert_contains "$REPLY" "フレーム不一致" "Error message mentions frame mismatch"
assert_contains "$REPLY" "video_001.mp4" "Error message identifies the file"

# ============================================================
# Test 3: 3ファイルが正しい順序で結合
# ============================================================
printf '\n## Test 3: Three files in correct order\n'
TEST_DIR="$TEST_TMP/verify_order_3"
mkdir -p "$TEST_DIR"
touch "$TEST_DIR/clip_001.mp4" "$TEST_DIR/clip_002.mp4" "$TEST_DIR/clip_003.mp4" "$TEST_DIR/output.mp4"

MOCK_DURATIONS=()
MOCK_DURATIONS[$TEST_DIR/clip_001.mp4]="5.0"
MOCK_DURATIONS[$TEST_DIR/clip_002.mp4]="8.0"
MOCK_DURATIONS[$TEST_DIR/clip_003.mp4]="12.0"

# clip_001: dur=5.0, sample_t = 1.500
# clip_002: dur=8.0, sample_t = 2.400
# clip_003: dur=12.0, sample_t = 3.600
MOCK_FRAME_HASHES=()
MOCK_FRAME_HASHES[$TEST_DIR/clip_001.mp4:1.500]="hash_1"
MOCK_FRAME_HASHES[$TEST_DIR/output.mp4:1.500]="hash_1"      # cumulative=0 + 1.5
MOCK_FRAME_HASHES[$TEST_DIR/clip_002.mp4:2.400]="hash_2"
MOCK_FRAME_HASHES[$TEST_DIR/output.mp4:7.400]="hash_2"      # cumulative=5.0 + 2.4
MOCK_FRAME_HASHES[$TEST_DIR/clip_003.mp4:3.600]="hash_3"
MOCK_FRAME_HASHES[$TEST_DIR/output.mp4:16.600]="hash_3"     # cumulative=5.0+8.0 + 3.6

unsetopt err_exit
__concat_verify_frame_order "$TEST_DIR/output.mp4" "$TEST_DIR/clip_001.mp4" "$TEST_DIR/clip_002.mp4" "$TEST_DIR/clip_003.mp4"
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Three files in correct order returns success"

# ============================================================
# Test 4: 3ファイルで2番目のファイルが不一致
# ============================================================
printf '\n## Test 4: Three files, second file mismatch\n'
TEST_DIR="$TEST_TMP/verify_order_4"
mkdir -p "$TEST_DIR"
touch "$TEST_DIR/clip_001.mp4" "$TEST_DIR/clip_002.mp4" "$TEST_DIR/clip_003.mp4" "$TEST_DIR/output.mp4"

MOCK_DURATIONS=()
MOCK_DURATIONS[$TEST_DIR/clip_001.mp4]="5.0"
MOCK_DURATIONS[$TEST_DIR/clip_002.mp4]="8.0"
MOCK_DURATIONS[$TEST_DIR/clip_003.mp4]="12.0"

MOCK_FRAME_HASHES=()
MOCK_FRAME_HASHES[$TEST_DIR/clip_001.mp4:1.500]="hash_1"
MOCK_FRAME_HASHES[$TEST_DIR/output.mp4:1.500]="hash_1"      # OK
MOCK_FRAME_HASHES[$TEST_DIR/clip_002.mp4:2.400]="hash_2"
MOCK_FRAME_HASHES[$TEST_DIR/output.mp4:7.400]="hash_WRONG"  # 不一致

unsetopt err_exit
__concat_verify_frame_order "$TEST_DIR/output.mp4" "$TEST_DIR/clip_001.mp4" "$TEST_DIR/clip_002.mp4" "$TEST_DIR/clip_003.mp4"
exit_code=$?
setopt err_exit
assert_exit_code "1" "$exit_code" "Second file mismatch returns error"
assert_contains "$REPLY" "フレーム不一致" "Error message mentions frame mismatch"
assert_contains "$REPLY" "clip_002.mp4" "Error message identifies the second file"

# ============================================================
# Test 5: duration取得失敗
# ============================================================
printf '\n## Test 5: Duration retrieval failure\n'
TEST_DIR="$TEST_TMP/verify_order_5"
mkdir -p "$TEST_DIR"
touch "$TEST_DIR/video_001.mp4" "$TEST_DIR/video_002.mp4" "$TEST_DIR/output.mp4"

MOCK_DURATIONS=()
# video_001のdurationを設定しない → 空文字が返る

MOCK_FRAME_HASHES=()

unsetopt err_exit
__concat_verify_frame_order "$TEST_DIR/output.mp4" "$TEST_DIR/video_001.mp4" "$TEST_DIR/video_002.mp4"
exit_code=$?
setopt err_exit
assert_exit_code "1" "$exit_code" "Missing duration returns error"
assert_contains "$REPLY" "duration取得失敗" "Error message mentions duration failure"

# ============================================================
# Test 6: 短い動画（sample_tが0.5sに切り上げ）
# ============================================================
printf '\n## Test 6: Short video (sample_t clamped to 0.5s)\n'
TEST_DIR="$TEST_TMP/verify_order_6"
mkdir -p "$TEST_DIR"
touch "$TEST_DIR/short_001.mp4" "$TEST_DIR/short_002.mp4" "$TEST_DIR/output.mp4"

MOCK_DURATIONS=()
MOCK_DURATIONS[$TEST_DIR/short_001.mp4]="1.0"
MOCK_DURATIONS[$TEST_DIR/short_002.mp4]="1.0"

# dur=1.0: sample_t = 0.500
MOCK_FRAME_HASHES=()
MOCK_FRAME_HASHES[$TEST_DIR/short_001.mp4:0.500]="hash_short_a"
MOCK_FRAME_HASHES[$TEST_DIR/output.mp4:0.500]="hash_short_a"    # cumulative=0 + 0.5
MOCK_FRAME_HASHES[$TEST_DIR/short_002.mp4:0.500]="hash_short_b"
MOCK_FRAME_HASHES[$TEST_DIR/output.mp4:1.500]="hash_short_b"    # cumulative=1.0 + 0.5

unsetopt err_exit
__concat_verify_frame_order "$TEST_DIR/output.mp4" "$TEST_DIR/short_001.mp4" "$TEST_DIR/short_002.mp4"
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Short video with clamped sample_t succeeds"

# ============================================================
# Test 7: 長い動画（sample_tが10sにキャップ）
# ============================================================
printf '\n## Test 7: Long video (sample_t capped at 10s)\n'
TEST_DIR="$TEST_TMP/verify_order_7"
mkdir -p "$TEST_DIR"
touch "$TEST_DIR/long_001.mp4" "$TEST_DIR/long_002.mp4" "$TEST_DIR/output.mp4"

MOCK_DURATIONS=()
MOCK_DURATIONS[$TEST_DIR/long_001.mp4]="3600.0"
MOCK_DURATIONS[$TEST_DIR/long_002.mp4]="3600.0"

# dur=3600: sample_t = 10.000
MOCK_FRAME_HASHES=()
MOCK_FRAME_HASHES[$TEST_DIR/long_001.mp4:10.000]="hash_long_a"
MOCK_FRAME_HASHES[$TEST_DIR/output.mp4:10.000]="hash_long_a"      # cumulative=0 + 10
MOCK_FRAME_HASHES[$TEST_DIR/long_002.mp4:10.000]="hash_long_b"
MOCK_FRAME_HASHES[$TEST_DIR/output.mp4:3610.000]="hash_long_b"    # cumulative=3600 + 10

unsetopt err_exit
__concat_verify_frame_order "$TEST_DIR/output.mp4" "$TEST_DIR/long_001.mp4" "$TEST_DIR/long_002.mp4"
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Long video with capped sample_t succeeds"

# ============================================================
# Test 8: フレーム抽出失敗（空ハッシュ）
# ============================================================
printf '\n## Test 8: Frame extraction failure (empty hash)\n'
TEST_DIR="$TEST_TMP/verify_order_8"
mkdir -p "$TEST_DIR"
touch "$TEST_DIR/video_001.mp4" "$TEST_DIR/video_002.mp4" "$TEST_DIR/output.mp4"

MOCK_DURATIONS=()
MOCK_DURATIONS[$TEST_DIR/video_001.mp4]="10.0"
MOCK_DURATIONS[$TEST_DIR/video_002.mp4]="10.0"

# input_hashに空文字が返るようにモックをオーバーライド（alwaysブロックで復元を保証）
{
  __concat_frame_hash() {
    echo ""  # 常に空を返す
  }

  unsetopt err_exit
  output=$(__concat_verify_frame_order "$TEST_DIR/output.mp4" "$TEST_DIR/video_001.mp4" "$TEST_DIR/video_002.mp4" 2>&1)
  exit_code=$?
  setopt err_exit
  assert_exit_code "0" "$exit_code" "Empty hash skips verification (no error)"
  assert_contains "$output" "フレーム抽出スキップ" "Warning message mentions extraction skip"
} always {
  # モックを復元（アサーション失敗でも必ず実行される）
  __concat_frame_hash() {
    local file="$1" timestamp="$2"
    local key="${file}:${timestamp}"
    if [[ -n "${MOCK_FRAME_HASHES[$key]}" ]]; then
      echo "${MOCK_FRAME_HASHES[$key]}"
    else
      echo "unknown_hash_${file:t}_${timestamp}"
    fi
  }
}

# ============================================================
# Test 9: 独自ソート — 未ソート順で渡しても正しくソートされる
# ============================================================
printf '\n## Test 9: Independent sort — unsorted input is sorted correctly\n'
TEST_DIR="$TEST_TMP/verify_order_9"
mkdir -p "$TEST_DIR"
touch "$TEST_DIR/vid_003.mp4" "$TEST_DIR/vid_001.mp4" "$TEST_DIR/vid_002.mp4" "$TEST_DIR/output.mp4"

MOCK_DURATIONS=()
MOCK_DURATIONS[$TEST_DIR/vid_001.mp4]="10.0"
MOCK_DURATIONS[$TEST_DIR/vid_002.mp4]="10.0"
MOCK_DURATIONS[$TEST_DIR/vid_003.mp4]="10.0"

# 正しい順序: vid_001 → vid_002 → vid_003（独自ソートで並び替えられるはず）
MOCK_FRAME_HASHES=()
MOCK_FRAME_HASHES[$TEST_DIR/vid_001.mp4:3.000]="hash_1"
MOCK_FRAME_HASHES[$TEST_DIR/output.mp4:3.000]="hash_1"       # cumulative=0 + 3.0
MOCK_FRAME_HASHES[$TEST_DIR/vid_002.mp4:3.000]="hash_2"
MOCK_FRAME_HASHES[$TEST_DIR/output.mp4:13.000]="hash_2"      # cumulative=10.0 + 3.0
MOCK_FRAME_HASHES[$TEST_DIR/vid_003.mp4:3.000]="hash_3"
MOCK_FRAME_HASHES[$TEST_DIR/output.mp4:23.000]="hash_3"      # cumulative=20.0 + 3.0

# 逆順で渡す: 003, 001, 002
unsetopt err_exit
__concat_verify_frame_order "$TEST_DIR/output.mp4" "$TEST_DIR/vid_003.mp4" "$TEST_DIR/vid_001.mp4" "$TEST_DIR/vid_002.mp4"
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Unsorted input is correctly sorted by independent logic"

# ============================================================
# Test 10: 独自ソート — (N)パターン
# ============================================================
printf '\n## Test 10: Independent sort — (N) pattern\n'
TEST_DIR="$TEST_TMP/verify_order_10"
mkdir -p "$TEST_DIR"
touch "$TEST_DIR/movie(2).mp4" "$TEST_DIR/movie(1).mp4" "$TEST_DIR/output.mp4"

MOCK_DURATIONS=()
MOCK_DURATIONS[$TEST_DIR/movie(1).mp4]="10.0"
MOCK_DURATIONS[$TEST_DIR/movie(2).mp4]="10.0"

MOCK_FRAME_HASHES=()
MOCK_FRAME_HASHES[$TEST_DIR/movie(1).mp4:3.000]="hash_m1"
MOCK_FRAME_HASHES[$TEST_DIR/output.mp4:3.000]="hash_m1"
MOCK_FRAME_HASHES[$TEST_DIR/movie(2).mp4:3.000]="hash_m2"
MOCK_FRAME_HASHES[$TEST_DIR/output.mp4:13.000]="hash_m2"

# 逆順で渡す
unsetopt err_exit
__concat_verify_frame_order "$TEST_DIR/output.mp4" "$TEST_DIR/movie(2).mp4" "$TEST_DIR/movie(1).mp4"
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "(N) pattern sorted correctly"

# ============================================================
# Test 11: 独自ソート — partNパターン
# ============================================================
printf '\n## Test 11: Independent sort — partN pattern\n'
TEST_DIR="$TEST_TMP/verify_order_11"
mkdir -p "$TEST_DIR"
touch "$TEST_DIR/file_part2.mp4" "$TEST_DIR/file_part1.mp4" "$TEST_DIR/output.mp4"

MOCK_DURATIONS=()
MOCK_DURATIONS[$TEST_DIR/file_part1.mp4]="10.0"
MOCK_DURATIONS[$TEST_DIR/file_part2.mp4]="10.0"

MOCK_FRAME_HASHES=()
MOCK_FRAME_HASHES[$TEST_DIR/file_part1.mp4:3.000]="hash_p1"
MOCK_FRAME_HASHES[$TEST_DIR/output.mp4:3.000]="hash_p1"
MOCK_FRAME_HASHES[$TEST_DIR/file_part2.mp4:3.000]="hash_p2"
MOCK_FRAME_HASHES[$TEST_DIR/output.mp4:13.000]="hash_p2"

# 逆順で渡す
unsetopt err_exit
__concat_verify_frame_order "$TEST_DIR/output.mp4" "$TEST_DIR/file_part2.mp4" "$TEST_DIR/file_part1.mp4"
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "partN pattern sorted correctly"

# ============================================================
# Test 12: 独自ソート — -N-suffixパターン
# ============================================================
printf '\n## Test 12: Independent sort — -N-suffix pattern\n'
TEST_DIR="$TEST_TMP/verify_order_12"
mkdir -p "$TEST_DIR"
touch "$TEST_DIR/xxx-2-enc.mp4" "$TEST_DIR/xxx-1-enc.mp4" "$TEST_DIR/output.mp4"

MOCK_DURATIONS=()
MOCK_DURATIONS[$TEST_DIR/xxx-1-enc.mp4]="10.0"
MOCK_DURATIONS[$TEST_DIR/xxx-2-enc.mp4]="10.0"

MOCK_FRAME_HASHES=()
MOCK_FRAME_HASHES[$TEST_DIR/xxx-1-enc.mp4:3.000]="hash_x1"
MOCK_FRAME_HASHES[$TEST_DIR/output.mp4:3.000]="hash_x1"
MOCK_FRAME_HASHES[$TEST_DIR/xxx-2-enc.mp4:3.000]="hash_x2"
MOCK_FRAME_HASHES[$TEST_DIR/output.mp4:13.000]="hash_x2"

# 逆順で渡す
unsetopt err_exit
__concat_verify_frame_order "$TEST_DIR/output.mp4" "$TEST_DIR/xxx-2-enc.mp4" "$TEST_DIR/xxx-1-enc.mp4"
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "-N-suffix pattern sorted correctly"

printf '\n=== Frame Order Verification Tests Completed ===\n'
