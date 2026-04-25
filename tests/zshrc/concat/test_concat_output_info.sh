#!/usr/bin/env zsh
# shellcheck shell=bash
# concat --output-info 機械可読 IF テスト
#
# 仕様:
#   --output-info <FILE> 指定時、結合に成功した出力ファイルの絶対パスを
#   <FILE> に NUL 区切りで追記する。
#
#   - 失敗・既存スキップ・--dryrun では書き込まない
#   - ディレクトリモード／マルチグループモードでは各グループ成功ごとに追記
#   - 引数欠如はエラー (exit 1)
#   - ヘルプにオプション説明あり

source "${0:A:h}/test_helper.sh"

# 追加ヘルパー
assert_equals() {
  local expected="$1"
  local actual="$2"
  local message="$3"
  if [[ "$expected" == "$actual" ]]; then
    printf '✓ %s\n' "$message"
    return 0
  else
    printf '✗ %s (expected: %s, got: %s)\n' "$message" "$expected" "$actual"
    return 1
  fi
}

# NUL バイト数を数える (= 追記レコード数)
count_nul_bytes() {
  tr -cd '\0' < "$1" | wc -c | tr -d ' '
}

# ファイル全体のバイト数
file_byte_size() {
  wc -c < "$1" | tr -d ' '
}

printf '\n=== concat --output-info Tests ===\n\n'

# ============================================================
# Test 1: ヘルプに --output-info の説明が含まれる
# ============================================================
printf '## Test 1: Help mentions --output-info\n'
help_output=$(concat --help 2>&1)
assert_contains "$help_output" "--output-info" "Help message mentions --output-info option"
assert_contains "$help_output" "NUL" "Help mentions NUL-separated format"

# ============================================================
# Test 2: 結合成功時に絶対パスが NUL 区切りで書き込まれる
# ============================================================
printf '\n## Test 2: Successful concat writes NUL-terminated absolute path\n'
TEST_DIR="$TEST_TMP/oi_2"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/clip_001.mp4"
echo "video 2" > "$TEST_DIR/clip_002.mp4"
INFO_FILE="$TEST_DIR/info.bin"
: > "$INFO_FILE"

unsetopt err_exit
output=$(concat --keep --output-info "$INFO_FILE" \
  "$TEST_DIR/clip_001.mp4" "$TEST_DIR/clip_002.mp4" 2>&1)
exit_code=$?
setopt err_exit

assert_exit_code "0" "$exit_code" "concat succeeds"
assert_file_exists "$TEST_DIR/clip.mp4" "Output file created"
assert_file_exists "$INFO_FILE" "Info file exists"

expected_path="${TEST_DIR:A}/clip.mp4"
# 文字列含有チェック (grep は NUL を含むファイルでも -F で動く)
if grep -qF "$expected_path" "$INFO_FILE"; then
  printf '✓ Info file contains absolute output path\n'
else
  printf '✗ Info file missing expected path: %s\n' "$expected_path"
fi

# NUL 終端されている (1 レコード = 1 NUL)
nul_count=$(count_nul_bytes "$INFO_FILE")
assert_equals "1" "$nul_count" "Info file has exactly 1 NUL terminator"

# サイズ = path長 + 1 (NUL)
expected_size=$(( ${#expected_path} + 1 ))
actual_size=$(file_byte_size "$INFO_FILE")
assert_equals "$expected_size" "$actual_size" "Info file size matches path + NUL"

# ============================================================
# Test 3: --dryrun 時は info ファイルに書き込まない
# ============================================================
printf '\n## Test 3: --dryrun does NOT write to info file\n'
TEST_DIR="$TEST_TMP/oi_3"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/clip_001.mp4"
echo "video 2" > "$TEST_DIR/clip_002.mp4"
INFO_FILE="$TEST_DIR/info.bin"
: > "$INFO_FILE"

unsetopt err_exit
concat --dryrun --output-info "$INFO_FILE" \
  "$TEST_DIR/clip_001.mp4" "$TEST_DIR/clip_002.mp4" >/dev/null 2>&1
exit_code=$?
setopt err_exit

assert_exit_code "0" "$exit_code" "dryrun returns 0"
actual_size=$(file_byte_size "$INFO_FILE")
assert_equals "0" "$actual_size" "Info file is empty after dryrun"

# ============================================================
# Test 4: 既存出力 SKIP 時は info ファイルに書き込まない
# ============================================================
printf '\n## Test 4: Existing-output SKIP does NOT write\n'
TEST_DIR="$TEST_TMP/oi_4"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/clip_001.mp4"
echo "video 2" > "$TEST_DIR/clip_002.mp4"
echo "existing" > "$TEST_DIR/clip.mp4"  # 既存出力
INFO_FILE="$TEST_DIR/info.bin"
: > "$INFO_FILE"

unsetopt err_exit
output=$(concat --keep --output-info "$INFO_FILE" \
  "$TEST_DIR/clip_001.mp4" "$TEST_DIR/clip_002.mp4" 2>&1)
exit_code=$?
setopt err_exit

assert_contains "$output" "SKIP 既存" "concat reports SKIP for existing output"
assert_exit_code "0" "$exit_code" "SKIP returns 0"
actual_size=$(file_byte_size "$INFO_FILE")
assert_equals "0" "$actual_size" "Info file is empty after SKIP"

# ============================================================
# Test 5: 失敗 (コーデック不一致) 時は info ファイルに書き込まない
# ============================================================
printf '\n## Test 5: Codec-mismatch failure does NOT write\n'
TEST_DIR="$TEST_TMP/oi_5"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/mismatch_001.mp4"
echo "video 2" > "$TEST_DIR/mismatch_002.mp4"
INFO_FILE="$TEST_DIR/info.bin"
: > "$INFO_FILE"

unsetopt err_exit
output=$(concat --keep --output-info "$INFO_FILE" \
  "$TEST_DIR/mismatch_001.mp4" "$TEST_DIR/mismatch_002.mp4" 2>&1)
exit_code=$?
setopt err_exit

assert_contains "$output" "再エンコードが必要" "concat reports codec mismatch"
actual_size=$(file_byte_size "$INFO_FILE")
assert_equals "0" "$actual_size" "Info file is empty after codec failure"

# ============================================================
# Test 6: --output-info の引数欠如はエラー
# ============================================================
printf '\n## Test 6: --output-info without argument errors\n'
unsetopt err_exit
output=$(concat --output-info 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "ファイルパスが必要" "Reports missing argument error"
assert_exit_code "1" "$exit_code" "Exits with code 1"

# ============================================================
# Test 7: マルチグループモードで各グループ成功ごとに追記される
# ============================================================
printf '\n## Test 7: Multi-group mode appends per success\n'
TEST_DIR="$TEST_TMP/oi_7"
mkdir -p "$TEST_DIR"
echo "v1" > "$TEST_DIR/clip_001.mp4"
echo "v2" > "$TEST_DIR/clip_002.mp4"
echo "v3" > "$TEST_DIR/scene_001.mp4"
echo "v4" > "$TEST_DIR/scene_002.mp4"
INFO_FILE="$TEST_DIR/info.bin"
: > "$INFO_FILE"

unsetopt err_exit
output=$(concat --keep --output-info "$INFO_FILE" \
  "$TEST_DIR/clip_001.mp4" "$TEST_DIR/clip_002.mp4" \
  "$TEST_DIR/scene_001.mp4" "$TEST_DIR/scene_002.mp4" 2>&1)
exit_code=$?
setopt err_exit

assert_exit_code "0" "$exit_code" "Multi-group concat succeeds"
assert_file_exists "$TEST_DIR/clip.mp4" "Group 1 output exists"
assert_file_exists "$TEST_DIR/scene.mp4" "Group 2 output exists"

nul_count=$(count_nul_bytes "$INFO_FILE")
assert_equals "2" "$nul_count" "Info file has 2 NUL terminators (2 groups)"

if grep -qF "${TEST_DIR:A}/clip.mp4" "$INFO_FILE"; then
  printf '✓ Info file contains group 1 path\n'
else
  printf '✗ Info file missing group 1 path\n'
fi
if grep -qF "${TEST_DIR:A}/scene.mp4" "$INFO_FILE"; then
  printf '✓ Info file contains group 2 path\n'
else
  printf '✗ Info file missing group 2 path\n'
fi

# ============================================================
# Test 8: ディレクトリモードで各グループ成功ごとに追記される
# ============================================================
printf '\n## Test 8: Directory mode appends per success\n'
TEST_DIR="$TEST_TMP/oi_8"
mkdir -p "$TEST_DIR"
echo "v1" > "$TEST_DIR/movie_001.mp4"
echo "v2" > "$TEST_DIR/movie_002.mp4"
echo "v3" > "$TEST_DIR/show_001.mp4"
echo "v4" > "$TEST_DIR/show_002.mp4"
INFO_FILE="$TEST_TMP/oi_8_info.bin"  # 走査対象ディレクトリの外に置く
: > "$INFO_FILE"

unsetopt err_exit
output=$(concat --keep --output-info "$INFO_FILE" "$TEST_DIR" 2>&1)
exit_code=$?
setopt err_exit

assert_exit_code "0" "$exit_code" "Directory-mode concat succeeds"
assert_file_exists "$TEST_DIR/movie.mp4" "Group 1 output exists"
assert_file_exists "$TEST_DIR/show.mp4" "Group 2 output exists"

nul_count=$(count_nul_bytes "$INFO_FILE")
assert_equals "2" "$nul_count" "Info file has 2 NUL terminators (directory mode)"

# ============================================================
# Test 9: 既存ファイルへ追記される (truncate しない)
# ============================================================
printf '\n## Test 9: Appends to existing info file (does not truncate)\n'
TEST_DIR="$TEST_TMP/oi_9"
mkdir -p "$TEST_DIR"
echo "v1" > "$TEST_DIR/foo_001.mp4"
echo "v2" > "$TEST_DIR/foo_002.mp4"
INFO_FILE="$TEST_DIR/info.bin"
# 事前に何かが書かれている状態
printf 'preexisting\0' > "$INFO_FILE"

unsetopt err_exit
concat --keep --output-info "$INFO_FILE" \
  "$TEST_DIR/foo_001.mp4" "$TEST_DIR/foo_002.mp4" >/dev/null 2>&1
exit_code=$?
setopt err_exit

assert_exit_code "0" "$exit_code" "concat succeeds"
if grep -qF "preexisting" "$INFO_FILE"; then
  printf '✓ Pre-existing content preserved (append mode)\n'
else
  printf '✗ Pre-existing content lost (file was truncated)\n'
fi
nul_count=$(count_nul_bytes "$INFO_FILE")
assert_equals "2" "$nul_count" "Info file has 2 NUL terminators (1 preexisting + 1 new)"

printf '\n=== --output-info Tests Completed ===\n'
