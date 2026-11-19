#!/usr/bin/env bash
# devtool/bin/mutate (読み手) が、obaket の本物の書き手 (macOS/bin/run-logged-command・run-with-test-profile-residue) の出力を
# 今も読めるかを、obaket の check-mutate-format-contract にこの dotfiles の作業ツリーを読み手として渡して確かめる。
#
# 書き手の形の正本は obaket の道具で、突き合わせの検査も obaket の lint-gates にある。ここで回すのは、dotfiles の側だけ
# (読み手の正規表現・fixture) を直したときにも同じ検査を掛けるため (obaket の gate は ~/dotfiles を読むので、
# dotfiles の作業ツリーの変更は push して本体へ pull するまで見ない)。
# test_mutate.sh は形を手で写した行で試すので、obaket が形を変えたことには気づかない。
#
# obaket の checkout は OBAKET_ROOT (無ければ ~/src/my-products/apps/obaket)。検査の本体が無い (checkout が無い・古い。CI もこれ) なら
# 理由を出して exit 77 (skip)。obaket の checkout は読むだけで、書き換えない (検査は一時 dir の中で一時 repo を作る)。
set -uo pipefail
unset CDPATH

TEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT_DIR="$(cd "$TEST_DIR/../.." && pwd -P)"
# 継承した GIT_DIR などを外す (外さないと、下の obaket の HEAD の読み取りと gate の git が継承した repo を相手にする)
# shellcheck source=bin/lib/git_location_env.sh
. "$ROOT_DIR/bin/lib/git_location_env.sh" && git_location_env_unset || { echo "✗ 継承した GIT_DIR などを外せない"; exit 1; }
OBAKET="${OBAKET_ROOT:-$HOME/src/my-products/apps/obaket}"
GATE="$OBAKET/macOS/libexec/check/check-mutate-format-contract"

if [ ! -x "$GATE" ]; then
  echo "skipped: obaket の check-mutate-format-contract が無い (${GATE}。obaket の checkout が無いか古い。OBAKET_ROOT で指定できる)"
  exit 77
fi

out="$(mktemp)" || { echo "✗ 一時ファイルを作れない"; exit 1; }
trap 'rm -f "$out"' EXIT
rc=0
OBAKET_MUTATE_DOTFILES_ROOT="$ROOT_DIR" "$GATE" >"$out" 2>&1 || rc=$?
obaket_head="$(git -C "$OBAKET" rev-parse --short HEAD 2>&1)"
if [ "$rc" -ne 0 ]; then
  echo "✗ obaket ($OBAKET @ $obaket_head) の書き手の出力を、この dotfiles ($ROOT_DIR) の bin/mutate で読めない (rc=$rc):"
  sed 's/^/    /' "$out"
  exit 1
fi
# 検査が走った証拠: 最後の行 (rc 0 でも、Xcode Cloud の判定に入って飛ばした形を通さない)
if ! grep -qF "check-mutate-format-contract: 書き手の形を bin/mutate で読めた" "$out"; then
  echo "✗ obaket の検査が rc 0 で終わったが、突き合わせた行が無い (飛ばした?):"
  sed 's/^/    /' "$out"
  exit 1
fi
echo "✓ obaket ($OBAKET @ $obaket_head) の書き手の出力を、この dotfiles の bin/mutate で読めた"
