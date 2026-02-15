#!/usr/bin/env zsh
# shellcheck shell=bash
# av1ify バリアントスキップテスト (Test 58-74)
# バリアント検出、偽陽性防止、ハイフンID、日本語ファイル名、早期スキップ、複合タグ

source "${0:A:h}/test_helper.sh"

printf '\n=== av1ify Variant Skip Tests (58-74) ===\n\n'

# Test 58: 別バリアント存在時にスキップ（-aac96k-enc.mp4 が存在 → --compact でスキップ）
printf '## Test 58: Skip when different variant exists (aac96k → compact)\n'
TEST_DIR="$TEST_TMP/test58"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
echo "already encoded" > "$TEST_DIR/input-aac96k-enc.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_FPS="60/1" av1ify --compact "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "SKIP" "Skips when different variant exists"
assert_contains "$output" "別バリアント" "Shows variant skip message"
assert_file_not_exists "$TEST_DIR/input-720p-30fps-aac96k-enc.mp4" "Does not create duplicate output"

# Test 59: 別バリアント存在時にスキップ（-720p-30fps-aac96k-enc.mp4 が存在 → オプションなしでスキップ）
printf '\n## Test 59: Skip when different variant exists (compact → no options)\n'
TEST_DIR="$TEST_TMP/test59"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
echo "already encoded" > "$TEST_DIR/input-720p-30fps-aac96k-enc.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "SKIP" "Skips when compact variant exists"
assert_contains "$output" "別バリアント" "Shows variant skip message"
assert_file_not_exists "$TEST_DIR/input-enc.mp4" "Does not create duplicate output"

# Test 60: 完全一致の既存チェックが優先される（-enc.mp4 が存在 → 従来のSKIPメッセージ）
printf '\n## Test 60: Exact match takes priority over variant check\n'
TEST_DIR="$TEST_TMP/test60"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
echo "already encoded" > "$TEST_DIR/input-enc.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(av1ify "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "SKIP 既存:" "Shows exact match skip (not variant)"

# Test 61: バリアントがなければ通常処理される
printf '\n## Test 61: Normal processing when no variant exists\n'
TEST_DIR="$TEST_TMP/test61"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
av1ify "$TEST_DIR/input.avi" > /dev/null 2>&1 || true
setopt err_exit
assert_file_exists "$TEST_DIR/input-enc.mp4" "Creates output when no variant exists"

# Test 62: 別バリアント存在時にスキップ（-dn2-enc.mp4 が存在 → -r 720p でスキップ）
printf '\n## Test 62: Skip when denoise variant exists (denoise → resolution)\n'
TEST_DIR="$TEST_TMP/test62"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
echo "already encoded" > "$TEST_DIR/input-dn2-enc.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(av1ify -r 720p "$TEST_DIR/input.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "SKIP" "Skips when denoise variant exists"
assert_file_not_exists "$TEST_DIR/input-720p-enc.mp4" "Does not create duplicate output"

# Test 63: encを含む無関係なファイルはバリアント検出しない
printf '\n## Test 63: Unrelated files with enc are not false-positive variants\n'
TEST_DIR="$TEST_TMP/test63"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
echo "other file" > "$TEST_DIR/input2-enc.mp4"
cd "$TEST_DIR"
unsetopt err_exit
av1ify "$TEST_DIR/input.avi" > /dev/null 2>&1 || true
setopt err_exit
assert_file_exists "$TEST_DIR/input-enc.mp4" "Processes file when only unrelated enc file exists"

# Test 64: ハイフン区切りID系ファイル名 — バリアント検出
printf '\n## Test 64: Hyphenated ID filename - variant detection\n'
TEST_DIR="$TEST_TMP/test64"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/clip-raw-1234567.mp4"
echo "already encoded" > "$TEST_DIR/clip-raw-1234567-aac96k-enc.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(av1ify "$TEST_DIR/clip-raw-1234567.mp4" 2>&1 || true)
setopt err_exit
assert_contains "$output" "SKIP" "Hyphenated ID: skips when aac96k variant exists"
assert_contains "$output" "別バリアント" "Hyphenated ID: shows variant skip message"

# Test 65: ハイフン区切りID — 別ファイルの出力を誤検出しない（偽陽性防止）
printf '\n## Test 65: Hyphenated ID - no false positive with similar-named files\n'
TEST_DIR="$TEST_TMP/test65"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/clip-raw-1234567.mp4"
echo "dummy video 2" > "$TEST_DIR/clip-raw-1234567-1.mp4"
echo "other output" > "$TEST_DIR/clip-raw-1234567-1-enc.mp4"
cd "$TEST_DIR"
unsetopt err_exit
av1ify "$TEST_DIR/clip-raw-1234567.mp4" > /dev/null 2>&1 || true
setopt err_exit
assert_file_exists "$TEST_DIR/clip-raw-1234567-enc.mp4" "Does NOT falsely skip when -1-enc.mp4 exists (different source)"

# Test 66: ハイフン区切りID — パート番号付きの別ファイル出力を誤検出しない
printf '\n## Test 66: Hyphenated ID - part number file not detected as variant\n'
TEST_DIR="$TEST_TMP/test66"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/clip-raw-1234567.mp4"
echo "other output" > "$TEST_DIR/clip-raw-1234567-part2-enc.mp4"
cd "$TEST_DIR"
unsetopt err_exit
av1ify "$TEST_DIR/clip-raw-1234567.mp4" > /dev/null 2>&1 || true
setopt err_exit
assert_file_exists "$TEST_DIR/clip-raw-1234567-enc.mp4" "Does NOT falsely skip when -part2-enc.mp4 exists"

# Test 67: 日本語ファイル名 — バリアント検出
printf '\n## Test 67: Japanese filename with hyphens - variant detection\n'
TEST_DIR="$TEST_TMP/test67"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/VLOG-013_家族旅行の記録.mp4"
echo "already encoded" > "$TEST_DIR/VLOG-013_家族旅行の記録-aac96k-enc.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(MOCK_FPS="60/1" av1ify --compact "$TEST_DIR/VLOG-013_家族旅行の記録.mp4" 2>&1 || true)
setopt err_exit
assert_contains "$output" "SKIP" "Japanese filename: skips when variant exists"
assert_contains "$output" "別バリアント" "Japanese filename: shows variant message"

# Test 68: 日本語ファイル名 — 別ファイルの出力を誤検出しない
printf '\n## Test 68: Japanese filename - no false positive\n'
TEST_DIR="$TEST_TMP/test68"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/TRIP-097-北海道旅行まとめ.mp4"
echo "other output" > "$TEST_DIR/TRIP-097-北海道旅行まとめvol2-enc.mp4"
cd "$TEST_DIR"
unsetopt err_exit
av1ify "$TEST_DIR/TRIP-097-北海道旅行まとめ.mp4" > /dev/null 2>&1 || true
setopt err_exit
assert_file_exists "$TEST_DIR/TRIP-097-北海道旅行まとめ-enc.mp4" "Japanese: does NOT falsely skip for different file"

# Test 69: compact出力済み → オプションなしで実行してもスキップ
printf '\n## Test 69: Skip with compact variant when running without options\n'
TEST_DIR="$TEST_TMP/test69"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/proj-doc-9876543.mp4"
echo "already encoded" > "$TEST_DIR/proj-doc-9876543-720p-30fps-aac96k-enc.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(av1ify "$TEST_DIR/proj-doc-9876543.mp4" 2>&1 || true)
setopt err_exit
assert_contains "$output" "SKIP" "Skips when compact variant exists (no options run)"
assert_contains "$output" "別バリアント" "Shows variant skip message"

# Test 70: バリアントスキップはffprobe前に実行される（「ファイル取得中」が出ない）
printf '\n## Test 70: Variant skip happens before ffprobe (no file fetch message)\n'
TEST_DIR="$TEST_TMP/test70"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/video.avi"
echo "already encoded" > "$TEST_DIR/video-aac96k-enc.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(av1ify "$TEST_DIR/video.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "SKIP" "Variant skip fires"
if [[ "$output" != *"ファイル取得中"* ]]; then
  printf '✓ No file fetch before variant skip\n'
else
  printf '✗ File fetch should not happen before variant skip\n'
fi

# Test 71: 複合タグのバリアント検出（720p-aac96k）
printf '\n## Test 71: Multi-tag variant detection (720p-aac96k)\n'
TEST_DIR="$TEST_TMP/test71"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/video.avi"
echo "already encoded" > "$TEST_DIR/video-720p-aac96k-enc.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(av1ify "$TEST_DIR/video.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "SKIP" "Multi-tag variant is detected"
assert_contains "$output" "別バリアント" "Shows variant message for multi-tag"

# Test 72: 全タグ組み合わせのバリアント検出（720p-30fps-dn2-aac96k）
printf '\n## Test 72: All-tag variant detection (720p-30fps-dn2-aac96k)\n'
TEST_DIR="$TEST_TMP/test72"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/video.avi"
echo "already encoded" > "$TEST_DIR/video-720p-30fps-dn2-aac96k-enc.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(av1ify "$TEST_DIR/video.avi" 2>&1 || true)
setopt err_exit
assert_contains "$output" "SKIP" "All-tag variant is detected"

# Test 73: ダブルハイフン（空タグ）はバリアントとして認識しない
printf '\n## Test 73: Double hyphen (empty tag) is not recognized as variant\n'
TEST_DIR="$TEST_TMP/test73"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/video.avi"
echo "not a variant" > "$TEST_DIR/video--enc.mp4"
cd "$TEST_DIR"
unsetopt err_exit
av1ify "$TEST_DIR/video.avi" > /dev/null 2>&1 || true
setopt err_exit
assert_file_exists "$TEST_DIR/video-enc.mp4" "Processes file when double-hyphen enc file exists"

# Test 74: 不正タグはバリアントとして認識しない
printf '\n## Test 74: Invalid tags are not recognized as variants\n'
TEST_DIR="$TEST_TMP/test74"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/video.avi"
echo "not a variant" > "$TEST_DIR/video-bonus-enc.mp4"
echo "not a variant" > "$TEST_DIR/video-HD-enc.mp4"
echo "not a variant" > "$TEST_DIR/video-x264-enc.mp4"
cd "$TEST_DIR"
unsetopt err_exit
av1ify "$TEST_DIR/video.avi" > /dev/null 2>&1 || true
setopt err_exit
assert_file_exists "$TEST_DIR/video-enc.mp4" "Processes file when only invalid-tag enc files exist"

printf '\n=== Variant Skip Tests Completed ===\n'
