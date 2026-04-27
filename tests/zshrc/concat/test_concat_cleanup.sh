#!/usr/bin/env zsh
# shellcheck shell=bash
# concat デフォルトのゴミ箱移動動作 & --keep オプションのテスト
#
# 仕様:
#   - ファイル指定モード: デフォルトで結合成功後に元ファイルをゴミ箱へ移動
#   - ディレクトリモード: デフォルトで結合成功後に元ファイルをゴミ箱へ移動
#   - マルチグループモード: デフォルトで結合成功後に元ファイルをゴミ箱へ移動
#   - --keep 指定時: 元ファイルを残す
#   - --dryrun 指定時: 元ファイルをゴミ箱へ移動しない（--keep の有無に関わらず）
#   - 既存出力でスキップされた場合: ゴミ箱へ移動しない

source "${0:A:h}/test_helper.sh"

printf '\n=== concat Cleanup Behavior Tests ===\n\n'

# ============================================================
# Test 1: ファイル指定モード（デフォルト）で元ファイルがゴミ箱へ移動される
# ============================================================
printf '## Test 1: File-spec mode moves source files to trash by default\n'
TEST_DIR="$TEST_TMP/cleanup_1"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/clip_001.mp4"
echo "video 2" > "$TEST_DIR/clip_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/clip_001.mp4" "$TEST_DIR/clip_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "File-spec concat succeeds"
assert_file_exists "$TEST_DIR/clip.mp4" "Output file is created"
assert_file_not_exists "$TEST_DIR/clip_001.mp4" "Source file 1 removed from origin"
assert_file_not_exists "$TEST_DIR/clip_002.mp4" "Source file 2 removed from origin"
assert_in_mock_trash "clip_001.mp4" "Source file 1 landed in trash"
assert_in_mock_trash "clip_002.mp4" "Source file 2 landed in trash"
assert_contains "$output" "元ファイルをゴミ箱へ移動中" "Output mentions trashing"
assert_contains "$output" "ゴミ箱へ: clip_001.mp4" "Output confirms file 1 trashing"
assert_contains "$output" "ゴミ箱へ: clip_002.mp4" "Output confirms file 2 trashing"

# ============================================================
# Test 2: --keep 指定時は元ファイルを残す
# ============================================================
printf '\n## Test 2: --keep preserves source files\n'
TEST_DIR="$TEST_TMP/cleanup_2"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/clip_001.mp4"
echo "video 2" > "$TEST_DIR/clip_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat --keep "$TEST_DIR/clip_001.mp4" "$TEST_DIR/clip_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "File-spec concat with --keep succeeds"
assert_file_exists "$TEST_DIR/clip.mp4" "Output file is created"
assert_file_exists "$TEST_DIR/clip_001.mp4" "Source file 1 kept with --keep"
assert_file_exists "$TEST_DIR/clip_002.mp4" "Source file 2 kept with --keep"
# ゴミ箱移動ログが出ていないこと
if [[ "$output" == *"元ファイルをゴミ箱へ移動中"* ]]; then
  printf '✗ --keep should not print trash message\n'
  return 1
else
  printf '✓ No trash message with --keep\n'
fi

# ============================================================
# Test 3: ディレクトリモードのデフォルトゴミ箱移動（既存挙動の維持）
# ============================================================
printf '\n## Test 3: Directory mode trashes by default (unchanged)\n'
TEST_DIR="$TEST_TMP/cleanup_3"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/show_001.mp4"
echo "video 2" > "$TEST_DIR/show_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Directory mode succeeds"
assert_file_exists "$TEST_DIR/show.mp4" "Output file is created"
assert_file_not_exists "$TEST_DIR/show_001.mp4" "Source file 1 trashed in directory mode"
assert_file_not_exists "$TEST_DIR/show_002.mp4" "Source file 2 trashed in directory mode"

# ============================================================
# Test 4: ディレクトリモードで --keep が機能する
# ============================================================
printf '\n## Test 4: Directory mode with --keep preserves source files\n'
TEST_DIR="$TEST_TMP/cleanup_4"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/show_001.mp4"
echo "video 2" > "$TEST_DIR/show_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat --keep "$TEST_DIR" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Directory mode with --keep succeeds"
assert_file_exists "$TEST_DIR/show.mp4" "Output file is created"
assert_file_exists "$TEST_DIR/show_001.mp4" "Source file 1 kept with --keep"
assert_file_exists "$TEST_DIR/show_002.mp4" "Source file 2 kept with --keep"

# ============================================================
# Test 5: --dryrun はゴミ箱へ移動しない（--keep なしでも）
# ============================================================
printf '\n## Test 5: --dryrun does not trash source files\n'
TEST_DIR="$TEST_TMP/cleanup_5"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/demo_001.mp4"
echo "video 2" > "$TEST_DIR/demo_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat --dryrun "$TEST_DIR/demo_001.mp4" "$TEST_DIR/demo_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Dryrun succeeds"
assert_file_exists "$TEST_DIR/demo_001.mp4" "Source file 1 not trashed in dryrun"
assert_file_exists "$TEST_DIR/demo_002.mp4" "Source file 2 not trashed in dryrun"

# ============================================================
# Test 6: マルチグループモードで各グループの元ファイルがゴミ箱へ移動される
# ============================================================
printf '\n## Test 6: Multi-group mode trashes source files for all groups\n'
TEST_DIR="$TEST_TMP/cleanup_6"
mkdir -p "$TEST_DIR"
echo "a1" > "$TEST_DIR/alpha_001.mp4"
echo "a2" > "$TEST_DIR/alpha_002.mp4"
echo "b1" > "$TEST_DIR/beta_001.mp4"
echo "b2" > "$TEST_DIR/beta_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/alpha_001.mp4" "$TEST_DIR/alpha_002.mp4" "$TEST_DIR/beta_001.mp4" "$TEST_DIR/beta_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Multi-group mode succeeds"
assert_file_exists "$TEST_DIR/alpha.mp4" "alpha group output created"
assert_file_exists "$TEST_DIR/beta.mp4" "beta group output created"
assert_file_not_exists "$TEST_DIR/alpha_001.mp4" "alpha_001 trashed"
assert_file_not_exists "$TEST_DIR/alpha_002.mp4" "alpha_002 trashed"
assert_file_not_exists "$TEST_DIR/beta_001.mp4" "beta_001 trashed"
assert_file_not_exists "$TEST_DIR/beta_002.mp4" "beta_002 trashed"
assert_in_mock_trash "alpha_001.mp4" "alpha_001 in trash"
assert_in_mock_trash "alpha_002.mp4" "alpha_002 in trash"
assert_in_mock_trash "beta_001.mp4" "beta_001 in trash"
assert_in_mock_trash "beta_002.mp4" "beta_002 in trash"

# ============================================================
# Test 7: マルチグループモード + --keep は元ファイルを残す
# ============================================================
printf '\n## Test 7: Multi-group mode with --keep preserves source files\n'
TEST_DIR="$TEST_TMP/cleanup_7"
mkdir -p "$TEST_DIR"
echo "a1" > "$TEST_DIR/alpha_001.mp4"
echo "a2" > "$TEST_DIR/alpha_002.mp4"
echo "b1" > "$TEST_DIR/beta_001.mp4"
echo "b2" > "$TEST_DIR/beta_002.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat --keep "$TEST_DIR/alpha_001.mp4" "$TEST_DIR/alpha_002.mp4" "$TEST_DIR/beta_001.mp4" "$TEST_DIR/beta_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Multi-group with --keep succeeds"
assert_file_exists "$TEST_DIR/alpha.mp4" "alpha output created"
assert_file_exists "$TEST_DIR/beta.mp4" "beta output created"
assert_file_exists "$TEST_DIR/alpha_001.mp4" "alpha_001 preserved with --keep"
assert_file_exists "$TEST_DIR/alpha_002.mp4" "alpha_002 preserved with --keep"
assert_file_exists "$TEST_DIR/beta_001.mp4" "beta_001 preserved with --keep"
assert_file_exists "$TEST_DIR/beta_002.mp4" "beta_002 preserved with --keep"

# ============================================================
# Test 8: 既存出力でスキップされた場合はゴミ箱へ移動しない
# ============================================================
printf '\n## Test 8: Skip case (existing output) does not trash source files\n'
TEST_DIR="$TEST_TMP/cleanup_8"
mkdir -p "$TEST_DIR"
echo "video 1" > "$TEST_DIR/movie_001.mp4"
echo "video 2" > "$TEST_DIR/movie_002.mp4"
echo "existing" > "$TEST_DIR/movie.mp4"   # 既存出力
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/movie_001.mp4" "$TEST_DIR/movie_002.mp4" 2>&1)
exit_code=$?
setopt err_exit
assert_exit_code "0" "$exit_code" "Skip case returns success"
assert_contains "$output" "SKIP 既存" "Reports skip for existing output"
assert_file_exists "$TEST_DIR/movie_001.mp4" "Source file 1 kept when output existed (skip)"
assert_file_exists "$TEST_DIR/movie_002.mp4" "Source file 2 kept when output existed (skip)"

# ============================================================
# Test 9: 結合失敗時は元ファイルをゴミ箱へ移動しない
# ============================================================
printf '\n## Test 9: Failed concat does not trash source files\n'
TEST_DIR="$TEST_TMP/cleanup_9"
mkdir -p "$TEST_DIR"
# サフィックス不一致で失敗させる（前処理バリデーションで落ちる）
echo "video 1" > "$TEST_DIR/mix-1-enc.mp4"
echo "video 2" > "$TEST_DIR/mix-2-raw.mp4"
cd "$TEST_DIR"
unsetopt err_exit
output=$(concat "$TEST_DIR/mix-1-enc.mp4" "$TEST_DIR/mix-2-raw.mp4" 2>&1)
exit_code=$?
setopt err_exit
# 失敗することを期待
if (( exit_code == 0 )); then
  printf '✗ concat should have failed but succeeded\n'
  return 1
fi
assert_file_exists "$TEST_DIR/mix-1-enc.mp4" "Source file 1 kept on failure"
assert_file_exists "$TEST_DIR/mix-2-raw.mp4" "Source file 2 kept on failure"

printf '\n=== Cleanup Behavior Tests Completed ===\n'
