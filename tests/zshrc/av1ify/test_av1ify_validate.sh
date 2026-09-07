#!/usr/bin/env zsh
unset CDPATH
# shellcheck shell=bash
# av1ify ↔ validate-mp4 連携テスト (issue 005 Phase B/C)
#
# 🚨 ここで守りたい不変条件は「**validate が NG を出したら元ファイルを消さない**」。
# postcheck を通った出力に対する最後の砦なので、これが抜けると
# 「再生できない AV1 だけが残り、元が消えている」という復旧不能な状態になる。
#
# validate が「呼ばれたか」を出力ファイルの有無で判定できない (OK でも skip でも
# 出力は同じ) ため、mock の呼び出し回数 (MOCK_VALIDATE_CALLS) で見る。

source "${0:A:h}/test_helper.sh"

printf '\n=== av1ify Validate Integration Tests ===\n\n'

# Test 1: 既定で validate が呼ばれる
printf '## Test 1: validate runs by default\n'
TEST_DIR="$TEST_TMP/validate1"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
mock_validate_reset
unset MOCK_VALIDATE_NG
unsetopt err_exit
output=$(av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
calls=$(mock_validate_calls)
(( calls >= 1 )) \
  && printf '✓ validate_mp4 was called (%d)\n' "$calls" \
  || { printf '✗ validate_mp4 was NOT called\n'; exit 1; }
assert_contains "$output" "出力を検証中" "Output announces the validation step"
assert_file_exists "$TEST_DIR/input-enc.mp4" "Output file exists"

# Test 2: --no-validate で skip される
printf '\n## Test 2: --no-validate skips validation\n'
TEST_DIR="$TEST_TMP/validate2"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
mock_validate_reset
unset MOCK_VALIDATE_NG
unsetopt err_exit
output=$(av1ify --no-validate "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
calls=$(mock_validate_calls)
(( calls == 0 )) \
  && printf '✓ validate_mp4 was skipped\n' \
  || { printf '✗ validate_mp4 ran despite --no-validate (%d calls)\n' "$calls"; exit 1; }
assert_not_contains "$output" "出力を検証中" "No validation announcement when skipped"
assert_file_exists "$TEST_DIR/input-enc.mp4" "Output file still produced"

# Test 3: validate NG なら「要確認」で元ファイルを保持する (本丸)
printf '\n## Test 3: validate NG keeps the origin file\n'
TEST_DIR="$TEST_TMP/validate3"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
TRASH_LOG="$TEST_TMP/validate3.trash.log"
: > "$TRASH_LOG"
mock_validate_reset
MOCK_VALIDATE_NG="decode-error"
unsetopt err_exit
output=$(TEST_TRASH_LOG="$TRASH_LOG" av1ify --delete-origin-if-success-and-no-ng "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
unset MOCK_VALIDATE_NG
assert_contains "$output" "要確認" "Output reports the file needs checking"
assert_contains "$output" "decode-error" "Output includes the NG reason from validate"
# 🚨 これが本丸。削除指定があっても、validate NG なら元は残す
assert_file_exists "$TEST_DIR/input.avi" "Origin file is preserved when validate fails"
trash_log_contents="$(<"$TRASH_LOG")"
assert_not_contains "$trash_log_contents" "$TEST_DIR/input.avi" "trash was NOT invoked on validate NG"

# Test 4: validate OK なら削除指定どおり元ファイルを消す (締めすぎの検出)
printf '\n## Test 4: validate OK still deletes the origin as requested\n'
TEST_DIR="$TEST_TMP/validate4"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
TRASH_LOG="$TEST_TMP/validate4.trash.log"
: > "$TRASH_LOG"
unset MOCK_VALIDATE_NG
unsetopt err_exit
output=$(TEST_TRASH_LOG="$TRASH_LOG" av1ify --delete-origin-if-success-and-no-ng "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "ゴミ箱へ移動" "Origin is trashed when validate passes"
assert_file_not_exists "$TEST_DIR/input.avi" "Origin file removed on full success"

# Test 5: validate は「元ファイル削除より前」に走る
printf '\n## Test 5: validation happens before the origin is deleted\n'
# 🚨 順序が逆だと、壊れた出力を検出したときには元が既に消えている。
# NG を注入した状態で削除指定を出し、元が残ることで順序を確かめる
# (Test 3 と同じ形だが、こちらは「順序」を主張として明示しておく)。
TEST_DIR="$TEST_TMP/validate5"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR" || exit 1
MOCK_VALIDATE_NG="truncated"
unsetopt err_exit
av1ify --delete-origin-if-success-and-no-ng "$TEST_DIR/input.avi" >/dev/null 2>&1 || true
setopt err_exit
unset MOCK_VALIDATE_NG
assert_file_exists "$TEST_DIR/input.avi" "Origin survives because validate ran first"

# Test 6: ヘルプに --no-validate が載っている
printf '\n## Test 6: --no-validate is documented\n'
help_output=$(av1ify --help 2>&1)
assert_contains "$help_output" "--no-validate" "Help lists --no-validate option"

printf '\n=== Validate Integration Tests Completed ===\n'
