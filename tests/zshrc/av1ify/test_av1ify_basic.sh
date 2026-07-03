#!/usr/bin/env zsh
unset CDPATH
# shellcheck shell=bash
# av1ify 基本テスト (Test 1-12)
# ヘルプ、単一ファイル、スキップ、ディレクトリ、複数ファイル、-f オプション

source "${0:A:h}/test_helper.sh"

printf '\n=== av1ify Basic Tests (1-12) ===\n\n'

# Test 1: ヘルプメッセージの表示
# 全オプションのヘルプ剥がれ検出もここに集約する (--help 1 回で全 assert)。
# 新オプション追加時はこのリストにも 1 行足すこと。
printf '## Test 1: Help message display\n'
help_output=$(av1ify --help 2>&1)
assert_contains "$help_output" "av1ify" "Help message contains command name"
assert_contains "$help_output" "使い方" "Help message is in Japanese"
assert_contains "$help_output" "複数のファイルを順番に変換" "Help message mentions multiple file support"
assert_contains "$help_output" "-f <ファイル>" "Help lists -f option"
assert_contains "$help_output" "-r, --resolution" "Help lists --resolution option"
assert_contains "$help_output" "--fps" "Help lists --fps option"
assert_contains "$help_output" "-c, --compact" "Help lists --compact option"
assert_contains "$help_output" "--denoise" "Help lists --denoise option"
assert_contains "$help_output" "--force" "Help lists --force option"
assert_contains "$help_output" "--delete-origin-if-success-and-no-ng" "Help lists --delete-origin option"
assert_contains "$help_output" "--no-delete-origin-if-success-and-no-ng" "Help lists --no-delete-origin variant"
assert_contains "$help_output" "-n, --dry-run" "Help lists --dry-run option"

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

# Test 10b: -f が先頭以外の位置でも機能し、位置引数と併用できる
# 回帰防止: 旧実装は positional 先頭の "-f" だけを処理していたため、
# `av1ify a.mp4 -f list.txt` が「-f というファイルが無い」NG になっていた
printf '\n## Test 10b: -f works in non-leading position combined with positional args\n'
TEST_DIR="$TEST_TMP/test10b"
mkdir -p "$TEST_DIR"
echo "video a" > "$TEST_DIR/posA.avi"
echo "video b" > "$TEST_DIR/listB.mkv"
cat > "$TEST_DIR/list.txt" <<LISTEOF
$TEST_DIR/listB.mkv
LISTEOF
cd "$TEST_DIR"
unsetopt err_exit
output=$(av1ify "$TEST_DIR/posA.avi" -f "$TEST_DIR/list.txt" 2>&1 || true)
setopt err_exit
assert_file_exists "$TEST_DIR/posA-enc.mp4" "Positional file is processed"
assert_file_exists "$TEST_DIR/listB-enc.mp4" "List file entry is processed"
assert_not_contains "$output" "ファイルが無い: -f" "-f is not treated as a file path"

# Test 10c: -f の空リスト (コメント/空行のみ) + 位置引数あり → 位置引数は処理される
# (「対象ファイルなし」で early return するのはリストも位置引数も空のときだけ)
printf '\n## Test 10c: Empty -f list with positional args still processes positionals\n'
TEST_DIR="$TEST_TMP/test10c"
mkdir -p "$TEST_DIR"
echo "video a" > "$TEST_DIR/posOnly.avi"
cat > "$TEST_DIR/empty_list.txt" <<LISTEOF
# コメントのみ

LISTEOF
cd "$TEST_DIR"
unsetopt err_exit
output=$(av1ify -f "$TEST_DIR/empty_list.txt" "$TEST_DIR/posOnly.avi" 2>&1 || true)
setopt err_exit
assert_file_exists "$TEST_DIR/posOnly-enc.mp4" "Positional file is processed despite empty list"
assert_not_contains "$output" "対象ファイルなし" "Does not early-return when positionals exist"

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

# Test 13: 成功時のサイズ削減サマリ表示 (元→出力, 削減率)
printf '\n## Test 13: Size reduction summary on success\n'
TEST_DIR="$TEST_TMP/test13"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"
head -c 1000 /dev/zero > "$TEST_DIR/input.avi"   # 元 1000 bytes
unsetopt err_exit
output=$(MOCK_FFMPEG_OUTPUT_SIZE=250 av1ify "$TEST_DIR/input.avi" 2>&1)   # 出力 250 bytes
setopt err_exit
assert_contains "$output" "ファイル取得中: $TEST_DIR/input.avi (1000 B)" "Shows source size at fetch start"
assert_contains "$output" "📉" "Shows size-down icon on reduction"
assert_contains "$output" "1000 B" "Shows source size"
assert_contains "$output" "250 B" "Shows output size"
assert_contains "$output" "(-75%)" "Shows reduction percentage"

# Test 14: __av1ify_format_size の人間可読フォーマット (境界値)
printf '\n## Test 14: __av1ify_format_size human-readable units\n'
assert_contains "$(__av1ify_format_size 512)" "512 B" "bytes under 1KB"
assert_contains "$(__av1ify_format_size 1536)" "1.5 KB" "1.5 KB"
assert_contains "$(__av1ify_format_size 1572864)" "1.5 MB" "1.5 MB"
assert_contains "$(__av1ify_format_size 1610612736)" "1.5 GB" "1.5 GB"

printf '\n=== Basic Tests Completed ===\n'
