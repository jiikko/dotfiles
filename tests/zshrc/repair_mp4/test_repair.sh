#!/usr/bin/env zsh
# shellcheck shell=bash
setopt err_exit no_unset pipe_fail

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

# ffmpegモック
cat > "$MOCK_BIN_DIR/ffmpeg" <<'EOF'
#!/usr/bin/env sh
for arg in "$@"; do
  last_arg="$arg"
done
if [ -n "$last_arg" ] && [ "${last_arg#-}" = "$last_arg" ]; then
  echo "mock video data" > "$last_arg"
  exit 0
fi
exit 1
EOF
chmod +x "$MOCK_BIN_DIR/ffmpeg"

# ffprobeモック
cat > "$MOCK_BIN_DIR/ffprobe" <<'EOF'
#!/usr/bin/env sh
if echo "$*" | grep -q "r_frame_rate"; then
  echo "${MOCK_FPS:-30/1}"
elif echo "$*" | grep -q "codec_name"; then
  echo "h264"
elif echo "$*" | grep -q "format_name"; then
  echo "${MOCK_FORMAT:-mpegts}"
fi
exit 0
EOF
chmod +x "$MOCK_BIN_DIR/ffprobe"

export PATH="$MOCK_BIN_DIR:$PATH"

source "$ROOT_DIR/zshlib/_repair.zsh"

assert_file_exists() {
  local file="$1" message="$2"
  if [[ -f "$file" ]]; then
    printf '✓ %s\n' "$message"
  else
    printf '✗ %s (file not found: %s)\n' "$message" "$file"
    return 1
  fi
}

assert_contains() {
  local haystack="$1" needle="$2" message="$3"
  if [[ "$haystack" == *"$needle"* ]]; then
    printf '✓ %s\n' "$message"
  else
    printf '✗ %s (expected: %s)\n' "$message" "$needle"
    return 1
  fi
}

printf '\n=== repair Unit Tests ===\n\n'

# Test 1: ヘルプ表示
printf '## Test 1: Help message\n'
unsetopt err_exit
help_output=$(repair --help 2>&1 || true)
setopt err_exit
assert_contains "$help_output" "repair" "Help contains command name"
assert_contains "$help_output" "対応形式" "Help lists supported formats"

# Test 2: mp4ファイル
printf '\n## Test 2: .mp4 file\n'
TEST_DIR="$TEST_TMP/test2"
mkdir -p "$TEST_DIR"
echo "dummy" > "$TEST_DIR/video.mp4"
export MOCK_FPS="30/1"
repair "$TEST_DIR/video.mp4" > /dev/null 2>&1
assert_file_exists "$TEST_DIR/video-repaired.mp4" ".mp4 processed"

# Test 3: tsファイル
printf '\n## Test 3: .ts file\n'
TEST_DIR="$TEST_TMP/test3"
mkdir -p "$TEST_DIR"
echo "dummy" > "$TEST_DIR/video.ts"
repair "$TEST_DIR/video.ts" > /dev/null 2>&1
assert_file_exists "$TEST_DIR/video-repaired.mp4" ".ts processed"

# Test 4: movファイル
printf '\n## Test 4: .mov file\n'
TEST_DIR="$TEST_TMP/test4"
mkdir -p "$TEST_DIR"
echo "dummy" > "$TEST_DIR/video.mov"
repair "$TEST_DIR/video.mov" > /dev/null 2>&1
assert_file_exists "$TEST_DIR/video-repaired.mp4" ".mov processed"

# Test 5: 未対応形式
printf '\n## Test 5: Unsupported format\n'
TEST_DIR="$TEST_TMP/test5"
mkdir -p "$TEST_DIR"
echo "dummy" > "$TEST_DIR/video.avi"
unsetopt err_exit
output=$(repair "$TEST_DIR/video.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "未対応の形式" "Reports unsupported"

# Test 6: 存在しないファイル
printf '\n## Test 6: Non-existent file\n'
unsetopt err_exit
output=$(repair "$TEST_TMP/none.mp4" 2>&1 || true)
setopt err_exit
assert_contains "$output" "ファイルが無い" "Reports missing file"

# Test 7: 複数ファイルとサマリ
printf '\n## Test 7: Multiple files with summary\n'
TEST_DIR="$TEST_TMP/test7"
mkdir -p "$TEST_DIR"
echo "v1" > "$TEST_DIR/v1.mp4"
echo "v2" > "$TEST_DIR/v2.ts"
echo "v3" > "$TEST_DIR/v3.avi"
unsetopt err_exit
output=$(repair "$TEST_DIR/v1.mp4" "$TEST_DIR/v2.ts" "$TEST_DIR/v3.avi" 2>&1 || true)
setopt err_exit
assert_file_exists "$TEST_DIR/v1-repaired.mp4" "First file processed"
assert_file_exists "$TEST_DIR/v2-repaired.mp4" "Second file processed"
assert_contains "$output" "サマリ" "Summary displayed"
assert_contains "$output" "OK=2" "2 OK"
assert_contains "$output" "SKIP=1" "1 SKIP"

# Test 8: 大文字拡張子
printf '\n## Test 8: Uppercase extension\n'
TEST_DIR="$TEST_TMP/test8"
mkdir -p "$TEST_DIR"
echo "dummy" > "$TEST_DIR/VIDEO.MP4"
repair "$TEST_DIR/VIDEO.MP4" > /dev/null 2>&1
assert_file_exists "$TEST_DIR/VIDEO-repaired.mp4" ".MP4 processed"

printf '\n=== All Tests Completed ===\n'
printf 'All repair tests passed!\n'
