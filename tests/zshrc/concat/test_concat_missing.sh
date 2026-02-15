#!/usr/bin/env zsh
# shellcheck shell=bash
# concat ディレクトリ内欠落チェックテスト (Test 24-29)

source "${0:A:h}/test_helper.sh"

printf '\n=== concat Missing File Tests (24-29) ===\n\n'

# Test 24: ディレクトリ内の同一パターンファイル欠落チェック
printf '## Test 24: Error when directory has matching files not passed\n'
TEST_DIR="$TEST_TMP/test24"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/video_001.mp4"
echo "video 2" > "$TEST_DIR/video_002.mp4"
echo "video 3" > "$TEST_DIR/video_003.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/video_001.mp4" "$TEST_DIR/video_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "1" "$exit_code" "Returns error when matching file is missing from args"
assert_contains "$output" "同じパターンのファイルが指定されていません" "Reports missing pattern files"
assert_contains "$output" "video_003.mp4" "Lists the missing file"

# Test 25: 全ファイル渡せばエラーにならない
printf '\n## Test 25: No error when all matching files are passed\n'
TEST_DIR="$TEST_TMP/test25"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/video_001.mp4"
echo "video 2" > "$TEST_DIR/video_002.mp4"
echo "video 3" > "$TEST_DIR/video_003.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/video_001.mp4" "$TEST_DIR/video_002.mp4" "$TEST_DIR/video_003.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Succeeds when all matching files are passed"
assert_contains "$output" "完了" "Reports success"

# Test 26: 異なるパターンのファイルは検出しない
printf '\n## Test 26: Different pattern files are not flagged\n'
TEST_DIR="$TEST_TMP/test26"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/video_001.mp4"
echo "video 2" > "$TEST_DIR/video_002.mp4"
echo "other" > "$TEST_DIR/other_file.mp4"
echo "clip" > "$TEST_DIR/clip_001.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/video_001.mp4" "$TEST_DIR/video_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Different pattern files don't cause error"
assert_contains "$output" "完了" "Reports success ignoring unrelated files"

# Test 27: -NNNパターンでの欠落チェック（連番は連続、ディレクトリに追加ファイル）
printf '\n## Test 27: Missing file detection with -NNN pattern\n'
TEST_DIR="$TEST_TMP/test27"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/clip-01.mp4"
echo "video 2" > "$TEST_DIR/clip-02.mp4"
echo "video 3" > "$TEST_DIR/clip-03.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/clip-01.mp4" "$TEST_DIR/clip-02.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "1" "$exit_code" "Error for missing -NNN pattern file"
assert_contains "$output" "同じパターンのファイルが指定されていません" "Reports missing -NNN pattern files"
assert_contains "$output" "clip-03.mp4" "Lists the missing -NNN file"

# Test 28: (N)パターンでの欠落チェック
printf '\n## Test 28: Missing file detection with (N) pattern\n'
TEST_DIR="$TEST_TMP/test28"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/movie(1).mp4"
echo "video 2" > "$TEST_DIR/movie(2).mp4"
echo "video 3" > "$TEST_DIR/movie(3).mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/movie(1).mp4" "$TEST_DIR/movie(2).mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "1" "$exit_code" "Error for missing (N) pattern file"
assert_contains "$output" "movie(3).mp4" "Lists the missing (N) file"

# Test 29: partNパターンでの欠落チェック
printf '\n## Test 29: Missing file detection with partN pattern\n'
TEST_DIR="$TEST_TMP/test29"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/file_part1.mp4"
echo "video 2" > "$TEST_DIR/file_part2.mp4"
echo "video 3" > "$TEST_DIR/file_part3.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/file_part1.mp4" "$TEST_DIR/file_part2.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "1" "$exit_code" "Error for missing partN pattern file"
assert_contains "$output" "file_part3.mp4" "Lists the missing partN file"

printf '\n=== Missing File Tests Completed ===\n'
