#!/usr/bin/env zsh
# shellcheck shell=bash
# __concat_frame_hash のシーク引数リグレッションテスト
#
# 目的: 目的 PTS から 1ms 引いた値で -ss するロジック (_EPS=0.001) が
# 壊れていないことを保証する。
#
# 背景コミット: 6eac68b
#   -ss に目的 PTS を直接渡すと FP 丸めで隣フレームに超過する問題。
#   目的 PTS から 1ms 引いたものを _target とし、そこから 2 段階シークする。

source "${0:A:h}/test_helper.sh"

printf '\n=== concat Frame Hash Seek Epsilon Tests ===\n\n'

# ffmpeg モックを引数記録型に差し替え
# （test_helper.sh のデフォルトモックを上書き）
ARGS_LOG="$TEST_TMP/ffmpeg_seek_args.log"
cat > "$MOCK_BIN_DIR/ffmpeg" <<EOF
#!/usr/bin/env sh
# 引数一行で記録
printf '%s\n' "\$*" >> "$ARGS_LOG"
# ffmpeg は pipe:1 へ raw video を吐くので、何らかのバイト列を stdout へ
printf 'mock_frame_data'
exit 0
EOF
chmod +x "$MOCK_BIN_DIR/ffmpeg"

# テスト用のダミー入力ファイル
DUMMY_FILE="$TEST_TMP/fake.mp4"
touch "$DUMMY_FILE"

# ============================================================
# Test 1: timestamp=10.000 → _target=9.999, _approx=4.999, _fine=5.000
# ============================================================
printf '## Test 1: Standard case (timestamp=10.000)\n'
: > "$ARGS_LOG"
__concat_frame_hash "$DUMMY_FILE" "10.000" > /dev/null
args=$(cat "$ARGS_LOG")
assert_contains "$args" "-ss 4.999 " "approx seek is (target-1ms) - 5s = 4.999"
assert_contains "$args" "-ss 5.000 " "fine seek covers remaining 5s"

# ============================================================
# Test 2: 実際の誤検知ケース(timestamp=4312.332)
# ============================================================
printf '\n## Test 2: Real-world failing case (timestamp=4312.332)\n'
: > "$ARGS_LOG"
__concat_frame_hash "$DUMMY_FILE" "4312.332" > /dev/null
args=$(cat "$ARGS_LOG")
# _target = 4312.332 - 0.001 = 4312.331
# _approx = 4312.331 - 5 = 4307.331
# _fine = 4312.331 - 4307.331 = 5.000
assert_contains "$args" "-ss 4307.331 " "approx seek for 4312.332 is 4307.331 (1ms shifted)"
assert_contains "$args" "-ss 5.000 " "fine seek is 5.000"

# ============================================================
# Test 3: epsilon が効いていることを確認(epsilon 欠損だと -ss 4312.332 が入る)
# ============================================================
printf '\n## Test 3: Epsilon is actually applied (no raw timestamp leak)\n'
: > "$ARGS_LOG"
__concat_frame_hash "$DUMMY_FILE" "4312.332" > /dev/null
args=$(cat "$ARGS_LOG")
# 生の値 4312.332 や 4307.332 が seek 引数として現れたら epsilon が壊れている
if [[ "$args" == *"-ss 4307.332"* ]] || [[ "$args" == *"-ss 4312.332"* ]]; then
  printf '✗ Epsilon not applied: raw timestamp leaked into seek args\n  args: %s\n' "$args"
  return 1
else
  printf '✓ Epsilon is applied (raw timestamp not seen in seek args)\n'
fi

# ============================================================
# Test 4: 小さい timestamp でも負値シークにならない
# ============================================================
printf '\n## Test 4: Small timestamp clamped safely (timestamp=0.500)\n'
: > "$ARGS_LOG"
__concat_frame_hash "$DUMMY_FILE" "0.500" > /dev/null
args=$(cat "$ARGS_LOG")
# _target = 0.499, _approx = max(0, -4.501) = 0.000, _fine = 0.499
assert_contains "$args" "-ss 0.000 " "approx seek clamped to 0 for small timestamp"
assert_contains "$args" "-ss 0.499 " "fine seek carries the full offset"
# 負値が紛れ込んでいないこと
if [[ "$args" == *"-ss -"* ]]; then
  printf '✗ Negative seek value detected: %s\n' "$args"
  return 1
else
  printf '✓ No negative seek values\n'
fi

# ============================================================
# Test 5: timestamp=0 の極端なケース
# ============================================================
printf '\n## Test 5: Zero timestamp (edge case)\n'
: > "$ARGS_LOG"
__concat_frame_hash "$DUMMY_FILE" "0" > /dev/null
args=$(cat "$ARGS_LOG")
# _target = max(0, -0.001) = 0.000, _approx = 0.000, _fine = 0.000
# いずれも負にならないこと
if [[ "$args" == *"-ss -"* ]]; then
  printf '✗ Negative seek value at t=0: %s\n' "$args"
  return 1
else
  printf '✓ t=0 produces no negative seek\n'
fi

# ============================================================
# Test 6: ffmpeg が空出力 → 空ハッシュ(偽一致防止)
# ============================================================
printf '\n## Test 6: Empty ffmpeg output returns empty hash\n'
# 空出力の ffmpeg モックに差し替え
cat > "$MOCK_BIN_DIR/ffmpeg" <<'EOF'
#!/usr/bin/env sh
exit 0
EOF
chmod +x "$MOCK_BIN_DIR/ffmpeg"

# __concat_frame_hash は空出力時に exit 1 を返すので err_exit を一時解除
unsetopt err_exit
hash_result=$(__concat_frame_hash "$DUMMY_FILE" "10.000")
hash_exit=$?
setopt err_exit

if [[ -z "$hash_result" ]]; then
  printf '✓ Empty ffmpeg output yields empty hash\n'
else
  printf '✗ Expected empty hash, got: %s\n' "$hash_result"
  exit 1
fi
assert_exit_code "1" "$hash_exit" "Empty output returns non-zero exit"

printf '\n=== Frame Hash Seek Epsilon Tests Completed ===\n'
