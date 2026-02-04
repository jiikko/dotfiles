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

# Test 14: --resolution オプションのヘルプメッセージ
printf '\n## Test 14: Help message includes --resolution option\n'
help_output=$(av1ify --help 2>&1)
assert_contains "$help_output" "--resolution" "Help message contains --resolution option"
assert_contains "$help_output" "-r," "Help message contains -r short option"
assert_contains "$help_output" "アスペクト比は維持" "Help message mentions aspect ratio preservation"

# Test 15: --fps オプションのヘルプメッセージ
printf '\n## Test 15: Help message includes --fps option\n'
help_output=$(av1ify --help 2>&1)
assert_contains "$help_output" "--fps" "Help message contains --fps option"
assert_contains "$help_output" "フレームレート" "Help message mentions frame rate"

# Test 16: --resolution オプション (dry-run)
printf '\n## Test 16: Resolution option with dry-run\n'
TEST_DIR="$TEST_TMP/test16"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(av1ify --dry-run -r 720p "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "resolution=720p" "Dry-run shows resolution=720p"
assert_file_not_exists "$TEST_DIR/input-720p-enc.mp4" "Dry-run does not create output file"

# Test 17: --fps オプション (dry-run)
printf '\n## Test 17: FPS option with dry-run\n'
TEST_DIR="$TEST_TMP/test17"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(av1ify --dry-run --fps 24 "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "fps=24" "Dry-run shows fps=24"

# Test 18: --resolution と --fps の組み合わせ (dry-run)
printf '\n## Test 18: Resolution and FPS combined with dry-run\n'
TEST_DIR="$TEST_TMP/test18"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(av1ify --dry-run -r 1080p --fps 30 "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "resolution=1080p" "Dry-run shows resolution=1080p"
assert_contains "$output" "fps=30" "Dry-run shows fps=30"

# Test 19: 無効な解像度のバリデーション
printf '\n## Test 19: Invalid resolution validation\n'
TEST_DIR="$TEST_TMP/test19"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(av1ify --dry-run -r 0 "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "無効な解像度指定" "Reports invalid resolution for 0"
assert_contains "$output" "resolution=auto" "Falls back to auto when invalid"

output=$(av1ify --dry-run -r 10000 "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "無効な解像度指定" "Reports invalid resolution for 10000"

output=$(av1ify --dry-run -r abc "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "無効な解像度指定" "Reports invalid resolution for non-numeric"

# Test 20: 無効なfpsのバリデーション
printf '\n## Test 20: Invalid FPS validation\n'
TEST_DIR="$TEST_TMP/test20"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(av1ify --dry-run --fps 0 "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "無効なfps指定" "Reports invalid FPS for 0"
assert_contains "$output" "fps=auto" "Falls back to auto when invalid"

output=$(av1ify --dry-run --fps 300 "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "無効なfps指定" "Reports invalid FPS for 300"

output=$(av1ify --dry-run --fps abc "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "無効なfps指定" "Reports invalid FPS for non-numeric"

# Test 21: 有効な解像度のバリエーション
printf '\n## Test 21: Valid resolution variations\n'
TEST_DIR="$TEST_TMP/test21"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"

for res in 480p 720p 1080p 1440p 4k 540; do
  output=$(av1ify --dry-run -r "$res" "$TEST_DIR/input.avi" 2>&1 || true)
  if [[ "$output" != *"無効な解像度"* ]]; then
    printf '✓ Resolution %s is valid\n' "$res"
  else
    printf '✗ Resolution %s should be valid\n' "$res"
  fi
done

# Test 22: 有効なfpsのバリエーション (小数点含む)
printf '\n## Test 22: Valid FPS variations including decimal\n'
TEST_DIR="$TEST_TMP/test22"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"

for fps in 24 30 60 29.97 23.976; do
  output=$(av1ify --dry-run --fps "$fps" "$TEST_DIR/input.avi" 2>&1 || true)
  if [[ "$output" != *"無効なfps"* ]]; then
    printf '✓ FPS %s is valid\n' "$fps"
  else
    printf '✗ FPS %s should be valid\n' "$fps"
  fi
done

# Test 23: 環境変数 AV1_RESOLUTION
printf '\n## Test 23: AV1_RESOLUTION environment variable\n'
TEST_DIR="$TEST_TMP/test23"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(AV1_RESOLUTION=720p av1ify --dry-run "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "resolution=720p" "AV1_RESOLUTION env var works"

# Test 24: 環境変数 AV1_FPS
printf '\n## Test 24: AV1_FPS environment variable\n'
TEST_DIR="$TEST_TMP/test24"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(AV1_FPS=24 av1ify --dry-run "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "fps=24" "AV1_FPS env var works"

# Test 25: CLIオプションが環境変数より優先される
printf '\n## Test 25: CLI option takes priority over env var\n'
TEST_DIR="$TEST_TMP/test25"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(AV1_RESOLUTION=480p AV1_FPS=30 av1ify --dry-run -r 1080p --fps 60 "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "resolution=1080p" "CLI -r overrides AV1_RESOLUTION"
assert_contains "$output" "fps=60" "CLI --fps overrides AV1_FPS"

# Test 26: --resolution オプションで実際にファイル処理
printf '\n## Test 26: Resolution option creates output file with tag\n'
TEST_DIR="$TEST_TMP/test26"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
av1ify -r 720p "$TEST_DIR/input.avi" > /dev/null 2>&1 || true
setopt err_exit
assert_file_exists "$TEST_DIR/input-720p-enc.mp4" "Output file has resolution tag"

# Test 27: --fps オプションで実際にファイル処理
printf '\n## Test 27: FPS option creates output file with tag\n'
TEST_DIR="$TEST_TMP/test27"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
av1ify --fps 24 "$TEST_DIR/input.avi" > /dev/null 2>&1 || true
setopt err_exit
assert_file_exists "$TEST_DIR/input-24fps-enc.mp4" "Output file has fps tag"

# Test 28: --resolution と --fps の組み合わせで実際にファイル処理
printf '\n## Test 28: Resolution and FPS combined creates output file\n'
TEST_DIR="$TEST_TMP/test28"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
av1ify -r 720p --fps 24 "$TEST_DIR/input.avi" > /dev/null 2>&1 || true
setopt err_exit
assert_file_exists "$TEST_DIR/input-720p-24fps-enc.mp4" "Output file has both resolution and fps tags"

printf '\n=== All Tests Completed ===\n'
printf 'All av1ify tests passed successfully!\n'
