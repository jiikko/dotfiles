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

# Test 5: 修復は常に max timescale (高分解能側) を target にする
# 旧実装は「先頭ファイル基準」だったため、低分解能ファイルが先頭にあると
# 高分解能ファイルを低分解能側に丸めようとして PTS 誤差が発生する穴があった。
printf '\n## Test 5: Repair always targets max timescale (90000), regardless of file order\n'
TEST_DIR="$TEST_TMP/tb5"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/tbase_001.mp4"  # mock: 1/90000
echo "video 2" > "$TEST_DIR/tbase_002.mp4"  # mock: 1/30000
cd "$TEST_DIR"

# 順方向: _001 (90000) → _002 (30000)。修復対象は _002 で target=90000
unsetopt err_exit
output=$(concat "$TEST_DIR/tbase_001.mp4" "$TEST_DIR/tbase_002.mp4" 2>&1)
setopt err_exit
assert_contains "$output" "repair-mp4-timebase 90000" "Forward order: target is 90000 (max)"
assert_contains "$output" "tbase_002.mp4" "Forward order: lists 30000-side file as repair target"

# 逆方向: _002 (30000) → _001 (90000)。先頭基準なら 30000 を提案するはずだが、
# 新実装では max=90000 が選ばれ、修復対象は _002 のままになる
unsetopt err_exit
output=$(concat "$TEST_DIR/tbase_002.mp4" "$TEST_DIR/tbase_001.mp4" 2>&1)
setopt err_exit
assert_contains "$output" "repair-mp4-timebase 90000" "Reverse order: target is still 90000 (max), not 30000"
assert_contains "$output" "tbase_002.mp4" "Reverse order: lists 30000-side file as repair target"

printf '\n=== time_base Tests Completed ===\n'
