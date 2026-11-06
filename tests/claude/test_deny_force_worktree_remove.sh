#!/usr/bin/env bash
# shellcheck disable=SC2016 # 検査に渡すコマンドの文字は展開させずにそのまま書く ($W を含む)
# _claude/hooks/deny-force-worktree-remove.sh (PreToolUse(Bash): git worktree remove の --force を deny) の unit テスト。
#
# なぜ: --force は未コミットの変更ごと worktree を消す。hook は --force だけを止め、汚れているかの判定は git に任せる。
# その前提 (--force 無しなら git が汚れた worktree を拒否し、ignore されたファイルだけなら消せる) も本物の git で固定する。
# 脅威モデルと「検出しない形」はフック冒頭が正本。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/deny-force-worktree-remove.sh"
SETTINGS="$ROOT_DIR/_claude/settings.json"
HOOK_TIMEOUT=10 # 本番の配線 (settings.json) と同じ上限
# shellcheck source=bin/lib/runtimeout.sh
. "$ROOT_DIR/bin/lib/runtimeout.sh"
runtimeout_resolve "$ROOT_DIR" || exit 1
# 継承した GIT_DIR などが残ると、使い捨ての repo でなく継承した repo を書き換える (sandbox-real-destructive-test-apis.md)
# shellcheck source=bin/lib/git_location_env.sh
. "$ROOT_DIR/bin/lib/git_location_env.sh"
git_location_env_unset || { printf '✗ git の場所の環境変数を外せない\n'; exit 1; }
[ -x "$HOOK" ] || { printf '✗ フック本体が無い / 実行権限が無い: %s\n' "$HOOK"; exit 1; }
command -v jq >/dev/null 2>&1 || { printf '✗ jq が無いため判定を検査できない (フック自体も jq 不在時は無効)\n'; exit 1; }
jq -e '[.hooks.PreToolUse[]? | select((.matcher // "") | test("Bash")) | .hooks[]?.command // ""]
       | map(test("deny-force-worktree-remove")) | any' "$SETTINGS" > /dev/null \
  || { printf '✗ settings.json の PreToolUse(Bash) にフックが配線されていない (防御が丸ごと無効)\n'; exit 1; }
printf '✓ settings.json の PreToolUse(Bash) に配線されている\n'

ok=0; fail=0
decide() { # $1=コマンド → deny / allow / error
  local out rc
  out=$(printf '%s' "$1" | jq -Rs '{tool_name: "Bash", tool_input: {command: .}}' | "$RUNTIMEOUT" "$HOOK_TIMEOUT" "$HOOK" 2>&1)
  rc=$?
  if [ "$rc" -ne 0 ]; then echo "error rc=$rc"; return; fi
  if [ -z "$out" ]; then echo allow; return; fi
  printf '%s' "$out" | jq -r '.hookSpecificOutput.permissionDecision // "error"' 2>/dev/null || echo error
}
expect() { # $1=期待 $2=説明 $3=コマンド
  local got; got=$(decide "$3")
  if [ "$got" = "$1" ]; then echo "✓ $2"; ok=$((ok+1)); else echo "✗ $2: $got (want $1)"; fail=$((fail+1)); fi
}

expect deny  "--force を止める" 'git worktree remove --force /tmp/wt'
expect deny  "-C の付いた git でも止める" 'git -C /repo worktree remove /tmp/wt --force'
expect deny  "rm と -f でも止める" 'git worktree rm -f /tmp/wt'
expect deny  "-ff (lock の解除) も止める" 'git worktree remove -ff /tmp/wt'
expect deny  "--force=… も止める" 'git worktree remove --force=true /tmp/wt'
expect deny  "/usr/bin/git でも止める" '/usr/bin/git worktree remove --force /tmp/wt'
expect deny  "; で繋いだ後ろでも止める (起点の形)" 'git rebase origin/master && git commit -m x; git push; git worktree remove --force "$W"'
expect deny  "&& で繋いだ後ろでも止める" 'git push origin HEAD:master && git -C ~/dotfiles worktree remove --force /tmp/wt'
expect deny  "行の継続をまたいでも止める" $'git worktree remove \\\n  --force /tmp/wt'
expect deny  "括弧の中でも止める" '(cd /x && git worktree remove --force /tmp/wt)'
expect deny  "コマンド置換の中でも止める" 'out=$(git worktree remove --force /tmp/wt)'
expect deny  "バッククォートの中でも止める" 'echo `git worktree remove --force /tmp/wt`'
expect deny  "bash -c の引用符の中でも止める" "bash -c 'git worktree remove --force /tmp/wt'"
expect deny  "引用符で囲んだ --force も止める" 'git worktree remove "--force" /tmp/wt'
expect deny  "git が受ける省略形 (--forc) も止める" 'git worktree remove --forc /tmp/wt'
expect deny  "-c status.showUntrackedFiles=no も止める (git が untracked ごと消す)" 'git -c status.showUntrackedFiles=no worktree remove /tmp/wt'
expect allow "git で終わる別の語は git と読まない" 'digit worktree remove --force /tmp/wt'
expect deny  "--config-env で status.showUntrackedFiles を変える形も止める" 'git --config-env=status.showUntrackedFiles=V worktree remove /tmp/wt'
expect deny  "前置きの GIT_CONFIG_KEY で status.showUntrackedFiles を変える形も止める" 'GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=status.showUntrackedFiles GIT_CONFIG_VALUE_0=no git worktree remove /tmp/wt'
expect deny  "大域オプション (-C / -c) の後ろの worktree も見る" 'git -C /repo -c core.x=y worktree remove --force /tmp/wt'
expect deny  "引用符で囲んだサブコマンドでも止める" 'git "worktree" remove --force /tmp/wt'
expect deny  "引用符で囲んだ remove でも止める" "git worktree 'rm' -f /tmp/wt"
expect deny  "受ける偽陽性: 引用符の中の検索語 (git grep) も止める" 'git grep -n "worktree remove --force"'
expect allow "語を崩した検索は通す" "git grep -n 'worktree.remove'"
expect deny  "-c core.excludesFile も止める (git が untracked を ignore と読んで消す)" 'git -c core.excludesFile=/tmp/all worktree remove /tmp/wt'
expect deny  "-c include.path も止める" 'git -c include.path=/tmp/cfg worktree remove /tmp/wt'
expect deny  "HOME の前置きも止める (global の設定が変わる)" 'HOME=/tmp/h git worktree remove /tmp/wt'
expect deny  "-C の値にコマンド置換があっても後ろの worktree を見る" 'git -C "$(git rev-parse --show-toplevel)" worktree remove --force ../wt'
expect deny  "値を空白で区切る大域オプションの後ろでも止める" 'git --attr-source HEAD worktree remove --force /tmp/wt'
expect deny  "--config-env の値を空白で区切っても止める" 'git --config-env status.showUntrackedFiles=V worktree remove /tmp/wt'
expect deny  "git で終わる前置きの語の後ろの git でも止める" 'FOO=/a/git git worktree remove --force /tmp/wt'
expect deny  "引用符で囲んだ GIT_CONFIG_KEY の値も止める" 'GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0="status.showUntrackedFiles" GIT_CONFIG_VALUE_0=no git worktree remove /tmp/wt'
expect deny  "GIT_CONFIG_PARAMETERS でも止める" "GIT_CONFIG_PARAMETERS=\"'status.showuntrackedfiles'='no'\" git worktree remove /tmp/wt"
expect deny  "-c の鍵を引用符で囲んでも止める" 'git -c "status.showUntrackedFiles"=no worktree remove /tmp/wt'
expect deny  "設定ファイルを差し替える前置きも止める" 'GIT_CONFIG_GLOBAL=/tmp/cfg git worktree remove /tmp/wt'
expect allow "--force の無い remove は通す (汚れていれば git が拒否する)" 'git worktree remove /tmp/wt'
expect allow "worktree の一覧は通す" 'git worktree list --porcelain'
expect allow "worktree add は通す" 'git worktree add --detach /tmp/wt origin/master'
expect allow "別の区切りの rm -f は通す" 'git worktree remove /tmp/wt && rm -f /tmp/log'
expect allow "heredoc の本文の文言は通す" $'git commit -F - <<\'M\' -- a\ngit worktree remove --force x\nM'
expect allow "worktree の語が無ければ通す" 'git push --force-with-lease'

# 前提: --force 無しなら git が汚れた worktree を拒否し、ignore されたファイルだけなら消せる
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
repo="$WORK/repo"
g() { git -C "$repo" -c user.name=t -c user.email=t@t -c commit.gpgsign=false "$@" >/dev/null 2>&1; }
mkdir -p "$repo"
g init -q -b master
printf 'tmp/\n' > "$repo/.gitignore"
g add .gitignore
g commit -q -m base
g worktree add -q --detach "$WORK/clean"
mkdir -p "$WORK/clean/tmp" && echo x > "$WORK/clean/tmp/log"
if git -C "$repo" worktree remove "$WORK/clean" >/dev/null 2>&1 && [ ! -e "$WORK/clean" ]; then
  echo "✓ 前提: ignore されたファイルだけの worktree は --force 無しで消せる"; ok=$((ok+1))
else
  echo "✗ 前提: ignore されたファイルだけの worktree を --force 無しで消せない (手順書の --force を外せない)"; fail=$((fail+1))
fi
for kind in untracked modified; do
  g worktree add -q --detach "$WORK/$kind"
  if [ "$kind" = untracked ]; then echo y > "$WORK/$kind/new.txt"; else echo z >> "$WORK/$kind/.gitignore"; fi
  if ! git -C "$repo" worktree remove "$WORK/$kind" >/dev/null 2>&1 && [ -e "$WORK/$kind" ]; then
    echo "✓ 前提: $kind のある worktree は --force 無しでは git が拒否する"; ok=$((ok+1))
  else
    echo "✗ 前提: $kind のある worktree を --force 無しで消せた (hook が --force だけを止める前提が崩れた)"; fail=$((fail+1))
  fi
done

# 前提: dotfiles が配る _gitconfig は status.showUntrackedFiles を立てない (立てると --force 無しでも untracked ごと消える)
if grep -qi 'showUntrackedFiles' "$ROOT_DIR/_gitconfig"; then
  echo "✗ 前提: _gitconfig が status.showUntrackedFiles を設定している (--force 無しの削除が untracked を守らない)"; fail=$((fail+1))
else
  echo "✓ 前提: _gitconfig は status.showUntrackedFiles を設定しない"; ok=$((ok+1))
fi

echo "---"
echo "ok=$ok fail=$fail"
[ "$fail" -eq 0 ]
