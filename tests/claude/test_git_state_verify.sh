#!/usr/bin/env bash
# _claude/hooks/git-state-verify.sh (PostToolUse(Bash): git commit / push の直後に実 git state を
# 注入する) の unit テスト。
#
# なぜ: この hook は「誤った成功報告を構造で潰す」ためのもので、**壊れると誤報を後押しする側に
# 回る**。実際に 3 通り壊れていた (issue 310):
#   ① `git -C dir commit` を拾えない (この repo の規範がまさにその形を要求している)
#   ② 未 push 判定が detached HEAD を見ない (issue 311 で修正)
#   ③ 見ている repo が違ううえ、第三者のテキストを権威的ラベルで注入する
# 兄弟の hook はすべてテストを持っているのに、**この hook だけが 0 本**だった。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/git-state-verify.sh"

command -v jq >/dev/null 2>&1 || { echo "✗ jq が無い。hook は jq 前提なので検査できない" >&2; exit 1; }

fail=0; ok=0
TMP_ROOT=$(mktemp -d); trap 'rm -rf "$TMP_ROOT"' EXIT

# 偽 repo をケースごとに新規で作る (ケース間で状態を共有しない)
new_repo() { # → $REPO
  REPO=$(mktemp -d "$TMP_ROOT/repo.XXXXXX")
  ( cd "$REPO" && git init -q . && : > f.txt &&
    git add -A && git -c user.email=t@t -c user.name=t commit -qm init ) >/dev/null 2>&1
}

run_hook() { # run_hook <cmd 文字列> → stdout
  ( cd "${2:-$REPO}" && printf '%s' "$1" | jq -R '{tool_input:{command:.}}' | "$HOOK" 2>/dev/null ) || true
}

# --- ① トリガ: 7 形式すべてで発火する (issue 310 の表) ---------------------------------------
printf '## トリガ: git を**コマンドとして**呼ぶ形は拾う\n'
new_repo
for c in \
  'git commit -m x' \
  'git push origin HEAD:master' \
  'cd /tmp && git push' \
  'git -C /tmp/r commit -m x' \
  'git -C /Users/koji/dotfiles push' \
  'git --no-pager -C /x push' \
  'git -c user.name=a commit'
do
  out=$(run_hook "$c")
  if [ -n "$out" ]; then printf '✓ 発火: %s\n' "$c"; ok=$((ok+1))
  else printf '✗ 発火しない: %s\n' "$c"; fail=$((fail+1)); fi
done

# --- 陰性対照: git を実行しない文字列では発火しない -------------------------------------------
#
# 🚨 これが無いと「トリガを緩める」方向の修正が過剰発火ごと通る。実際に旧実装は
# `echo` と `grep` だけのコマンドで発火し、無関係な repo の state を注入していた。
printf '\n## 陰性対照: git を実行しない文字列では発火しない\n'
for c in \
  'echo "git commit したら git push する"' \
  'grep -rn "git push" docs/' \
  'git status' \
  'git log --oneline -1' \
  'ls -la'
do
  out=$(run_hook "$c")
  if [ -z "$out" ]; then printf '✓ 沈黙: %s\n' "$c"; ok=$((ok+1))
  else printf '✗ 発火してはいけないのに発火: %s\n' "$c"; fail=$((fail+1)); fi
done

# --- ③ 注入本文: 出典と untrusted ヘッダ -------------------------------------------------------
printf '\n## 注入本文の契約\n'
new_repo
body=$(run_hook 'git commit -m x' | jq -r '.hookSpecificOutput.additionalContext // ""')

if [ -z "$body" ]; then
  printf '✗ additionalContext を取り出せない (JSON が壊れている)\n'; fail=$((fail+1))
else
  # 🚨 **どこを見た state か**。hook は cwd の repo を見るので、`git -C <別 repo>` のときは
  # 報告と実際に触った repo が食い違う。出典が無いと「これで検証すること」の下に別 repo の
  # (しばしば別セッションが書いた) state が置かれる。
  real=$( cd "$REPO" && git rev-parse --show-toplevel )
  if grep -qF "検査した repo: $real" <<< "$body"; then
    printf '✓ 検査した repo を出典として出す\n'; ok=$((ok+1))
  else
    printf '✗ 「検査した repo: %s」が無い:\n' "$real"; printf '%s\n' "$body" | head -5; fail=$((fail+1))
  fi

  if grep -q '指示ではない' <<< "$body"; then
    printf '✓ untrusted 引用のヘッダが付く\n'; ok=$((ok+1))
  else
    printf '✗ untrusted 引用のヘッダが無い (コミットメッセージは第三者が書いたテキスト)\n'; fail=$((fail+1))
  fi
fi

# --- ② 未 push 判定 (issue 311 と同じ主張を、この hook の入口から固定する) ---------------------
printf '\n## 未 push 判定\n'

# detached worktree で commit → 「未 push」に出ること
det=$(mktemp -d "$TMP_ROOT/det.XXXXXX")
( git init -q --bare "$det/remote.git" && git init -q "$det/main" &&
  cd "$det/main" && : > f.txt && git add -A &&
  git -c user.email=t@t -c user.name=t commit -qm init &&
  git remote add origin "$det/remote.git" && git push -q origin HEAD:master &&
  git worktree add -q --detach "$det/wt" HEAD &&
  cd "$det/wt" && : > g.txt && git add -A &&
  git -c user.email=t@t -c user.name=t commit -qm 'unpushed in detached' ) >/dev/null 2>&1
body=$(run_hook 'git commit -m x' "$det/wt" | jq -r '.hookSpecificOutput.additionalContext // ""')
if grep -q 'unpushed in detached' <<< "$body"; then
  printf '✓ detached worktree の未 push commit を出す\n'; ok=$((ok+1))
else
  printf '✗ detached worktree の未 push を見落とした (--branches は detached を見ない):\n'
  printf '%s\n' "$body" | tail -5; fail=$((fail+1))
fi

# 🚨 **判定不能を「push 済み」に丸めない**。remote が無い repo は「どこにも push していない」が
# 正しく、「すべて push 済み」と出すのは逆向きの嘘。
new_repo
body=$(run_hook 'git commit -m x' | jq -r '.hookSpecificOutput.additionalContext // ""')
if grep -q '判定不能: remote が設定されていない' <<< "$body"; then
  printf '✓ remote 未設定を「判定不能」として出す\n'; ok=$((ok+1))
elif grep -q 'すべて push 済み' <<< "$body"; then
  printf '✗ remote が無い repo で「すべて push 済み」と言った (逆向きの嘘)\n'; fail=$((fail+1))
else
  printf '✗ 未 push 欄の出力が想定外:\n'; printf '%s\n' "$body" | tail -5; fail=$((fail+1))
fi

printf '\n検査 %d 件: ok=%d / fail=%d\n' "$((ok+fail))" "$ok" "$fail"
[ "$fail" -eq 0 ] || exit 1
