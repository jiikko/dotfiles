#!/usr/bin/env zsh
unset CDPATH
# shellcheck shell=bash
# concat フレームレート不一致テスト
#
# フレームレート差は concat demuxer + `-c copy` では再エンコードを要しないため、
# エラーではなく警告に落としてある。エラーに戻すと --force が必要になり、
# --force はフレーム順序検証まで落とすので「無害な差のために有害な取り違えの
# 検出を失う」逆転が起きる。ここではその逆転が戻っていないことを固定する。

source "${0:A:h}/test_helper.sh"

printf '\n=== concat frame rate Tests ===\n\n'

# Test 1: フレームレート不一致でも結合は成功する
printf '## Test 1: frame rate mismatch still concatenates\n'
TEST_DIR="$TEST_TMP/fps1"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/fpsdiff_001.mp4"
echo "video 2" > "$TEST_DIR/fpsdiff_002.mp4"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(concat "$TEST_DIR/fpsdiff_001.mp4" "$TEST_DIR/fpsdiff_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Returns exit code 0 despite frame rate mismatch"
assert_file_exists "$TEST_DIR/fpsdiff.mp4" "Output file is created"
assert_not_contains "$output" "映像情報不一致" "Does NOT report it as a codec mismatch error"

# Test 2: 警告として両方のフレームレートを表示する
printf '\n## Test 2: warns and shows both frame rates\n'
assert_contains "$output" "フレームレート不一致" "Warns about the frame rate mismatch"
assert_contains "$output" "結合は続行します" "States that concat continues"
assert_contains "$output" "30/1" "Shows the first file's frame rate"
assert_contains "$output" "30000/1001" "Shows the mismatching file's frame rate"

# Test 3: フレーム順序検証が実際に呼ばれる (--force を強いられていないことの証拠)
#
# ログ文字列 (">> フレーム順序検証中...") を見るだけでは足りない。あれは検証関数を
# 呼ぶ直前に出るので、関数を丸ごと無効化しても出てしまう。呼び出し自体をスパイで
# 捕まえる。
printf '\n## Test 3: frame order verification is actually invoked\n'
TEST_DIR="$TEST_TMP/fps3"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/fpsdiff_001.mp4"
echo "video 2" > "$TEST_DIR/fpsdiff_002.mp4"
SPY_LOG="$TEST_DIR/.verify_spy"
functions -c __concat_verify_frame_order __concat_verify_frame_order_orig
__concat_verify_frame_order() {
  print -r -- "called" >> "$SPY_LOG"
  __concat_verify_frame_order_orig "$@"
}
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(concat --verbose "$TEST_DIR/fpsdiff_001.mp4" "$TEST_DIR/fpsdiff_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Returns exit code 0 with --verbose"
assert_file_exists "$SPY_LOG" "Frame order verification was actually called"
assert_not_contains "$output" "フレーム順序検証スキップ" "Verification is NOT skipped"

# Test 4: 元ファイルはゴミ箱へ送られる (--force に落ちていたら残ってしまう)
printf '\n## Test 4: source files are trashed (not kept as --force would)\n'
assert_file_not_exists "$TEST_DIR/fpsdiff_001.mp4" "Source file 1 moved to trash"
assert_file_not_exists "$TEST_DIR/fpsdiff_002.mp4" "Source file 2 moved to trash"

# Test 4b: 検証の結果が結合の成否に効いている
#
# 「呼ばれた」だけでは、戻り値が無視されていても通ってしまう。検証を失敗させたとき
# concat が失敗し、出力を消して元ファイルを残すところまで確かめる。
printf '\n## Test 4b: a failing verification actually fails the concat\n'
TEST_DIR="$TEST_TMP/fps4b"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/fpsdiff_001.mp4"
echo "video 2" > "$TEST_DIR/fpsdiff_002.mp4"
__concat_verify_frame_order() {
  REPLY="injected failure"
  return 1
}
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(concat "$TEST_DIR/fpsdiff_001.mp4" "$TEST_DIR/fpsdiff_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
# スパイを本物へ戻す (以降のテストが素の挙動を見るため)
functions -c __concat_verify_frame_order_orig __concat_verify_frame_order
assert_exit_code "1" "$exit_code" "Failing verification makes concat fail"
assert_contains "$output" "フレーム順序エラー" "Reports the frame order error"
assert_file_not_exists "$TEST_DIR/fpsdiff.mp4" "Bad output is removed"
assert_file_exists "$TEST_DIR/fpsdiff_001.mp4" "Source file 1 is kept on failure"
assert_file_exists "$TEST_DIR/fpsdiff_002.mp4" "Source file 2 is kept on failure"

# Test 5: フレームレートが一致していれば警告は出ない
printf '\n## Test 5: no warning when frame rates match\n'
TEST_DIR="$TEST_TMP/fps5"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/normal_001.mp4"
echo "video 2" > "$TEST_DIR/normal_002.mp4"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(concat "$TEST_DIR/normal_001.mp4" "$TEST_DIR/normal_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Returns exit code 0 for matching frame rates"
assert_not_contains "$output" "フレームレート不一致" "No frame rate warning when they match"

# Test 6: コーデック不一致は従来どおりエラーのまま
printf '\n## Test 6: codec mismatch is still an error\n'
TEST_DIR="$TEST_TMP/fps6"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/mismatch_001.mp4"
echo "video 2" > "$TEST_DIR/mismatch_002.mp4"
cd "$TEST_DIR" || exit 1
unsetopt err_exit
output=$(concat "$TEST_DIR/mismatch_001.mp4" "$TEST_DIR/mismatch_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "1" "$exit_code" "Returns exit code 1 for codec mismatch"
assert_contains "$output" "映像情報不一致" "Still reports codec mismatch as an error"

printf '\n=== All frame rate tests passed ===\n'
