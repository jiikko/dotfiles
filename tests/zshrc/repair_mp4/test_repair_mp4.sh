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

# ffmpegモックスクリプトを作成
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

# ffprobeモックスクリプトを作成（フレームレート対応版）
# MOCK_FPS環境変数で返すフレームレートを制御
cat > "$MOCK_BIN_DIR/ffprobe" <<'EOF'
#!/usr/bin/env sh
# どんなクエリでも成功を返す
if echo "$*" | grep -q "r_frame_rate"; then
  # MOCK_FPS環境変数でフレームレートを制御（デフォルト: 30/1）
  echo "${MOCK_FPS:-30/1}"
elif echo "$*" | grep -q "codec_name"; then
  echo "h264"
elif echo "$*" | grep -q "stream=index"; then
  echo "0"
elif echo "$*" | grep -q "duration"; then
  echo "10.0"
elif echo "$*" | grep -q "format_name"; then
  # MOCK_FORMAT環境変数でフォーマットを制御（デフォルト: mpegts）
  echo "${MOCK_FORMAT:-mpegts}"
fi
exit 0
EOF
chmod +x "$MOCK_BIN_DIR/ffprobe"

# PATH設定
export PATH="$MOCK_BIN_DIR:$PATH"

# repair_mp4をロード
source "$ROOT_DIR/zshlib/_repair_mp4.zsh"

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

assert_not_contains() {
  local haystack="$1"
  local needle="$2"
  local message="$3"
  if [[ "$haystack" != *"$needle"* ]]; then
    printf '✓ %s\n' "$message"
    return 0
  else
    printf '✗ %s (should not contain: %s)\n' "$message" "$needle"
    return 1
  fi
}

# テスト開始
printf '\n=== repair_mp4 Unit Tests ===\n\n'

# Test 1: ヘルプメッセージの表示
printf '## Test 1: Help message display\n'
help_output=$(repair_mp4 --help 2>&1)
assert_contains "$help_output" "repair_mp4" "Help message contains command name"
assert_contains "$help_output" "問題のあるコンテナ" "Help message describes purpose"
assert_contains "$help_output" "-repaired.mp4" "Help message mentions output format"

# Test 2: 単一ファイルの処理（正常なフレームレート）
printf '\n## Test 2: Single file processing with normal framerate\n'
TEST_DIR="$TEST_TMP/test2"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.mp4"
cd "$TEST_DIR"
export MOCK_FPS="30/1"
output=$(repair_mp4 "$TEST_DIR/input.mp4" 2>&1)
assert_file_exists "$TEST_DIR/input-repaired.mp4" "Output file is created with -repaired.mp4 suffix"
assert_contains "$output" "copy" "Uses stream copy for normal framerate"
assert_contains "$output" "30fps (正常)" "Reports normal framerate"

# Test 3: 異常なフレームレートの検出と正規化
printf '\n## Test 3: Abnormal framerate detection and normalization\n'
TEST_DIR="$TEST_TMP/test3"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/broken.mp4"
cd "$TEST_DIR"
export MOCK_FPS="90000/1"
output=$(repair_mp4 "$TEST_DIR/broken.mp4" 2>&1)
assert_file_exists "$TEST_DIR/broken-repaired.mp4" "Output file is created"
assert_contains "$output" "異常なフレームレート" "Detects abnormal framerate"
assert_contains "$output" "copy" "Uses stream copy for abnormal framerate"
assert_contains "$output" "30fpsに正規化" "Normalizes to 30fps"

# Test 4: 既存の-repairedファイルのスキップ
printf '\n## Test 4: Skip existing -repaired files\n'
TEST_DIR="$TEST_TMP/test4"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/video.mp4"
echo "already repaired" > "$TEST_DIR/video-repaired.mp4"
cd "$TEST_DIR"
export MOCK_FPS="30/1"
output=$(repair_mp4 "$TEST_DIR/video.mp4" 2>&1)
assert_contains "$output" "SKIP" "Skips when output file already exists"

# Test 5: 存在しないファイルのエラー処理
printf '\n## Test 5: Error handling for non-existent file\n'
TEST_DIR="$TEST_TMP/test5"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"
unsetopt err_exit
output=$(repair_mp4 "$TEST_DIR/nonexistent.mp4" 2>&1 || true)
setopt err_exit
assert_contains "$output" "ファイルが無い" "Reports error for non-existent file"

# Test 6: 空の引数でヘルプ表示
printf '\n## Test 6: Help display with empty argument\n'
unsetopt err_exit
help_output=$(repair_mp4 2>&1 || true)
setopt err_exit
assert_contains "$help_output" "repair_mp4" "Shows help when no arguments"

# Test 7: 複数ファイルの処理
printf '\n## Test 7: Multiple files processing\n'
TEST_DIR="$TEST_TMP/test7"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/file1.mp4"
echo "video 2" > "$TEST_DIR/file2.mp4"
echo "video 3" > "$TEST_DIR/file3.mp4"
cd "$TEST_DIR"
export MOCK_FPS="30/1"
unsetopt err_exit
repair_mp4 "$TEST_DIR/file1.mp4" "$TEST_DIR/file2.mp4" "$TEST_DIR/file3.mp4" > /dev/null 2>&1 || true
setopt err_exit
assert_file_exists "$TEST_DIR/file1-repaired.mp4" "First file is processed"
assert_file_exists "$TEST_DIR/file2-repaired.mp4" "Second file is processed"
assert_file_exists "$TEST_DIR/file3-repaired.mp4" "Third file is processed"

# Test 8: コンテナフォーマットの表示
printf '\n## Test 8: Container format display\n'
TEST_DIR="$TEST_TMP/test8"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.mp4"
cd "$TEST_DIR"
export MOCK_FPS="30/1"
export MOCK_FORMAT="mpegts"
output=$(repair_mp4 "$TEST_DIR/input.mp4" 2>&1)
assert_contains "$output" "入力フォーマット: mpegts" "Displays input format"

# Test 9: 環境変数 REPAIR_MP4_FPS によるカスタムFPS
printf '\n## Test 9: Custom FPS via REPAIR_MP4_FPS environment variable\n'
TEST_DIR="$TEST_TMP/test9"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.mp4"
cd "$TEST_DIR"
export MOCK_FPS="90000/1"
export REPAIR_MP4_FPS=60
output=$(repair_mp4 "$TEST_DIR/input.mp4" 2>&1)
assert_contains "$output" "60fpsに正規化" "Uses custom FPS from environment variable"
unset REPAIR_MP4_FPS

# Test 10: 240fps以下は正常として扱う
printf '\n## Test 10: Framerate <= 240 is treated as normal\n'
TEST_DIR="$TEST_TMP/test10"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.mp4"
cd "$TEST_DIR"
export MOCK_FPS="240/1"
output=$(repair_mp4 "$TEST_DIR/input.mp4" 2>&1)
assert_contains "$output" "240fps (正常)" "240fps is treated as normal"
assert_contains "$output" "copy" "Uses stream copy for 240fps"

# Test 11: 241fpsは異常として扱う
printf '\n## Test 11: Framerate > 240 is treated as abnormal\n'
TEST_DIR="$TEST_TMP/test11"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.mp4"
cd "$TEST_DIR"
export MOCK_FPS="241/1"
output=$(repair_mp4 "$TEST_DIR/input.mp4" 2>&1)
assert_contains "$output" "異常なフレームレート" "241fps is treated as abnormal"

# Test 12: ヘルプに環境変数の説明がある
printf '\n## Test 12: Help message includes environment variable\n'
help_output=$(repair_mp4 --help 2>&1)
assert_contains "$help_output" "REPAIR_MP4_FPS" "Help message contains REPAIR_MP4_FPS"

# Test 13: 修復不要なファイルのスキップ（MP4コンテナ + 正常なフレームレート）
printf '\n## Test 13: Skip files that do not need repair\n'
TEST_DIR="$TEST_TMP/test13"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/normal.mp4"
cd "$TEST_DIR"
export MOCK_FPS="30/1"
export MOCK_FORMAT="mov,mp4,m4a,3gp,3g2,mj2"
output=$(repair_mp4 "$TEST_DIR/normal.mp4" 2>&1)
assert_contains "$output" "修復不要" "Reports no repair needed for normal MP4"
assert_file_not_exists "$TEST_DIR/normal-repaired.mp4" "No output file created for normal MP4"

# Test 14: mpegtsコンテナは修復が必要（正常なフレームレートでも）
printf '\n## Test 14: mpegts container needs repair even with normal framerate\n'
TEST_DIR="$TEST_TMP/test14"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/video.mp4"
cd "$TEST_DIR"
export MOCK_FPS="30/1"
export MOCK_FORMAT="mpegts"
output=$(repair_mp4 "$TEST_DIR/video.mp4" 2>&1)
assert_file_exists "$TEST_DIR/video-repaired.mp4" "Output file created for mpegts container"
assert_not_contains "$output" "修復不要" "Does not report no repair needed for mpegts"

printf '\n=== All Tests Completed ===\n'
printf 'All repair_mp4 tests passed successfully!\n'
