#!/usr/bin/env zsh
# shellcheck shell=bash
# concat のオプション位置自由化のテスト
#
# 仕様:
#   - --force / --keep / --verbose / --dryrun は引数の先頭・途中・末尾どこに
#     置いても認識される
#   - "--" 以降は全てファイルパスとして扱う（"--" で始まるファイル名対策）
#   - 未知の "--xxx" オプションはエラーで停止する

source "${0:A:h}/test_helper.sh"

printf '\n=== concat Option Position Tests ===\n\n'

# ============================================================
# Test 1: --force を末尾に置いても認識される（実際に報告されたバグ）
# ============================================================
printf '## Test 1: --force at the end is recognized\n'
TEST_DIR="$TEST_TMP/optpos_1"
mkdir -p "$TEST_DIR"
echo "v1" > "$TEST_DIR/clip_001.mp4"
echo "v2" > "$TEST_DIR/clip_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/clip_001.mp4" "$TEST_DIR/clip_002.mp4" --force 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Trailing --force succeeds"
assert_file_exists "$TEST_DIR/clip.mp4" "Output created"
assert_file_exists "$TEST_DIR/clip_001.mp4" "Source kept (--force at end)"
# 「不明なオプション」「ファイルが見つかりません: --force」のようなエラーが出ていないこと
if [[ "$output" == *"不明なオプション"* ]] || [[ "$output" == *"ファイルが見つかりません: --force"* ]]; then
  printf '✗ Trailing --force should not be treated as a file\n'
  return 1
else
  printf '✓ Trailing --force not mistaken for filename\n'
fi

# ============================================================
# Test 2: --force を引数の途中に置いても認識される
# ============================================================
printf '\n## Test 2: --force in the middle is recognized\n'
TEST_DIR="$TEST_TMP/optpos_2"
mkdir -p "$TEST_DIR"
echo "v1" > "$TEST_DIR/mid_001.mp4"
echo "v2" > "$TEST_DIR/mid_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/mid_001.mp4" --force "$TEST_DIR/mid_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Middle --force succeeds"
assert_file_exists "$TEST_DIR/mid.mp4" "Output created"
assert_file_exists "$TEST_DIR/mid_001.mp4" "Source kept (--force in middle)"

# ============================================================
# Test 3: 複数オプションを末尾に置ける
# ============================================================
printf '\n## Test 3: Multiple trailing options work\n'
TEST_DIR="$TEST_TMP/optpos_3"
mkdir -p "$TEST_DIR"
echo "v1" > "$TEST_DIR/multi_001.mp4"
echo "v2" > "$TEST_DIR/multi_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/multi_001.mp4" "$TEST_DIR/multi_002.mp4" --force --keep 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Trailing --force --keep succeeds"
assert_file_exists "$TEST_DIR/multi.mp4" "Output created"
assert_file_exists "$TEST_DIR/multi_001.mp4" "Source kept"

# ============================================================
# Test 4: 未知のオプションは明示エラーになる
# ============================================================
printf '\n## Test 4: Unknown option produces explicit error\n'
TEST_DIR="$TEST_TMP/optpos_4"
mkdir -p "$TEST_DIR"
echo "v1" > "$TEST_DIR/unk_001.mp4"
echo "v2" > "$TEST_DIR/unk_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/unk_001.mp4" "$TEST_DIR/unk_002.mp4" --bogus 2>&1)
exit_code=$?
setopt err_exit
# 失敗することを期待
if (( exit_code == 0 )); then
  printf '✗ Unknown option should cause non-zero exit\n'
  return 1
else
  printf '✓ Unknown option fails the command\n'
fi
assert_contains "$output" "不明なオプション: --bogus" "Output names the unknown option"

# ============================================================
# Test 5: "--" 以降はオプション扱いされない（"--" で始まるファイル名対応）
# ============================================================
printf '\n## Test 5: "--" terminator stops option parsing\n'
TEST_DIR="$TEST_TMP/optpos_5"
mkdir -p "$TEST_DIR"
# 通常のファイル名で "--" の後ろに置けることだけ確認（"--" 始まりのファイル作成は環境依存）
echo "v1" > "$TEST_DIR/term_001.mp4"
echo "v2" > "$TEST_DIR/term_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat --force -- "$TEST_DIR/term_001.mp4" "$TEST_DIR/term_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" '"--" terminator with leading --force succeeds'
assert_file_exists "$TEST_DIR/term.mp4" "Output created"

# ============================================================
# Test 6: スペースを含むファイル名 + 末尾オプション（実例の再現）
# ============================================================
printf '\n## Test 6: Filename with spaces + trailing --force\n'
TEST_DIR="$TEST_TMP/optpos_6"
mkdir -p "$TEST_DIR"
echo "v1" > "$TEST_DIR/title with spaces_1.mp4"
echo "v2" > "$TEST_DIR/title with spaces_2.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/title with spaces_1.mp4" "$TEST_DIR/title with spaces_2.mp4" --force 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Spaces + trailing --force succeeds"
assert_file_exists "$TEST_DIR/title with spaces.mp4" "Output created with spaces in name"
assert_file_exists "$TEST_DIR/title with spaces_1.mp4" "Source kept (--force)"

printf '\n=== Option Position Tests Completed ===\n'
