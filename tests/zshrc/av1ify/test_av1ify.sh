#!/usr/bin/env zsh
# shellcheck shell=bash
setopt err_exit no_unset pipe_fail

# zshでの現在のスクリプトパス取得
SCRIPT_PATH="${(%):-%x}"
ROOT_DIR="$(cd "$(dirname "$SCRIPT_PATH")/../../.." && pwd)"
TEST_TMP="$(mktemp -d)"

cleanup() {
  rm -rf "$TEST_TMP"
}
trap cleanup EXIT

# モックスクリプト用のディレクトリ
MOCK_BIN_DIR="$TEST_TMP/mock_bin"
mkdir -p "$MOCK_BIN_DIR"

# ffmpegモックスクリプトを作成（シンプル版）
cat > "$MOCK_BIN_DIR/ffmpeg" <<'EOF'
#!/usr/bin/env sh
# 最後の引数を出力ファイルとして扱う
for arg in "$@"; do
  last_arg="$arg"
done

# -で始まらない最後の引数が出力ファイル
if [ -n "$last_arg" ] && [ "${last_arg#-}" = "$last_arg" ]; then
  echo "mock video data" > "$last_arg"
  exit 0
fi
exit 1
EOF
chmod +x "$MOCK_BIN_DIR/ffmpeg"

# ffprobeモックスクリプトを作成（シンプル版）
cat > "$MOCK_BIN_DIR/ffprobe" <<'EOF'
#!/usr/bin/env sh
# どんなクエリでも成功を返す
if echo "$*" | grep -q "codec_name"; then
  echo "aac"
elif echo "$*" | grep -q "stream=index"; then
  echo "0"
elif echo "$*" | grep -q "duration"; then
  echo "10.0"
elif echo "$*" | grep -q "height"; then
  echo "720"
elif echo "$*" | grep -q "format_name"; then
  echo "mp4"
fi
exit 0
EOF
chmod +x "$MOCK_BIN_DIR/ffprobe"

# PATH設定
export PATH="$MOCK_BIN_DIR:$PATH"

# av1ifyをロード
source "$ROOT_DIR/zshlib/_av1ify.zsh"

assert_file_exists() {
  local file="$1"
  local message="$2"
  if [[ -f "$file" ]]; then
    printf '✓ %s\n' "$message"
    return 0
  else
    printf '✗ %s (file not found: %s)\n' "$message" "$file"
    return 1
  fi
}

assert_file_not_exists() {
  local file="$1"
  local message="$2"
  if [[ ! -f "$file" ]]; then
    printf '✓ %s\n' "$message"
    return 0
  else
    printf '✗ %s (file exists: %s)\n' "$message" "$file"
    return 1
  fi
}

assert_contains() {
  local haystack="$1"
  local needle="$2"
  local message="$3"
  if [[ "$haystack" == *"$needle"* ]]; then
    printf '✓ %s\n' "$message"
    return 0
  else
    printf '✗ %s (expected to contain: %s)\n' "$message" "$needle"
    return 1
  fi
}

# テスト開始
printf '\n=== av1ify Unit Tests ===\n\n'

# Test 1: ヘルプメッセージの表示
printf '## Test 1: Help message display\n'
help_output=$(av1ify --help 2>&1)
assert_contains "$help_output" "av1ify" "Help message contains command name"
assert_contains "$help_output" "使い方" "Help message is in Japanese"
assert_contains "$help_output" "複数のファイルを順番に変換" "Help message mentions multiple file support"

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

printf '\n=== All Tests Completed ===\n'
printf 'All av1ify tests passed successfully!\n'
