#!/usr/bin/env zsh
# shellcheck shell=bash
# concat stdout リグレッションテスト (Test 30-33)
# zshの local 再宣言によるstdoutリークを検出する

source "${0:A:h}/test_helper.sh"

printf '\n=== concat Stdout Leak Tests (30-33) ===\n\n'

# ヘルパー: 出力に変数代入形式（var=value）のリークがないことを確認
assert_no_leak() {
  local output="$1"
  local message="$2"
  # 変数代入形式のリークを検出: word=value（ただし >> プレフィックス付きの正規出力は除外）
  local leaked_lines
  leaked_lines=$(echo "$output" | grep -E '^[a-zA-Z_][a-zA-Z0-9_]*=' || true)
  if [[ -z "$leaked_lines" ]]; then
    printf '✓ %s\n' "$message"
    return 0
  else
    printf '✗ %s (leaked: %s)\n' "$message" "$leaked_lines"
    return 1
  fi
}

# Test 30: 通常モード（複数ファイル）でstdoutにリークがない
printf '## Test 30: No stdout leak in normal mode\n'
TEST_DIR="$TEST_TMP/test30"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/video_001.mp4"
echo "video 2" > "$TEST_DIR/video_002.mp4"
echo "video 3" > "$TEST_DIR/video_003.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/video_001.mp4" "$TEST_DIR/video_002.mp4" "$TEST_DIR/video_003.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Concat succeeds"
assert_no_leak "$output" "No variable leak in normal mode stdout"

# Test 31: ディレクトリモードでstdoutにリークがない
printf '\n## Test 31: No stdout leak in directory mode\n'
TEST_DIR="$TEST_TMP/test31"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/clip_001.mp4"
echo "video 2" > "$TEST_DIR/clip_002.mp4"
echo "video 3" > "$TEST_DIR/clip_003.mp4"
echo "video 4" > "$TEST_DIR/scene_01.mp4"
echo "video 5" > "$TEST_DIR/scene_02.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Directory mode succeeds"
assert_no_leak "$output" "No variable leak in directory mode stdout"

# Test 32: 多数ファイルのループでリークがない（ループ2回目以降のlocal再宣言を検出）
printf '\n## Test 32: No stdout leak with many files in loop\n'
TEST_DIR="$TEST_TMP/test32"
mkdir -p "$TEST_DIR"
for i in $(seq -w 1 10); do
  echo "video $i" > "$TEST_DIR/batch_${i}.mp4"
done
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/batch_01.mp4" "$TEST_DIR/batch_02.mp4" "$TEST_DIR/batch_03.mp4" \
  "$TEST_DIR/batch_04.mp4" "$TEST_DIR/batch_05.mp4" "$TEST_DIR/batch_06.mp4" \
  "$TEST_DIR/batch_07.mp4" "$TEST_DIR/batch_08.mp4" "$TEST_DIR/batch_09.mp4" \
  "$TEST_DIR/batch_10.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Concat with 10 files succeeds"
assert_no_leak "$output" "No variable leak with 10 files"

# Test 33: ディレクトリ内の関連ファイルチェックでリークがない
# (f_stem, f_check_stem, remaining がリークしないことを確認)
printf '\n## Test 33: No stdout leak during missing-file scan\n'
TEST_DIR="$TEST_TMP/test33"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/video_001.mp4"
echo "video 2" > "$TEST_DIR/video_002.mp4"
echo "video 3" > "$TEST_DIR/video_003.mp4"
echo "other" > "$TEST_DIR/other_file.mp4"
echo "clip" > "$TEST_DIR/clip_001.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/video_001.mp4" "$TEST_DIR/video_002.mp4" "$TEST_DIR/video_003.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Concat succeeds with unrelated files in dir"
assert_no_leak "$output" "No variable leak during directory scan with unrelated files"

# Test 34: マルチグループモード（2グループ）で正常に動作
printf '\n## Test 34: Multi-group mode with 2 groups\n'
TEST_DIR="$TEST_TMP/test34"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/clip_01.mp4"
echo "video 2" > "$TEST_DIR/clip_02.mp4"
echo "video 3" > "$TEST_DIR/scene_1.mp4"
echo "video 4" > "$TEST_DIR/scene_2.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/clip_01.mp4" "$TEST_DIR/clip_02.mp4" "$TEST_DIR/scene_1.mp4" "$TEST_DIR/scene_2.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Multi-group concat succeeds"
assert_file_exists "$TEST_DIR/clip.mp4" "First group output file is created"
assert_file_exists "$TEST_DIR/scene.mp4" "Second group output file is created"
assert_contains "$output" "グループ 1" "Reports group 1"
assert_contains "$output" "グループ 2" "Reports group 2"
assert_no_leak "$output" "No variable leak in multi-group mode"

# Test 35: マルチグループで1グループのみ結合可能（残りは1ファイル）→ フォールスルー
printf '\n## Test 35: Falls through when only 1 viable group\n'
TEST_DIR="$TEST_TMP/test35"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/clip_01.mp4"
echo "video 2" > "$TEST_DIR/clip_02.mp4"
echo "video 3" > "$TEST_DIR/clip_03.mp4"
echo "video 4" > "$TEST_DIR/other_99.mp4"  # 別グループだが1ファイルのみ
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/clip_01.mp4" "$TEST_DIR/clip_02.mp4" "$TEST_DIR/clip_03.mp4" "$TEST_DIR/other_99.mp4" 2>&1)
exit_code=$?
setopt err_exit
# 単一グループモードにフォールスルーし、プレフィックス不一致でエラーになるはず
assert_exit_code "1" "$exit_code" "Falls through to single-group and errors on prefix mismatch"

# Test 36: マルチグループで元ファイルが残っている（削除しない）
printf '\n## Test 36: Multi-group mode does not delete originals\n'
TEST_DIR="$TEST_TMP/test36"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/aaa_1.mp4"
echo "video 2" > "$TEST_DIR/aaa_2.mp4"
echo "video 3" > "$TEST_DIR/bbb_1.mp4"
echo "video 4" > "$TEST_DIR/bbb_2.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/aaa_1.mp4" "$TEST_DIR/aaa_2.mp4" "$TEST_DIR/bbb_1.mp4" "$TEST_DIR/bbb_2.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Multi-group succeeds"
assert_file_exists "$TEST_DIR/aaa_1.mp4" "Original file aaa_1.mp4 still exists"
assert_file_exists "$TEST_DIR/bbb_2.mp4" "Original file bbb_2.mp4 still exists"

printf '\n=== Stdout Leak Tests Completed ===\n'
