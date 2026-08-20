#!/usr/bin/env zsh
unset CDPATH
# shellcheck shell=bash
# av1ify --color-tags オプションテスト
# 目的: h264_nvenc 等が誤って埋め込む matrix_coefficient=0 (Identity, ffprobe表記 "gbr") を
#       auto モードでのみ bt709 に補正すること、bt709/off モードの明示挙動、
#       無効値のバリデーションを検証する。

source "${0:A:h}/test_helper.sh"

printf '\n=== av1ify --color-tags Tests ===\n\n'

# Test 1: デフォルト (未指定) は dry-run 上で auto と表示される
printf '## Test 1: Default color-tags is auto (dry-run)\n'
TEST_DIR="$TEST_TMP/test1"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(av1ify --dry-run "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "color-tags=auto" "Dry-run shows color-tags=auto by default"

# Test 2: --color-tags bt709 (dry-run)
printf '\n## Test 2: --color-tags bt709 (dry-run)\n'
TEST_DIR="$TEST_TMP/test2"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(av1ify --dry-run --color-tags bt709 "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "color-tags=bt709" "Dry-run shows color-tags=bt709"

# Test 3: --color-tags off (dry-run)
printf '\n## Test 3: --color-tags off (dry-run)\n'
TEST_DIR="$TEST_TMP/test3"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(av1ify --dry-run --color-tags off "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "color-tags=off" "Dry-run shows color-tags=off"

# Test 4: 無効な --color-tags 値はエラー終了
printf '\n## Test 4: Invalid --color-tags value (error exit)\n'
TEST_DIR="$TEST_TMP/test4"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
unsetopt err_exit
output=$(av1ify --dry-run --color-tags bogus "$TEST_DIR/input.avi" 2>&1)
exit_code=$?
setopt err_exit
assert_contains "$output" "無効なcolor-tags指定" "Reports invalid color-tags value"
(( exit_code != 0 )) && printf '✓ Exit code is non-zero for invalid --color-tags (%d)\n' "$exit_code" || printf '✗ Exit code should be non-zero (got %d)\n' "$exit_code"

# Test 5: auto モード + ソースが Identity (gbr) → bt709 に補正される (実エンコード)
printf '\n## Test 5: auto mode corrects Identity (gbr) source to bt709\n'
TEST_DIR="$TEST_TMP/test5"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(MOCK_COLOR_SPACE=gbr av1ify "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "bt709 へ補正" "Auto mode logs correction message for Identity source"
assert_file_exists "$TEST_DIR/input-enc.mp4" "Encode succeeds after color tag correction"

# Test 6: auto モード + ソースが bt709 → 補正メッセージなし (触らない)
printf '\n## Test 6: auto mode leaves already-correct source untouched\n'
TEST_DIR="$TEST_TMP/test6"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(MOCK_COLOR_SPACE=bt709 av1ify "$TEST_DIR/input.avi" 2>&1 || true)
assert_not_contains "$output" "bt709 へ補正" "Auto mode does not log correction for already-correct source"

# Test 7: bt709 モードはソースの値によらず常に上書き
printf '\n## Test 7: --color-tags bt709 always overrides regardless of source\n'
TEST_DIR="$TEST_TMP/test7"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(MOCK_COLOR_SPACE=smpte170m av1ify --color-tags bt709 "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "強制上書き" "bt709 mode always logs forced override"

# Test 8: off モードは Identity ソースでも一切上書きしない
printf '\n## Test 8: --color-tags off never overrides, even for Identity source\n'
TEST_DIR="$TEST_TMP/test8"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(MOCK_COLOR_SPACE=gbr av1ify --color-tags off "$TEST_DIR/input.avi" 2>&1 || true)
assert_not_contains "$output" "bt709 へ補正" "off mode does not correct Identity source"
assert_not_contains "$output" "強制上書き" "off mode does not force override"

# Test 9: AV1_COLOR_TAGS 環境変数が CLI 未指定時に適用される
printf '\n## Test 9: AV1_COLOR_TAGS env var applies when CLI option omitted\n'
TEST_DIR="$TEST_TMP/test9"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(AV1_COLOR_TAGS=off av1ify --dry-run "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "color-tags=off" "AV1_COLOR_TAGS env var reflected in dry-run plan"

# Test 10: CLI オプションが環境変数より優先される
printf '\n## Test 10: CLI --color-tags takes precedence over AV1_COLOR_TAGS\n'
TEST_DIR="$TEST_TMP/test10"
mkdir -p "$TEST_DIR"
echo "dummy video" > "$TEST_DIR/input.avi"
cd "$TEST_DIR"
output=$(AV1_COLOR_TAGS=off av1ify --dry-run --color-tags bt709 "$TEST_DIR/input.avi" 2>&1 || true)
assert_contains "$output" "color-tags=bt709" "CLI option overrides env var in dry-run plan"

printf '\n=== av1ify --color-tags Tests Complete ===\n\n'
