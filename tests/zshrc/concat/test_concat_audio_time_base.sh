#!/usr/bin/env zsh
unset CDPATH
# shellcheck shell=bash
# concat 音声 time_base 不一致検出テスト (issue 143 再現 1)
# codec / sample_rate / channels が同じでも音声 time_base が違えば拒否し、remux を案内する。

source "${0:A:h}/test_helper.sh"

printf '\n=== concat audio time_base Tests ===\n\n'

# Test 1: 音声 time_base 不一致でエラー (映像側は全部一致)
printf '## Test 1: audio time_base mismatch error\n'
TEST_DIR="$TEST_TMP/atb1"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/audtb_001.mp4"
echo "video 2" > "$TEST_DIR/audtb_002.mp4"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(concat "$TEST_DIR/audtb_001.mp4" "$TEST_DIR/audtb_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "1" "$exit_code" "Returns exit code 1 for audio time_base mismatch"
assert_contains "$output" "音声 time_base不一致" "Reports audio time_base mismatch"
assert_not_contains "$output" "音声情報不一致" "Does not misreport as codec mismatch (re-encode)"
assert_contains "$output" "-c copy" "Suggests remux (no re-encode)"
assert_contains "$output" "audtb_002_remux.mp4" "Remux target is the file off 1/sample_rate"
assert_not_contains "$output" "audtb_001_remux.mp4" "Does not ask to remux the normal file"
assert_file_not_exists "$TEST_DIR/atbase.mp4" "No output file on mismatch"

# Test 2: --force なら無視
printf '\n## Test 2: --force ignores audio time_base mismatch\n'
TEST_DIR="$TEST_TMP/atb2"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/audtb_001.mp4"
echo "video 2" > "$TEST_DIR/audtb_002.mp4"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(concat --force "$TEST_DIR/audtb_001.mp4" "$TEST_DIR/audtb_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "--force allows concat despite audio time_base mismatch"

# Test 3: ffprobe が失敗したら「判定不能」として拒否 (空を一致と読んで素通りしない)
printf '\n## Test 3: audio probe failure is rejected, not treated as a match\n'
TEST_DIR="$TEST_TMP/atb3"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/probefail_001.mp4"
echo "video 2" > "$TEST_DIR/probefail_002.mp4"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(concat "$TEST_DIR/probefail_001.mp4" "$TEST_DIR/probefail_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "1" "$exit_code" "Returns exit code 1 when audio ffprobe fails"
assert_contains "$output" "ffprobe 失敗" "Reports the probe failure explicitly"
assert_not_contains "$output" "再エンコードが必要" "Does not misreport probe failure as codec mismatch"
assert_contains "$output" "probefail_002" "Names the file that could not be probed"
assert_file_not_exists "$TEST_DIR/probefail.mp4" "No output file when probe failed"

# Test 4: 一致なら通る (回帰)
printf '\n## Test 4: matching audio time_base succeeds\n'
TEST_DIR="$TEST_TMP/atb4"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/normal_001.mp4"
echo "video 2" > "$TEST_DIR/normal_002.mp4"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(concat "$TEST_DIR/normal_001.mp4" "$TEST_DIR/normal_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Returns exit code 0 for matching audio time_base"
assert_file_exists "$TEST_DIR/normal.mp4" "Output file is created"

printf '\n=== concat audio time_base Tests Completed ===\n'
