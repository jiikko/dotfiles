#!/usr/bin/env zsh
# _zshrc の _dotfiles_check_claude_links (~/.claude の per-file symlink 漏れ検出) のテスト。
#
# 検出したい失敗: repo の _claude/rules 等にファイルを足しても setup.sh を再実行するまで
# ~/.claude 配下にリンクが張られず、Claude がそのルール/スキルを読まない状態。

set -euo pipefail
unset CDPATH

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)

TMP_HOME=$(mktemp -d)
cleanup() { rm -rf "$TMP_HOME" }
trap cleanup EXIT

ln -s "$ROOT_DIR" "$TMP_HOME/dotfiles"

fails=0
# 関数だけを _zshrc から取り出して評価する (zshrc 全体の副作用を持ち込まない)。
# 関数定義の抽出は「行範囲」ではなく関数名で行う (行番号 pin を避ける)。
extract_fn() {
  awk '/^  _dotfiles_check_claude_links\(\) \{/ { inside = 1 }
       inside { print }
       inside && /^  \}$/ { exit }' "$ROOT_DIR/_zshrc"
}
fn=$(extract_fn)
if [[ -z "$fn" ]]; then
  print -u2 "✗ _zshrc から _dotfiles_check_claude_links を抽出できない (rename した?)"
  exit 1
fi

run_check() { HOME="$TMP_HOME" zsh -f -c "$fn"$'\n''_dotfiles_check_claude_links' }

# 1. ~/.claude が無いマシン (Claude Code 未導入) では黙る
out=$(run_check)
if [[ -z "$out" ]]; then
  print "✓ ~/.claude 不在なら何も言わない"
else
  print -u2 "✗ ~/.claude 不在で出力があった: $out"
  fails=$(( fails + 1 ))
fi

# 2. ~/.claude があってリンクが 1 つも無ければ不足を報告する
mkdir -p "$TMP_HOME/.claude"/{agents,commands,rules,hooks,skills,workflows}
out=$(run_check)
if [[ "$out" == *"リンクが"*"件不足"* && "$out" == *"setup.sh"* ]]; then
  print "✓ リンク不足を検出して setup.sh の再実行を促す"
else
  print -u2 "✗ リンク不足を検出できない: $out"
  fails=$(( fails + 1 ))
fi

# 3. setup.sh と同じ規約でリンクを張り切ったら黙る
#    (規約: agents/commands/rules/hooks は全ファイル / skills はディレクトリ / workflows は .js のみ)
for d in agents commands rules hooks; do
  for f in "$ROOT_DIR/_claude/$d"/*(N); do
    ln -sfn "$f" "$TMP_HOME/.claude/$d/${f:t}"
  done
done
for f in "$ROOT_DIR/_claude/skills"/*(N/); do
  ln -sfn "$f" "$TMP_HOME/.claude/skills/${f:t}"
done
for f in "$ROOT_DIR/_claude/workflows"/*.js(N); do
  ln -sfn "$f" "$TMP_HOME/.claude/workflows/${f:t}"
done
out=$(run_check)
if [[ -z "$out" ]]; then
  print "✓ 全リンクが揃っていれば黙る (誤検出しない)"
else
  print -u2 "✗ 揃っているのに不足を報告した: $out"
  fails=$(( fails + 1 ))
fi

# 4. 1 件消すと再び検出する (規約側の取りこぼしでなく実在チェックが効いていることの確認)
some_rule=$(print -r -- "$TMP_HOME/.claude/rules"/*(N[1]))
rm -f "$some_rule"
out=$(run_check)
if [[ "$out" == *"rules/${some_rule:t}"* ]]; then
  print "✓ 1 件削ると当該ファイル名を挙げて検出する"
else
  print -u2 "✗ 削除した ${some_rule:t} を検出できない: $out"
  fails=$(( fails + 1 ))
fi

if (( fails > 0 )); then
  print -u2 "[test-claude-link-check] $fails 件失敗"
  exit 1
fi
print "[test-claude-link-check] すべて成功"
