#!/usr/bin/env zsh
# shellcheck shell=bash
# concat time_base不一致検出テスト

source "${0:A:h}/test_helper.sh"

printf '\n=== concat time_base Tests ===\n\n'

# Test 1: time_base不一致でエラー
printf '## Test 1: time_base mismatch error\n'
TEST_DIR="$TEST_TMP/tb1"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/tbase_001.mp4"
echo "video 2" > "$TEST_DIR/tbase_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/tbase_001.mp4" "$TEST_DIR/tbase_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "1" "$exit_code" "Returns exit code 1 for time_base mismatch"
assert_contains "$output" "time_base不一致" "Reports time_base mismatch error"
assert_contains "$output" "修復方法" "Shows repair instructions"
assert_contains "$output" "repair-mp4-timebase" "Shows repair-mp4-timebase command"

# Test 2: time_base不一致でも--forceで無視
printf '\n## Test 2: --force ignores time_base mismatch\n'
TEST_DIR="$TEST_TMP/tb2"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/tbase_001.mp4"
echo "video 2" > "$TEST_DIR/tbase_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat --force "$TEST_DIR/tbase_001.mp4" "$TEST_DIR/tbase_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "完了" "--force allows concat despite time_base mismatch"

# Test 3: time_base一致なら正常結合
printf '\n## Test 3: Matching time_base succeeds\n'
TEST_DIR="$TEST_TMP/tb3"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/normal_001.mp4"
echo "video 2" > "$TEST_DIR/normal_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/normal_001.mp4" "$TEST_DIR/normal_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Returns exit code 0 for matching time_base"
assert_file_exists "$TEST_DIR/normal.mp4" "Output file is created"

# Test 4: 出力ファイルが残っていないことを確認（time_base不一致時）
printf '\n## Test 4: No output file on time_base mismatch\n'
TEST_DIR="$TEST_TMP/tb4"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/tbase_001.mp4"
echo "video 2" > "$TEST_DIR/tbase_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/tbase_001.mp4" "$TEST_DIR/tbase_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_file_not_exists "$TEST_DIR/tbase.mp4" "No output file created on time_base mismatch"

printf '\n=== time_base Tests Completed ===\n'
