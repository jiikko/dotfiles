#!/usr/bin/env zsh
# shellcheck shell=bash
# concat --force オプションのテスト
#
# 仕様:
#   - --force: コーデック不一致チェックをスキップ
#   - --force: 結合後のフレーム順序検証をスキップ
#   - --force: 元ファイルをゴミ箱へ移動せず残す（結果が保証されないため安全側に倒す）

source "${0:A:h}/test_helper.sh"

printf '\n=== concat --force Tests ===\n\n'

# ============================================================
# Test 1: --force でコーデック不一致を許容して結合成功する
# ============================================================
printf '## Test 1: --force bypasses codec mismatch\n'
TEST_DIR="$TEST_TMP/force_1"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/mismatch_001.mp4"
echo "video 2" > "$TEST_DIR/mismatch_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat --force "$TEST_DIR/mismatch_001.mp4" "$TEST_DIR/mismatch_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "--force succeeds despite codec mismatch"
assert_file_exists "$TEST_DIR/mismatch.mp4" "Output created with --force"

# ============================================================
# Test 2: --force は元ファイルを残す（ゴミ箱へも送らない）
# ============================================================
printf '\n## Test 2: --force preserves source files (no trash, no delete)\n'
assert_file_exists "$TEST_DIR/mismatch_001.mp4" "Source file 1 kept with --force"
assert_file_exists "$TEST_DIR/mismatch_002.mp4" "Source file 2 kept with --force"
# ゴミ箱メッセージが出ていないこと
if [[ "$output" == *"元ファイルをゴミ箱へ移動中"* ]]; then
  printf '✗ --force should not print trash message\n'
  return 1
else
  printf '✓ No trash message with --force\n'
fi
# ゴミ箱にも入っていないこと
if [[ -f "$TEST_TMP/_mock_trash/mismatch_001.mp4" ]]; then
  printf '✗ --force should not move source to trash\n'
  return 1
else
  printf '✓ Source not in trash with --force\n'
fi

# ============================================================
# Test 3: --force --verbose でフレーム検証スキップメッセージが出る
# ============================================================
printf '\n## Test 3: --force skips frame order verification\n'
TEST_DIR="$TEST_TMP/force_3"
mkdir -p "$TEST_DIR"
echo "v1" > "$TEST_DIR/normal_001.mp4"
echo "v2" > "$TEST_DIR/normal_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat --force --verbose "$TEST_DIR/normal_001.mp4" "$TEST_DIR/normal_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "--force --verbose succeeds"
assert_contains "$output" "フレーム順序検証スキップ" "Verbose output mentions verification skip"
# 通常の検証ログが出ていないこと
if [[ "$output" == *"フレーム順序検証中"* ]]; then
  printf '✗ --force should skip verification entirely\n'
  return 1
else
  printf '✓ Frame verification not run with --force\n'
fi

# ============================================================
# Test 4: --force なしの通常モードではフレーム順序検証が走る
# ============================================================
printf '\n## Test 4: Without --force, frame order verification runs\n'
TEST_DIR="$TEST_TMP/force_4"
mkdir -p "$TEST_DIR"
echo "v1" > "$TEST_DIR/check_001.mp4"
echo "v2" > "$TEST_DIR/check_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat --verbose "$TEST_DIR/check_001.mp4" "$TEST_DIR/check_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Normal concat with --verbose succeeds"
assert_contains "$output" "フレーム順序検証中" "Verbose output mentions verification step"

# ============================================================
# Test 5: --force と --keep の併用
# ============================================================
printf '\n## Test 5: --force combined with --keep\n'
TEST_DIR="$TEST_TMP/force_5"
mkdir -p "$TEST_DIR"
echo "v1" > "$TEST_DIR/combo_001.mp4"
echo "v2" > "$TEST_DIR/combo_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat --force --keep "$TEST_DIR/combo_001.mp4" "$TEST_DIR/combo_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "--force --keep succeeds"
assert_file_exists "$TEST_DIR/combo.mp4" "Output created"
assert_file_exists "$TEST_DIR/combo_001.mp4" "Source file 1 kept"
assert_file_exists "$TEST_DIR/combo_002.mp4" "Source file 2 kept"

# ============================================================
# Test 6: --force ヘルプ表示の確認
# ============================================================
printf '\n## Test 6: --force is documented in help\n'
unsetopt err_exit
help_output=$(concat --help 2>&1)
help_rc=$?
setopt err_exit
assert_exit_code "0" "$help_rc" "concat --help succeeds"
assert_contains "$help_output" "フレーム順序検証をスキップ" "Help mentions verification skip"
assert_contains "$help_output" "元ファイルは削除しません" "Help mentions source preservation"

printf '\n=== --force Tests Completed ===\n'
