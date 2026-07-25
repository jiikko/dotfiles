#!/usr/bin/env zsh
# zshlib/_git_prompt.zsh (プロンプトの git ブランチ表示。vcs_info の fork ゼロ置換) のテスト。
# 使い捨て repo を作り、ブランチ / subdir / detached / worktree / rebase / merge の各状態で
# 表示文字列が git の実状態と一致することを確認する。

set -euo pipefail
unset CDPATH

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)

TMP_ROOT=$(mktemp -d)
cleanup() { rm -rf "$TMP_ROOT"; }
trap cleanup EXIT

source "$ROOT_DIR/zshlib/_git_prompt.zsh"

fails=0
check() {
  local label="$1" want="$2"
  _dotfiles_git_branch || REPLY=""
  if [[ "$REPLY" == "$want" ]]; then
    print "✓ $label: [$REPLY]"
  else
    print -u2 "✗ $label: got [$REPLY], want [$want]"
    fails=$(( fails + 1 ))
  fi
}

export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null
cd "$TMP_ROOT"
git init -q -b main r
cd r
git config user.email t@example.com
git config user.name tester
print one > a; git add a; git commit -qm one
print two > a; git commit -qam two

check "ブランチ (repo ルート)" "main"

mkdir -p deep/nest
cd deep/nest
check "サブディレクトリから" "main"
cd "$TMP_ROOT/r"

# repo 外では空 (装飾も空文字列)
cd "$TMP_ROOT"
check "repo 外" ""
_dotfiles_git_prompt
if [[ -n "$_DOTFILES_GIT_PROMPT" ]]; then
  print -u2 "✗ repo 外で装飾文字列が空でない: $_DOTFILES_GIT_PROMPT"
  fails=$(( fails + 1 ))
else
  print "✓ repo 外の装飾文字列は空"
fi
cd "$TMP_ROOT/r"

# detached HEAD は短縮 SHA 7 桁
sha=$(git rev-parse --short=7 HEAD)
git checkout -q --detach HEAD
check "detached HEAD (短縮 SHA)" "$sha"
git checkout -q main

# worktree は .git が「gitdir: <path>」のテキストファイルになる
git worktree add -q "$TMP_ROOT/wt" -b wtbranch
cd "$TMP_ROOT/wt"
check "worktree (.git ファイル)" "wtbranch"
cd "$TMP_ROOT/r"

# rebase 停止中: HEAD は detached だが rebase-merge/head-name に元ブランチが残る
git checkout -q -b topic HEAD~1
print conflict > a; git commit -qam topic
git rebase main >/dev/null 2>&1 || true
check "rebase 停止中" "topic|REBASE"
git rebase --abort 2>/dev/null || true

# merge 競合中: MERGE_HEAD の存在で判定
git checkout -q main
git merge topic >/dev/null 2>&1 || true
check "merge 競合中" "main|MERGE"
git merge --abort 2>/dev/null || true
check "merge abort 後" "main"

# 装飾は vcs_info 時代の配色 (黒字 + 緑地の [branch]) を踏襲する
_dotfiles_git_prompt
if [[ "$_DOTFILES_GIT_PROMPT" != '%F{black}%K{green}[main]%f%k' ]]; then
  print -u2 "✗ 装飾文字列が想定と違う: $_DOTFILES_GIT_PROMPT"
  fails=$(( fails + 1 ))
else
  print "✓ 装飾文字列 (黒字/緑地)"
fi

# fork ゼロであること: サブプロセスを起動していたら 1 呼び出しで 1ms は下らない
zmodload zsh/datetime
s=$EPOCHREALTIME
for _ in {1..200}; do _dotfiles_git_prompt; done
e=$EPOCHREALTIME
per_ms=$(( (e - s) * 1000 / 200 ))
if (( per_ms > 1.0 )); then
  print -u2 "✗ 1 呼び出し ${per_ms}ms — fork が混ざっている可能性 (期待 <1ms)"
  fails=$(( fails + 1 ))
else
  printf "✓ fork ゼロ相当の速度 (%.3f ms/call)\n" "$per_ms"
fi

if (( fails > 0 )); then
  print -u2 "[test-git-prompt] $fails 件失敗"
  exit 1
fi
print "[test-git-prompt] すべて成功"
