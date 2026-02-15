#!/usr/bin/env zsh
# shellcheck shell=bash
# concat バリデーション + 正常系テスト (Test 1-16d)

source "${0:A:h}/test_helper.sh"

printf '\n=== concat Basic Tests (1-16d) ===\n\n'

# Test 1: ヘルプメッセージの表示
printf '## Test 1: Help message display\n'
help_output=$(concat --help 2>&1)
assert_contains "$help_output" "concat" "Help message contains command name"
assert_contains "$help_output" "使い方" "Help message is in Japanese"
assert_contains "$help_output" "--force" "Help message mentions --force option"

# Test 2: 引数不足のエラー
printf '\n## Test 2: Error with insufficient arguments\n'
TEST_DIR="$TEST_TMP/test2"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/video_001.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/video_001.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "最低2つのファイルが必要" "Reports error for single file"
assert_exit_code "1" "$exit_code" "Returns exit code 1"

# Test 3: 存在しないファイルのエラー
printf '\n## Test 3: Error for non-existent file\n'
TEST_DIR="$TEST_TMP/test3"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/video_001.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/video_001.mp4" "$TEST_DIR/video_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "ファイルが見つかりません" "Reports error for non-existent file"

# Test 4: 未対応拡張子のエラー
printf '\n## Test 4: Error for unsupported extension\n'
TEST_DIR="$TEST_TMP/test4"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/video_001.mp4"
echo "text file" > "$TEST_DIR/video_002.txt"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/video_001.mp4" "$TEST_DIR/video_002.txt" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "未対応の拡張子" "Reports error for unsupported extension"

# Test 5: 異なるディレクトリのエラー
printf '\n## Test 5: Error for different directories\n'
TEST_DIR="$TEST_TMP/test5"
mkdir -p "$TEST_DIR/dir1" "$TEST_DIR/dir2"
echo "video 1" > "$TEST_DIR/dir1/video_001.mp4"
echo "video 2" > "$TEST_DIR/dir2/video_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/dir1/video_001.mp4" "$TEST_DIR/dir2/video_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "同一ディレクトリに存在する" "Reports error for different directories"

# Test 6: 共通プレフィックス不足のエラー
printf '\n## Test 6: Error for insufficient common prefix\n'
TEST_DIR="$TEST_TMP/test6"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/ab_001.mp4"
echo "video 2" > "$TEST_DIR/xy_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/ab_001.mp4" "$TEST_DIR/xy_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "共通プレフィックス" "Reports error for insufficient common prefix"

# Test 7: 連番パターンなしのエラー
printf '\n## Test 7: Error for missing sequence pattern\n'
TEST_DIR="$TEST_TMP/test7"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/video_aaa.mp4"
echo "video 2" > "$TEST_DIR/video_bbb.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/video_aaa.mp4" "$TEST_DIR/video_bbb.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "連番パターンがありません" "Reports error for missing sequence pattern"

# Test 8: 欠番のエラー
printf '\n## Test 8: Error for missing sequence numbers\n'
TEST_DIR="$TEST_TMP/test8"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/video_001.mp4"
echo "video 3" > "$TEST_DIR/video_003.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/video_001.mp4" "$TEST_DIR/video_003.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "欠番があります" "Reports error for missing sequence numbers"

# Test 9: 連番が0/1以外から始まっても成功
printf '\n## Test 9: Sequence starting from 5 succeeds\n'
TEST_DIR="$TEST_TMP/test9"
mkdir -p "$TEST_DIR"
echo "video 5" > "$TEST_DIR/video_005.mp4"
echo "video 6" > "$TEST_DIR/video_006.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/video_005.mp4" "$TEST_DIR/video_006.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_file_exists "$TEST_DIR/video.mp4" "Output file is created for sequence starting from 5"
assert_contains "$output" "完了" "Reports success for sequence starting from 5"

# Test 10: コーデック不一致のエラー
printf '\n## Test 10: Error for codec mismatch\n'
TEST_DIR="$TEST_TMP/test10"
mkdir -p "$TEST_DIR"
# mismatch_001 は通常のコーデック、mismatch_002 は異なるコーデック（モックで判定）
echo "video 1" > "$TEST_DIR/mismatch_001.mp4"
echo "video 2" > "$TEST_DIR/mismatch_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/mismatch_001.mp4" "$TEST_DIR/mismatch_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "再エンコードが必要" "Reports error for codec mismatch"

# Test 11: --forceでコーデック不一致を無視
printf '\n## Test 11: --force ignores codec mismatch\n'
TEST_DIR="$TEST_TMP/test11"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/mismatch_001.mp4"
echo "video 2" > "$TEST_DIR/mismatch_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat --force "$TEST_DIR/mismatch_001.mp4" "$TEST_DIR/mismatch_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "完了" "--force allows concat despite mismatch"

# Test 12: 正常な結合（_NNNパターン）
printf '\n## Test 12: Successful concat with _NNN pattern\n'
TEST_DIR="$TEST_TMP/test12"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/video_001.mp4"
echo "video 2" > "$TEST_DIR/video_002.mp4"
echo "video 3" > "$TEST_DIR/video_003.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/video_001.mp4" "$TEST_DIR/video_002.mp4" "$TEST_DIR/video_003.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_file_exists "$TEST_DIR/video.mp4" "Output file is created"
assert_contains "$output" "完了" "Reports success"

# Test 13: 正常な結合（-NNNパターン）
printf '\n## Test 13: Successful concat with -NNN pattern\n'
TEST_DIR="$TEST_TMP/test13"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/clip-01.mp4"
echo "video 2" > "$TEST_DIR/clip-02.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/clip-01.mp4" "$TEST_DIR/clip-02.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_file_exists "$TEST_DIR/clip.mp4" "Output file is created with -NNN pattern"

# Test 14: 正常な結合（(N)パターン）
printf '\n## Test 14: Successful concat with (N) pattern\n'
TEST_DIR="$TEST_TMP/test14"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/movie(1).mp4"
echo "video 2" > "$TEST_DIR/movie(2).mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/movie(1).mp4" "$TEST_DIR/movie(2).mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_file_exists "$TEST_DIR/movie.mp4" "Output file is created with (N) pattern"

# Test 15: 正常な結合（partNパターン）
printf '\n## Test 15: Successful concat with partN pattern\n'
TEST_DIR="$TEST_TMP/test15"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/file_part1.mp4"
echo "video 2" > "$TEST_DIR/file_part2.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/file_part1.mp4" "$TEST_DIR/file_part2.mp4" 2>&1)
exit_code=$?
setopt err_exit
# 共通プレフィックス計算: file_part1, file_part2 → file_part → part除去 → file_ → 末尾_除去 → file
assert_file_exists "$TEST_DIR/file.mp4" "Output file is created with partN pattern"

# Test 16: 正常な結合（-N-suffixパターン）
printf '\n## Test 16: Successful concat with -N-suffix pattern\n'
TEST_DIR="$TEST_TMP/test16"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/xxx-1-enc.mp4"
echo "video 2" > "$TEST_DIR/xxx-2-enc.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/xxx-1-enc.mp4" "$TEST_DIR/xxx-2-enc.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_file_exists "$TEST_DIR/xxx.mp4" "Output file is created with -N-suffix pattern"

# Test 16b: サフィックス不一致のエラー
printf '\n## Test 16b: Error for mismatched suffix\n'
TEST_DIR="$TEST_TMP/test16b"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/xxx-1-enc.mp4"
echo "video 2" > "$TEST_DIR/xxx-2-raw.mp4"  # 異なるサフィックス
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/xxx-1-enc.mp4" "$TEST_DIR/xxx-2-raw.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "サフィックスが異なります" "Reports error for mismatched suffix"

# Test 16b2: サフィックス不一致でも末尾数字が連番なら成功
printf '\n## Test 16b2: Suffix mismatch but trailing numbers form sequence\n'
TEST_DIR="$TEST_TMP/test16b2"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/lecture_vol3_topic_review2.mp4"
echo "video 2" > "$TEST_DIR/lecture_vol3_topic_review1.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/lecture_vol3_topic_review2.mp4" "$TEST_DIR/lecture_vol3_topic_review1.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Trailing number retry succeeds"
assert_file_exists "$TEST_DIR/lecture_vol3_topic_review.mp4" "Output file uses trailing-number prefix"
assert_contains "$output" "完了" "Reports success for trailing number pattern"

# Test 16c: サフィックスに数字を含むパターン（-aac96k-enc など）
printf '\n## Test 16c: Suffix containing numbers\n'
TEST_DIR="$TEST_TMP/test16c"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/video_1-aac96k-enc.mp4"
echo "video 2" > "$TEST_DIR/video_2-aac96k-enc.mp4"
echo "video 3" > "$TEST_DIR/video_3-aac96k-enc.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/video_1-aac96k-enc.mp4" "$TEST_DIR/video_2-aac96k-enc.mp4" "$TEST_DIR/video_3-aac96k-enc.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_file_exists "$TEST_DIR/video.mp4" "Output file is created with numeric suffix pattern"
assert_contains "$output" "完了" "Reports success for numeric suffix pattern"

# Test 16d: 共通サフィックス除去後に連番を検出（clipNN_00-suffix パターン）
printf '\n## Test 16d: Common suffix removal then sequence detection\n'
TEST_DIR="$TEST_TMP/test16d"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/clip28_00-aac96k-enc.mp4"
echo "video 2" > "$TEST_DIR/clip29_00-aac96k-enc.mp4"
echo "video 3" > "$TEST_DIR/clip30_00-aac96k-enc.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/clip28_00-aac96k-enc.mp4" "$TEST_DIR/clip29_00-aac96k-enc.mp4" "$TEST_DIR/clip30_00-aac96k-enc.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_file_exists "$TEST_DIR/clip.mp4" "Output file is created with common suffix removal pattern"
assert_contains "$output" "完了" "Reports success for common suffix removal pattern"

printf '\n=== Basic Tests Completed ===\n'
