#!/usr/bin/env zsh
unset CDPATH
# shellcheck shell=bash
# concat 境界デコード検証テスト (issue 143 再現 2: extradata 不一致は入力属性では見えない)
# 出力を境界でデコードしてエラーが出たら失敗にし、出力を残さないこと。

source "${0:A:h}/test_helper.sh"

printf '\n=== concat boundary decode Tests ===\n\n'

# Test 1: デコードエラーがあれば失敗し、出力を消し、元ファイルを残す
printf '## Test 1: decode error at boundary fails concat\n'
TEST_DIR="$TEST_TMP/dec1"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/scene_001.mp4"
echo "video 2" > "$TEST_DIR/scene_002.mp4"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(MOCK_FFMPEG_DECODE_STDERR="[hevc] The cu_qp_delta 27 is outside the valid range [-26, 25]." \
  concat "$TEST_DIR/scene_001.mp4" "$TEST_DIR/scene_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "1" "$exit_code" "Returns exit code 1 on decode error"
assert_contains "$output" "デコードエラー" "Reports decode error"
assert_contains "$output" "cu_qp_delta" "Includes ffmpeg stderr in the message"
assert_contains "$output" "scene_001.mp4 の直後" "Names the boundary (after which segment)"
assert_file_not_exists "$TEST_DIR/scene.mp4" "Broken output is removed"
assert_file_exists "$TEST_DIR/scene_001.mp4" "Original file 1 is kept"
assert_file_exists "$TEST_DIR/scene_002.mp4" "Original file 2 is kept"
assert_not_contains "$output" "✅ 完了" "Does not claim success"

# Test 2: --force はデコード検証もスキップ (他の検証と同じくユーザー判断)
printf '\n## Test 2: --force skips decode check\n'
TEST_DIR="$TEST_TMP/dec2"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/scene_001.mp4"
echo "video 2" > "$TEST_DIR/scene_002.mp4"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(MOCK_FFMPEG_DECODE_STDERR="broken" concat --force "$TEST_DIR/scene_001.mp4" "$TEST_DIR/scene_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "--force succeeds even if decode would fail"
assert_file_exists "$TEST_DIR/scene.mp4" "Output kept under --force"

# Test 3: 正常 (stderr 空・rc 0) なら通る
printf '\n## Test 3: clean decode passes\n'
TEST_DIR="$TEST_TMP/dec3"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/scene_001.mp4"
echo "video 2" > "$TEST_DIR/scene_002.mp4"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(concat "$TEST_DIR/scene_001.mp4" "$TEST_DIR/scene_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Returns 0 when boundary decode is clean"
assert_file_exists "$TEST_DIR/scene.mp4" "Output file is created"

printf '\n=== concat boundary decode Tests Completed ===\n'
