#!/usr/bin/env zsh
# shellcheck shell=bash
# concat スペース含むファイル名でのグループ化リグレッションテスト (Test 40-44)
# バグ: ${(u)all_keys} がクォートなしで展開されると、キーに含まれるスペースで
#       単語分割が発生し、グループが検出されなくなる
# 修正: "${(u)all_keys[@]}" にクォート付き展開に変更

source "${0:A:h}/test_helper.sh"

printf '\n=== concat Space Grouping Regression Tests (40-44) ===\n\n'

# Test 40: ディレクトリモードでスペース含むファイル名のグループ検出
printf '## Test 40: Directory mode groups files with spaces in names\n'
TEST_DIR="$TEST_TMP/test40"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/lecture vol3 topic review_1.mp4"
echo "video 2" > "$TEST_DIR/lecture vol3 topic review_2.mp4"
echo "video 3" > "$TEST_DIR/single_file.mp4"  # グループ化されないファイル
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Directory mode succeeds with spaced filenames"
assert_contains "$output" "グループ 1" "Detects group with spaced filenames"
assert_file_exists "$TEST_DIR/lecture vol3 topic review.mp4" "Output file is created for spaced group"
# ディレクトリモードでは元ファイルが削除される
assert_file_not_exists "$TEST_DIR/lecture vol3 topic review_1.mp4" "Original _1 file deleted in directory mode"
assert_file_not_exists "$TEST_DIR/lecture vol3 topic review_2.mp4" "Original _2 file deleted in directory mode"

# Test 41: ディレクトリモードで複数のスペース含むグループを検出
printf '\n## Test 41: Directory mode detects multiple spaced groups\n'
TEST_DIR="$TEST_TMP/test41"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/meeting 2024 spring_1.mp4"
echo "video 2" > "$TEST_DIR/meeting 2024 spring_2.mp4"
echo "video 3" > "$TEST_DIR/workshop day two_1.mp4"
echo "video 4" > "$TEST_DIR/workshop day two_2.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Directory mode succeeds with multiple spaced groups"
assert_file_exists "$TEST_DIR/meeting 2024 spring.mp4" "First spaced group output created"
assert_file_exists "$TEST_DIR/workshop day two.mp4" "Second spaced group output created"
assert_contains "$output" "グループ" "Reports groups"
assert_contains "$output" "完了" "Reports completion"

# Test 42: マルチグループモードでスペース含むファイル名のグループ検出
printf '\n## Test 42: Multi-group file mode groups files with spaces\n'
TEST_DIR="$TEST_TMP/test42"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/conference keynote day_1.mp4"
echo "video 2" > "$TEST_DIR/conference keynote day_2.mp4"
echo "video 3" > "$TEST_DIR/tutorial session intro_1.mp4"
echo "video 4" > "$TEST_DIR/tutorial session intro_2.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat \
  "$TEST_DIR/conference keynote day_1.mp4" \
  "$TEST_DIR/conference keynote day_2.mp4" \
  "$TEST_DIR/tutorial session intro_1.mp4" \
  "$TEST_DIR/tutorial session intro_2.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Multi-group mode succeeds with spaced filenames"
assert_file_exists "$TEST_DIR/conference keynote day.mp4" "First group output created"
assert_file_exists "$TEST_DIR/tutorial session intro.mp4" "Second group output created"
assert_contains "$output" "グループ 1" "Reports group 1"
assert_contains "$output" "グループ 2" "Reports group 2"

# Test 43: ディレクトリモードで「結合可能なグループが見つかりませんでした」にならない
# (修正前はスペース含むキーの分割により0グループと判定されていた)
printf '\n## Test 43: Directory mode does not falsely report no groups\n'
TEST_DIR="$TEST_TMP/test43"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/project demo final cut version_1.mp4"
echo "video 2" > "$TEST_DIR/project demo final cut version_2.mp4"
echo "video 3" > "$TEST_DIR/project demo final cut version_3.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Directory mode finds spaced groups"
# 修正前はこのメッセージが出力されていた
if [[ "$output" == *"結合可能なグループが見つかりませんでした"* ]]; then
  printf '✗ Falsely reports no groups found (regression!)\n'
  return 1
else
  printf '✓ Does not falsely report no groups\n'
fi
assert_file_exists "$TEST_DIR/project demo final cut version.mp4" "Output file created for 3-file spaced group"

# Test 44: スペースなしファイルとスペースありファイルの混在ディレクトリモード
printf '\n## Test 44: Directory mode handles mixed spaced and non-spaced groups\n'
TEST_DIR="$TEST_TMP/test44"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/simple_1.mp4"
echo "video 2" > "$TEST_DIR/simple_2.mp4"
echo "video 3" > "$TEST_DIR/recording day one_1.mp4"
echo "video 4" > "$TEST_DIR/recording day one_2.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Directory mode handles mixed groups"
assert_file_exists "$TEST_DIR/simple.mp4" "Non-spaced group output created"
assert_file_exists "$TEST_DIR/recording day one.mp4" "Spaced group output created"

printf '\n=== Space Grouping Regression Tests Completed ===\n'
