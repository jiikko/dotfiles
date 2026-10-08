#!/usr/bin/env zsh
unset CDPATH
# shellcheck shell=bash
# concat の出力サイズの診断のうち、入力の末尾の「どの box からも参照されないデータ」を見分ける経路の回帰テスト (issue 670)
#
# なぜ: 出力が入力の合計より 5% 以上小さいとき、__concat_diagnose_output は入力ごとの実効サイズ
# (__concat_mp4_effective_size → bin/mp4-effective-size。壊れていないトップレベルの box の合計) を測り、差が 10MB を超える入力を
# 「未参照データ」として挙げる。実効サイズの合計で見て出力が正常なら、失敗 (rc=1) ではなく警告 (rc=2) にする。
# この経路にはテストが無く、実効サイズの計算 (以前は python3) が壊れても気づけなかった。
# 入力は 10MB の mdat の box の後ろに 11MB のゼロ (型が印字できない = 未参照) を足したもの。ゼロは疎なファイルにする (dd の seek)。

source "${0:A:h}/test_helper.sh"

# 実効サイズは Go の道具 (bin/mp4-effective-size) が測る。go が無い環境では測れないので、テストごと skip する (77。tests/CLAUDE.md)
if ! command -v go >/dev/null 2>&1; then
  printf 'skipped: go が無い (bin/mp4-effective-size をビルドできない)\n'
  exit 77
fi

TEST_DIR="$TEST_TMP/orphaned"
mkdir -p "$TEST_DIR"

printf '\n=== concat Orphaned Data Tests ===\n\n'

MB=1048576
# assert_eq <期待> <実際> <説明>: 値の一致 (assert_contains は「0」を 10485760 にも当ててしまう)
assert_eq() {
  if [[ "$1" == "$2" ]]; then printf '✓ %s\n' "$3"; else bad '✗ %s (expected: %s, got: %s)\n' "$3" "$1" "$2"; fi
}
# make_input <path> <末尾のごみ (バイト)>: 10MB の mdat の box + ごみ
make_input() {
  printf '\x00\xa0\x00\x00mdat' > "$1"
  dd if=/dev/null of="$1" bs=1 seek=$(( 10 * MB + $2 )) 2>/dev/null
}
# make_output <path> <バイト>
make_output() {
  : > "$1"
  dd if=/dev/null of="$1" bs=1 seek="$2" 2>/dev/null
}

make_input "$TEST_DIR/a.mp4" $(( 11 * MB ))
make_input "$TEST_DIR/b.mp4" $(( 11 * MB ))
make_input "$TEST_DIR/clean.mp4" 0
size_ab=$(( $(stat -f%z "$TEST_DIR/a.mp4") + $(stat -f%z "$TEST_DIR/b.mp4") ))

# --- 1. 実効サイズを測れる: 道具の値 --------------------------------------------
assert_eq "$(( 10 * MB ))" "$(__concat_mp4_effective_size "$TEST_DIR/a.mp4")" "実効サイズは末尾のごみを数えない (10MB)"

# --- 2. 入力の末尾にごみがあり、出力は実効サイズの合計どおり: 警告 (rc=2) --------
make_output "$TEST_DIR/out.mp4" $(( 20 * MB ))
unsetopt err_exit
__concat_diagnose_output "$TEST_DIR/out.mp4" "" 1 "$size_ab" "$TEST_DIR/a.mp4" "$TEST_DIR/b.mp4"
rc=$?
setopt err_exit
assert_exit_code 2 "$rc" "未参照データがあって出力が実効サイズどおりなら警告 (rc=2)"
assert_contains "$REPLY" "a.mp4: 未参照データ 11MB" "未参照データのある入力と大きさを挙げる"
assert_contains "$REPLY" "影響はありません" "出力は正常だと伝える"

# --- 3. ごみの無い入力で出力が小さい: 失敗 (rc=1)。未参照データは挙げない ---------
make_output "$TEST_DIR/small.mp4" $(( 5 * MB ))
unsetopt err_exit
__concat_diagnose_output "$TEST_DIR/small.mp4" "" 1 $(( 20 * MB )) "$TEST_DIR/clean.mp4" "$TEST_DIR/clean.mp4"
rc=$?
setopt err_exit
assert_exit_code 1 "$rc" "ごみの無い入力で出力が小さいなら失敗 (rc=1)"
assert_not_contains "$REPLY" "未参照データ" "ごみの無い入力を未参照データとして挙げない"

# --- 4. 道具を起動できない: 実効サイズ 0 として扱い、警告に落とさず失敗 (rc=1) -------
saved_bin="$__CONCAT_DOTFILES_BIN"
__CONCAT_DOTFILES_BIN="$TEST_DIR/no-such-bin"
assert_eq 0 "$(__concat_mp4_effective_size "$TEST_DIR/a.mp4")" "道具が無ければ 0"
unsetopt err_exit
__concat_diagnose_output "$TEST_DIR/out.mp4" "" 1 "$size_ab" "$TEST_DIR/a.mp4" "$TEST_DIR/b.mp4"
rc=$?
setopt err_exit
__CONCAT_DOTFILES_BIN="$saved_bin"
assert_exit_code 1 "$rc" "実効サイズを測れなければ、警告 (出力は正常) と言わずに失敗のまま"
assert_contains "$REPLY" "入力の実効サイズを測れなかった" "測れなかったことを理由に添える (旧の python3 と違い go が要るため)"

printf '\n=== Orphaned Data Tests Completed ===\n'
