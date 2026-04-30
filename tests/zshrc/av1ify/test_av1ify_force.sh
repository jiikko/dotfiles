#!/usr/bin/env zsh
# shellcheck shell=bash
# av1ify --force オプションテスト (Test 65-69)
# 健全性チェック失敗時の --force によるスキップ/続行動作

source "${0:A:h}/test_helper.sh"

printf '\n=== av1ify --force Tests (65-69) ===\n\n'

# Test 65: A/V末尾差は事前チェックで破損扱いしない（誤判定回帰テスト）
printf '## Test 65: A/V tail mismatch alone does NOT block encoding\n'
TEST_DIR="$TEST_TMP/test65"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# 音声duration=100.0, 映像duration=10.0 → A/V末尾差90秒でも事前チェックは通過し、エンコード成功するべき
output=$(MOCK_AUDIO_DURATION=100.0 av1ify "$TEST_DIR/input.avi" 2>&1)
exit_code=$?
setopt err_exit
assert_not_contains "$output" "破損しています" "A/V tail mismatch is not reported as corruption"
assert_not_contains "$output" "エンコードをスキップします" "Encoding is not skipped for A/V tail mismatch"
assert_file_exists "$TEST_DIR/input-enc.mp4" "Encoded output is created despite A/V tail mismatch"
(( exit_code == 0 )) && printf '✓ Exit code is 0 (encoding succeeded)\n' || { printf '✗ Exit code should be 0 (got %d)\n' "$exit_code"; false; }

# Test 66: --force ありの動作確認（fps異常で確認）
printf '\n## Test 66: --force with corruption (fps anomaly) continues encoding\n'
TEST_DIR="$TEST_TMP/test66"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_AVG_FPS="10000/1001" av1ify --force "$TEST_DIR/input.avi" 2>&1)
setopt err_exit
assert_contains "$output" "--force で続行" "Shows force continuation warning"
assert_not_contains "$output" "スキップします" "Does not skip encoding"

# Test 67: --force ヘルプメッセージ
printf '\n## Test 67: Help message includes --force option\n'
help_output=$(av1ify --help 2>&1)
assert_contains "$help_output" "--force" "Help message contains --force option"

# Test 68: フレームレート異常で --force なし → スキップ
printf '\n## Test 68: Frame rate anomaly without --force skips encoding\n'
TEST_DIR="$TEST_TMP/test68"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
# avg_fps=10fps vs r_fps=30fps → 比率0.33でNG
output=$(MOCK_AVG_FPS="10000/1001" av1ify "$TEST_DIR/input.avi" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "破損しています" "Reports corruption for fps anomaly"
assert_file_not_exists "$TEST_DIR/input-enc.mp4" "No output file created"

# Test 69: フレームレート異常で --force あり → 続行
printf '\n## Test 69: Frame rate anomaly with --force continues\n'
TEST_DIR="$TEST_TMP/test69"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_AVG_FPS="10000/1001" av1ify --force "$TEST_DIR/input.avi" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "--force で続行" "Shows force continuation for fps anomaly"

printf '\n=== --force Tests Completed ===\n'
