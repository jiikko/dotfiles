#!/usr/bin/env zsh
# shellcheck shell=bash
SCRIPT_PATH="${(%):-%x}"
source "$(dirname "$SCRIPT_PATH")/test_helper.sh"

# テスト開始
printf '\n=== av1ify Basic Tests (1-7) ===\n\n'

# Test 1: ヘルプメッセージの表示
printf '## Test 1: Help message display\n'
help_output=$(av1ify --help 2>&1)
assert_contains "$help_output" "av1ify" "Help message contains command name"
assert_contains "$help_output" "使い方" "Help message is in Japanese"
assert_contains "$help_output" "複数のファイルを順番に変換" "Help message mentions multiple file support"

# Test 1b: ドライラン表示
printf '\n## Test 1b: Dry-run option\n'
TEST_DIR="$TEST_TMP/test1b"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(av1ify "$TEST_DIR/input.avi" --dry-run 2>&1 || true)
assert_file_not_exists "$TEST_DIR/input-enc.mp4" "Dry-run does not create output file"
assert_contains "$output" "DRY-RUN" "Dry-run output contains marker"

# Test 2: 単一ファイルの処理
printf '\n## Test 2: Single file processing\n'
TEST_DIR="$TEST_TMP/test2"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
av1ify "$TEST_DIR/input.avi" > /dev/null 2>&1
assert_file_exists "$TEST_DIR/input-enc.mp4" "Output file is created with -enc.mp4 suffix"

# Test 3: 既存の-encファイルのスキップ
printf '\n## Test 3: Skip existing -enc files\n'
TEST_DIR="$TEST_TMP/test3"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/video.avi"
echo "already encoded" > "$TEST_DIR/video-enc.mp4"
cd "$TEST_DIR"
output=$(av1ify "$TEST_DIR/video.avi" 2>&1)
assert_contains "$output" "SKIP" "Skips when output file already exists"

# Test 4: -encファイル自体のスキップ
printf '\n## Test 4: Skip -enc.mp4 input files\n'
TEST_DIR="$TEST_TMP/test4"
mkdir -p "$TEST_DIR"
echo "already encoded" > "$TEST_DIR/video-enc.mp4"
cd "$TEST_DIR"
output=$(av1ify "$TEST_DIR/video-enc.mp4" 2>&1)
assert_contains "$output" "SKIP" "Skips -enc.mp4 input files"

# Test 5: -encoded.* ファイルのスキップ
printf '\n## Test 5: Skip -encoded.* input files\n'
TEST_DIR="$TEST_TMP/test5"
mkdir -p "$TEST_DIR"
echo "already encoded" > "$TEST_DIR/gachi625_hd縦ロール.mp4-encoded.mp4"
cd "$TEST_DIR"
output=$(av1ify "$TEST_DIR/gachi625_hd縦ロール.mp4-encoded.mp4" 2>&1)
assert_contains "$output" "SKIP" "Skips -encoded.* input files"

# Test 6: 存在しないファイルのエラー処理
printf '\n## Test 6: Error handling for non-existent file\n'
TEST_DIR="$TEST_TMP/test6"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"
output=$(av1ify "$TEST_DIR/nonexistent.avi" 2>&1 || true)
assert_contains "$output" "ファイルが無い" "Reports error for non-existent file"

# Test 7: 空の引数でヘルプ表示（スキップ - 環境依存）
printf '\n## Test 7: Help display with empty argument (SKIPPED)\n'
printf '↷ Skipped due to environment-specific behavior\n'

printf '\n=== av1ify Basic Tests Completed ===\n'
