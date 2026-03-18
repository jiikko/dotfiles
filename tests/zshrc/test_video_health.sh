#!/usr/bin/env zsh
# shellcheck shell=bash
# video_health 健全性チェックテスト

setopt err_exit no_unset pipe_fail

ROOT_DIR="${0:A:h}/../.."
TEST_TMP="$(mktemp -d)"
cleanup() { rm -rf "$TEST_TMP"; }
trap cleanup EXIT

# ffprobeモック
MOCK_BIN_DIR="$TEST_TMP/mock_bin"
mkdir -p "$MOCK_BIN_DIR"

cat > "$MOCK_BIN_DIR/ffprobe" <<'FFPROBE_MOCK'
#!/usr/bin/env sh
input_file=""
for arg in "$@"; do
  [ -f "$arg" ] && input_file="$arg"
done

# stream duration (映像)
if echo "$*" | grep -q "select_streams v:0" && echo "$*" | grep -q "stream=duration"; then
  case "$input_file" in
    *corrupted*) echo "7194.0" ;;
    *no_video*)  echo "" ;;
    *)           echo "14212.0" ;;
  esac
  exit 0
fi

# format duration
if echo "$*" | grep -q "format=duration"; then
  case "$input_file" in
    *no_video*) echo "" ;;
    *)          echo "14212.0" ;;
  esac
  exit 0
fi

exit 0
FFPROBE_MOCK
chmod +x "$MOCK_BIN_DIR/ffprobe"

export PATH="$MOCK_BIN_DIR:$PATH"
source "$ROOT_DIR/zshlib/_video_health.zsh"

# ヘルパー
assert_exit_code() {
  local expected="$1" actual="$2" message="$3"
  if [[ "$expected" == "$actual" ]]; then
    printf '✓ %s\n' "$message"
  else
    printf '✗ %s (expected: %s, got: %s)\n' "$message" "$expected" "$actual"
    return 1
  fi
}
assert_contains() {
  local haystack="$1" needle="$2" message="$3"
  if [[ "$haystack" == *"$needle"* ]]; then
    printf '✓ %s\n' "$message"
  else
    printf '✗ %s (expected to contain: %s)\n' "$message" "$needle"
    return 1
  fi
}

printf '\n=== video_health Tests ===\n\n'

# Test 1: 正常なファイル
printf '## Test 1: Normal file passes\n'
touch "$TEST_TMP/normal.mp4"
unsetopt err_exit
__video_health_check "$TEST_TMP/normal.mp4"
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Normal file returns 0"

# Test 2: 破損ファイル（映像duration << format duration）
printf '\n## Test 2: Corrupted file detected\n'
touch "$TEST_TMP/corrupted.mp4"
unsetopt err_exit
__video_health_check "$TEST_TMP/corrupted.mp4"
exit_code=$?
setopt err_exit
assert_exit_code "1" "$exit_code" "Corrupted file returns 1"
assert_contains "$REPLY" "time_base破損" "Error mentions time_base corruption"

# Test 3: 映像なしファイル
printf '\n## Test 3: No video stream\n'
touch "$TEST_TMP/no_video.mp4"
unsetopt err_exit
__video_health_check "$TEST_TMP/no_video.mp4"
exit_code=$?
setopt err_exit
assert_exit_code "2" "$exit_code" "No video returns 2 (skip)"

# Test 4: video_health コマンド - 単一ファイル正常
printf '\n## Test 4: video_health command - single normal file\n'
touch "$TEST_TMP/single_normal.mp4"
unsetopt err_exit
output=$(video_health "$TEST_TMP/single_normal.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "video_health returns 0 for normal file"
assert_contains "$output" "正常" "Output shows normal"

# Test 5: video_health コマンド - 単一ファイル破損
printf '\n## Test 5: video_health command - single corrupted file\n'
touch "$TEST_TMP/single_corrupted.mp4"
unsetopt err_exit
output=$(video_health "$TEST_TMP/single_corrupted.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "1" "$exit_code" "video_health returns 1 for corrupted file"

# Test 6: video_health コマンド - ディレクトリ
printf '\n## Test 6: video_health command - directory\n'
mkdir -p "$TEST_TMP/dir_test"
touch "$TEST_TMP/dir_test/ok.mp4"
touch "$TEST_TMP/dir_test/corrupted_file.mp4"
unsetopt err_exit
output=$(video_health "$TEST_TMP/dir_test" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "1" "$exit_code" "Directory with corrupted file returns 1"
assert_contains "$output" "破損=1" "Summary shows 1 corrupted"

# Test 7: ヘルプ表示
printf '\n## Test 7: Help message\n'
unsetopt err_exit
output=$(video_health --help 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Help returns 0"
assert_contains "$output" "video_health" "Help contains command name"

printf '\n=== video_health Tests Completed ===\n'
