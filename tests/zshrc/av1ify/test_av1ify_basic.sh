#!/usr/bin/env zsh
# shellcheck shell=bash
# av1ify 基本テスト (Test 1-12)
# ヘルプ、単一ファイル、スキップ、ディレクトリ、複数ファイル、-f オプション

source "${0:A:h}/test_helper.sh"

printf '\n=== av1ify Basic Tests (1-12) ===\n\n'

# Test 1: ヘルプメッセージの表示
printf '## Test 1: Help message display\n'
help_output=$(av1ify --help 2>&1)
assert_contains "$help_output" "av1ify" "Help message contains command name"
assert_contains "$help_output" "使い方" "Help message is in Japanese"
assert_contains "$help_output" "複数のファイルを順番に変換" "Help message mentions multiple file support"

# Test 1b: ドライラン表示
printf '\n## Test 1b: Dry-run option\n'
TEST_DIR="$TEST_TMP/test1b"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(av1ify "$TEST_DIR/input.avi" --dry-run 2>&1 || true)
assert_file_not_exists "$TEST_DIR/input-enc.mp4" "Dry-run does not create output file"
assert_contains "$output" "DRY-RUN" "Dry-run output contains marker"

# Test 2: 単一ファイルの処理
printf '\n## Test 2: Single file processing\n'
TEST_DIR="$TEST_TMP/test2"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
av1ify "$TEST_DIR/input.avi" > /dev/null 2>&1
assert_file_exists "$TEST_DIR/input-enc.mp4" "Output file is created with -enc.mp4 suffix"

# Test 3: 既存の-encファイルのスキップ
printf '\n## Test 3: Skip existing -enc files\n'
TEST_DIR="$TEST_TMP/test3"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/video.avi"
echo "already encoded" > "$TEST_DIR/video-enc.mp4"
cd "$TEST_DIR"
output=$(av1ify "$TEST_DIR/video.avi" 2>&1)
assert_contains "$output" "SKIP" "Skips when output file already exists"

# Test 4: -encファイル自体のスキップ
printf '\n## Test 4: Skip -enc.mp4 input files\n'
TEST_DIR="$TEST_TMP/test4"
mkdir -p "$TEST_DIR"
echo "already encoded" > "$TEST_DIR/video-enc.mp4"
cd "$TEST_DIR"
output=$(av1ify "$TEST_DIR/video-enc.mp4" 2>&1)
assert_contains "$output" "SKIP" "Skips -enc.mp4 input files"

# Test 5: -encoded.* ファイルのスキップ
printf '\n## Test 5: Skip -encoded.* input files\n'
TEST_DIR="$TEST_TMP/test5"
mkdir -p "$TEST_DIR"
echo "already encoded" > "$TEST_DIR/gachi625_hd縦ロール.mp4-encoded.mp4"
cd "$TEST_DIR"
output=$(av1ify "$TEST_DIR/gachi625_hd縦ロール.mp4-encoded.mp4" 2>&1)
assert_contains "$output" "SKIP" "Skips -encoded.* input files"

# Test 6: 存在しないファイルのエラー処理
printf '\n## Test 6: Error handling for non-existent file\n'
TEST_DIR="$TEST_TMP/test6"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"
output=$(av1ify "$TEST_DIR/nonexistent.avi" 2>&1 || true)
assert_contains "$output" "ファイルが無い" "Reports error for non-existent file"

# Test 7: 空の引数でヘルプ表示（スキップ - 環境依存）
printf '\n## Test 7: Help display with empty argument (SKIPPED)\n'
printf '↷ Skipped due to environment-specific behavior\n'

# Test 8: ディレクトリの再帰処理
printf '\n## Test 8: Directory recursive processing\n'
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
if [[ "$output" == *"ファイルパスが必要"* ]]; then
  printf '✓ Reports error when -f has no argument\n'
else
  printf '✗ Reports error when -f has no argument (output: %s)\n' "$output"
fi

printf '\n=== Basic Tests Completed ===\n'
