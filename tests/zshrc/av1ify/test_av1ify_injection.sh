#!/usr/bin/env zsh
unset CDPATH
# shellcheck shell=bash
# issue 089 の実経路テスト: 名前に $(...) を含むファイルを av1ify に通しても実行されない。
#
# なぜ実経路が必要か: 静的検査 (tests/zshrc/test_print_p_injection.sh) は「危険な形が
# 残っていないこと」しか見ない。実際に処理の全ログ経路 (完了ログ / 元ファイル削除 /
# 健全性チェック警告) を通したときに実行されないことは、通してみないと言えない。
# 対象は主に zshlib/_av1ify_encode.zsh (issue 089 の 24 件がここにある)。

source "${0:A:h}/test_helper.sh"
setopt prompt_subst   # 対話シェルの既定。この下でないと $(...) は実行されない

printf '\n=== av1ify $(...) injection Tests ===\n\n'

# Test 1: 変換の全ログ経路を通しても実行されない (完了ログ = _av1ify_encode.zsh)
printf '## Test 1: Full conversion of a file with $(...) in its name\n'
TEST_DIR="$TEST_TMP/inj1"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"
EVIL='evil$(touch pwned_convert)file.avi'
echo "dummy video" > "./$EVIL"
unsetopt err_exit
output=$(av1ify "./$EVIL" 2>&1)
setopt err_exit
assert_file_not_exists "$TEST_DIR/pwned_convert" "Conversion does not execute \$(...) in the filename"
assert_contains "$output" 'evil$(touch pwned_convert)file' "Logs the filename verbatim"
assert_file_exists "$TEST_DIR/evil\$(touch pwned_convert)file-enc.mp4" "Still converts the file"

# Test 2: 元ファイル削除のログ経路 (trash / rm の 3 分岐のうち trash)
printf '\n## Test 2: Delete-origin log path\n'
TEST_DIR="$TEST_TMP/inj2"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"
EVIL='del$(touch pwned_delete)file.avi'
echo "dummy video" > "./$EVIL"
export TEST_TRASH_LOG="$TEST_DIR/trash.log"
unsetopt err_exit
output=$(av1ify --delete-origin-if-success-and-no-ng "./$EVIL" 2>&1)
setopt err_exit
assert_file_not_exists "$TEST_DIR/pwned_delete" "Delete-origin log does not execute \$(...)"
assert_contains "$output" 'del$(touch pwned_delete)file' "Delete log shows the filename verbatim"
unset TEST_TRASH_LOG

# Test 3: 健全性チェック警告の経路 (--force。_av1ify_encode.zsh の警告 2 行)
printf '\n## Test 3: Health-check warning log path (--force)\n'
TEST_DIR="$TEST_TMP/inj3"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"
EVIL='warn$(touch pwned_warn)file.avi'
echo "dummy video" > "./$EVIL"
# 入力の健全性チェックを失敗させる (DTS 逆行) → --force で続行し警告ログを出す
unsetopt err_exit
output=$(MOCK_DTS_BACKWARD=1 av1ify --force "./$EVIL" 2>&1)
setopt err_exit
assert_file_not_exists "$TEST_DIR/pwned_warn" "Health-check warning does not execute \$(...)"
assert_contains "$output" 'warn$(touch pwned_warn)file' "Warning shows the filename verbatim"

# Test 4: 陽性対照 — 危険な形なら実際に実行される (検出器が生きている証拠)
printf '\n## Test 4: Positive control\n'
TEST_DIR="$TEST_TMP/inj4"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"
print -P -- "%F{green}✅ 完了: control\$(touch $TEST_DIR/pwned_control)x%f" > /dev/null
assert_file_exists "$TEST_DIR/pwned_control" "Positive control: print -P does execute \$(...)"

printf '\n=== av1ify $(...) injection Tests: all passed ===\n'
