#!/usr/bin/env bash
# scripts/tmux_version_gte.sh の unit テスト。
#
# 版数ガードは _tmux.conf の起動時チェック (.tmux-version) と機能単位ガード
# (3.6 scrollbars / 3.7 separator 等の if-shell) の両方が依存する判定器なのに
# テストが無く、比較ロジックの回帰 (例: 3.10 を 3.9 より小さいと誤判定する
# 文字列比較化) が起きても気づけなかった。ここで判定だけをスタブで固定する:
#   - サーバ版数は `tmux display-message -p #{version}` で取る (tmux -V ではない)
#     ため、PATH 先頭に置いた tmux スタブで任意の版数を注入できる
#   - 実 tmux サーバは起動しない (壊れやすい実サーバ系テストとは独立)
#
# 注意: 要求版数の major/minor 抽出は「サフィックス除去」(3.7b → 3.7) を含む。
# minor が 2 桁になった場合 (3.10) の数値比較もここで pin する。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/tmux_version_gte.sh"
TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

if [[ ! -x "$SCRIPT" ]]; then
  printf '✗ スクリプトが存在しない/実行不可: %s\n' "$SCRIPT"
  exit 1
fi
printf '✓ %s exists (executable)\n' "${SCRIPT#"$ROOT_DIR"/}"

# PATH 先頭に tmux スタブを置き、#{version} 要求に $STUB_VERSION を返す。
mkdir -p "$TMP_DIR/bin"
cat > "$TMP_DIR/bin/tmux" <<'EOS'
#!/bin/sh
# display-message -p '#{version}' の呼び出しにだけ応える版数スタブ
printf '%s\n' "$STUB_VERSION"
EOS
chmod +x "$TMP_DIR/bin/tmux"

# 判定を 1 回走らせて ok/ng を返す
check() {  # $1=サーバ版数, 残り=要求 (引数なし = .tmux-version 経路)
  local server="$1"; shift
  if PATH="$TMP_DIR/bin:$PATH" STUB_VERSION="$server" "$SCRIPT" "$@" 2>/dev/null; then
    printf 'ok'
  else
    printf 'ng'
  fi
}

assert_check() {  # $1=期待(ok/ng) $2=説明 $3=サーバ版数 残り=要求引数
  local expect="$1" msg="$2"; shift 2
  local actual
  actual="$(check "$@")"
  if [[ "$actual" != "$expect" ]]; then
    printf '✗ %s\n  expected: %s / actual: %s (server=%s req=%s)\n' "$msg" "$expect" "$actual" "$1" "${*:2}"
    exit 1
  fi
  printf '✓ %s\n' "$msg"
}

printf '\n## 引数指定 (機能単位ガード)\n'
assert_check ok "同版数 (3.7 vs 要求 3.7) は満たす"            "3.7"  3 7
assert_check ok "サフィックス付き同版数 (3.7b) も満たす"        "3.7b" 3 7
assert_check ng "minor 不足 (3.6a vs 要求 3.7) は満たさない"    "3.6a" 3 7
assert_check ok "major 超過 (4.0 vs 要求 3.7) は満たす"         "4.0"  3 7
assert_check ng "major 不足 (2.9 vs 要求 3.7) は満たさない"     "2.9"  3 7
assert_check ok "2 桁 minor (3.10 vs 要求 3.7) を数値比較で満たす (文字列比較化の回帰防止)" "3.10" 3 7
assert_check ng "2 桁 minor の逆方向 (3.9 vs 要求 3.10) は満たさない" "3.9" 3 10

printf '\n## 引数なし (.tmux-version 単一情報源の起動時チェック)\n'
REQ="$(tr -d '[:space:]' < "$ROOT_DIR/.tmux-version")"
REQ_MAJ="${REQ%%.*}"
assert_check ok ".tmux-version ($REQ) と同版数のサーバは満たす"       "$REQ"
assert_check ng ".tmux-version ($REQ) より古いサーバ (0.9) は満たさない" "0.9"
assert_check ok ".tmux-version ($REQ) より新しい major ($((REQ_MAJ + 1)).0) は満たす" "$((REQ_MAJ + 1)).0"

printf '\n## 引数なし + サフィックス付き .tmux-version (req 側正規化の回帰ガード)\n'
# 本物の .tmux-version は触らず、一時 ROOT にスクリプトごとコピーして suffix 付き要求を
# 読ませる (スクリプトは $0 相対で ../.tmux-version を解決するためこれで注入できる)。
# req 側の正規化が消えると req_min="7b" が test に渡りエラー → 常に「不足」判定になる
# (サーバが要求を満たしていても全体ゲートが false になる回帰)。
mkdir -p "$TMP_DIR/suffix_root/scripts"
cp "$SCRIPT" "$TMP_DIR/suffix_root/scripts/"
printf '3.7b\n' > "$TMP_DIR/suffix_root/.tmux-version"
SUFFIX_SCRIPT="$TMP_DIR/suffix_root/scripts/tmux_version_gte.sh"
if PATH="$TMP_DIR/bin:$PATH" STUB_VERSION="3.7" "$SUFFIX_SCRIPT" 2>/dev/null; then
  printf '✓ サフィックス付き要求 (3.7b) をサーバ 3.7 が満たす (req 側正規化)\n'
else
  printf '✗ サフィックス付き要求 (3.7b) が常に不足判定になる (req 正規化の回帰)\n'
  exit 1
fi
if PATH="$TMP_DIR/bin:$PATH" STUB_VERSION="3.6a" "$SUFFIX_SCRIPT" 2>/dev/null; then
  printf '✗ サーバ 3.6a がサフィックス付き要求 (3.7b) を満たしてしまう\n'
  exit 1
else
  printf '✓ サーバ 3.6a はサフィックス付き要求 (3.7b) を満たさない\n'
fi

printf '\nAll version-gte tests passed successfully!\n'
