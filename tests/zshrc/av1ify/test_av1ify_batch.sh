#!/usr/bin/env zsh
# shellcheck shell=bash
SCRIPT_PATH="${(%):-%x}"
source "$(dirname "$SCRIPT_PATH")/test_helper.sh"

# テスト開始
printf '\n=== av1ify Batch Tests (8-13) ===\n\n'

# Test 8: ディレクトリの再帰処理
printf '## Test 8: Directory recursive processing\n'
TEST_DIR="$TEST_TMP/test8"
mkdir -p "$TEST_DIR/subdir"
echo "video 1" > "$TEST_DIR/video1.avi"
echo "video 2" > "$TEST_DIR/subdir/video2.mkv"
echo "not a video" > "$TEST_DIR/readme.txt"
cd "$TEST_DIR"
unsetopt err_exit
av1ify "$TEST_DIR" > /dev/null 2>&1 || true
setopt err_exit
assert_file_exists "$TEST_DIR/video1-enc.mp4" "Top-level video is processed"
assert_file_exists "$TEST_DIR/subdir/video2-enc.mp4" "Subdirectory video is processed"
assert_file_not_exists "$TEST_DIR/readme-enc.mp4" "Non-video files are not processed"

# Test 9: 複数ファイルの処理（新機能）
printf '\n## Test 9: Multiple files processing\n'
TEST_DIR="$TEST_TMP/test9"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/file1.avi"
echo "video 2" > "$TEST_DIR/file2.mkv"
echo "video 3" > "$TEST_DIR/file3.wmv"
cd "$TEST_DIR"
unsetopt err_exit
output=$(av1ify "$TEST_DIR/file1.avi" "$TEST_DIR/file2.mkv" "$TEST_DIR/file3.wmv" 2>&1 || true)
setopt err_exit
assert_file_exists "$TEST_DIR/file1-enc.mp4" "First file is processed"
assert_file_exists "$TEST_DIR/file2-enc.mp4" "Second file is processed"
assert_file_exists "$TEST_DIR/file3-enc.mp4" "Third file is processed"
assert_contains "$output" "サマリ" "Summary is displayed for multiple files"

# Test 10: -f オプションでファイルリストから処理
printf '\n## Test 10: Processing from file list with -f option\n'
TEST_DIR="$TEST_TMP/test10"
mkdir -p "$TEST_DIR"
echo "video a" > "$TEST_DIR/videoA.avi"
echo "video b" > "$TEST_DIR/videoB.mkv"
echo "video c" > "$TEST_DIR/videoC.wmv"

# リストファイルを作成
cat > "$TEST_DIR/list.txt" <<LISTEOF
$TEST_DIR/videoA.avi
$TEST_DIR/videoB.mkv
# これはコメント
$TEST_DIR/videoC.wmv

LISTEOF

cd "$TEST_DIR"
unsetopt err_exit
output=$(av1ify -f "$TEST_DIR/list.txt" 2>&1 || true)
setopt err_exit
assert_file_exists "$TEST_DIR/videoA-enc.mp4" "File from list line 1 is processed"
assert_file_exists "$TEST_DIR/videoB-enc.mp4" "File from list line 2 is processed"
assert_file_exists "$TEST_DIR/videoC-enc.mp4" "File from list line 4 is processed"
assert_contains "$output" "サマリ" "Summary is displayed for file list"

# Test 11: -f オプションでファイルが見つからない場合
printf '\n## Test 11: Error when -f list file not found\n'
TEST_DIR="$TEST_TMP/test11"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"
unsetopt err_exit
output=$(av1ify -f "$TEST_DIR/nonexistent.txt" 2>&1 || true)
setopt err_exit
assert_contains "$output" "ファイルが見つかりません" "Reports error when list file not found"

# Test 12: -f オプションで引数なし
printf '\n## Test 12: Error when -f has no argument\n'
TEST_DIR="$TEST_TMP/test12"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"
unsetopt err_exit
output=$(av1ify -f 2>&1 || true)
setopt err_exit
# デバッグ用に出力を表示
# echo "Debug output: '$output'" >&2
if [[ "$output" == *"ファイルパスが必要"* ]]; then
  printf '✓ Reports error when -f has no argument\n'
else
  printf '✗ Reports error when -f has no argument (output: %s)\n' "$output"
fi

# Test 13: -f オプションのヘルプメッセージ
printf '\n## Test 13: Help message includes -f option\n'
help_output=$(av1ify --help 2>&1)
assert_contains "$help_output" "-f" "Help message contains -f option"
assert_contains "$help_output" "ファイルリスト" "Help message describes file list feature"

printf '\n=== av1ify Batch Tests Completed ===\n'
