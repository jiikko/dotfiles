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
cd "$TMP_ROOT" || exit 1
git init -q -b main r
cd r || exit 1
git config user.email t@example.com
git config user.name tester
print one > a; git add a; git commit -qm one
print two > a; git commit -qam two

check "ブランチ (repo ルート)" "main"

mkdir -p deep/nest
cd deep/nest || exit 1
check "サブディレクトリから" "main"
cd "$TMP_ROOT/r" || exit 1

# repo 外では空 (装飾も空文字列)
cd "$TMP_ROOT" || exit 1
check "repo 外" ""
_dotfiles_git_prompt
if [[ -n "$_DOTFILES_GIT_PROMPT" ]]; then
  print -u2 "✗ repo 外で装飾文字列が空でない: $_DOTFILES_GIT_PROMPT"
  fails=$(( fails + 1 ))
else
  print "✓ repo 外の装飾文字列は空"
fi
cd "$TMP_ROOT/r" || exit 1

# detached HEAD は短縮 SHA 7 桁
sha=$(git rev-parse --short=7 HEAD)
git checkout -q --detach HEAD
check "detached HEAD (短縮 SHA)" "$sha"
git checkout -q main

# worktree は .git が「gitdir: <path>」のテキストファイルになる
git worktree add -q "$TMP_ROOT/wt" -b wtbranch
cd "$TMP_ROOT/wt" || exit 1
check "worktree (.git ファイル)" "wtbranch"
cd "$TMP_ROOT/r" || exit 1

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

# ブランチ名は攻撃者が選べる文字列 (clone してきた repo / PR ブランチ) なので、prompt
# エスケープとして解釈させない。⚠️ 本番と同じ条件で観測する: PROMPT は prompt_subst の下で
# 'シングルクォート内の ${_DOTFILES_GIT_PROMPT}' として展開されるため、テストも同じ形で描く
# (値を先に展開して print -P へ渡す形にすると、それ自体が下の「コマンド実行しない」検査を壊す)。
setopt prompt_subst
render_prompt() { print -P -- 'X${_DOTFILES_GIT_PROMPT}Y' }
# ESC の本数だけ数える (fork なし。ESC 以外を消して長さを見る)
esc_count() { local s=${1//[^$'\e']/}; print -r -- "${#s}" }

_dotfiles_git_prompt
base_esc=$(esc_count "$(render_prompt)")   # 通常ブランチ (main) の ESC 本数が基準

git checkout -q -b 'x%F{red}%#%B'
_dotfiles_git_prompt
out=$(render_prompt)
# 解釈されていれば ESC 列になって字は残らない = 字が残っていることが「解釈されなかった」証拠
if [[ "$out" != *'[x%F{red}%#%B]'* ]]; then
  print -u2 "✗ % を含むブランチ名が字として出ていない: $(print -r -- "$out" | cat -v)"
  fails=$(( fails + 1 ))
else
  print "✓ ブランチ名の % は字として描かれる (エスケープとして解釈されない)"
fi
if [[ "$(esc_count "$out")" != "$base_esc" ]]; then
  print -u2 "✗ ブランチ名から ESC シーケンスが増えた (色が注入されている): 基準 $base_esc → $(esc_count "$out")"
  fails=$(( fails + 1 ))
else
  print "✓ ブランチ名で ESC シーケンスが増えない (色漏れなし)"
fi

# コマンド実行までは至らないこと。prompt 展開は 1 パスなので、値に含まれる $(...) は再走査
# されない。この配線 (PROMPT にシングルクォートで埋める) が唯一の防波堤なので固定する。
git checkout -q -b 'v$(id)w'
_dotfiles_git_prompt
out=$(render_prompt)
if [[ "$out" != *'v$(id)w'* || "$out" == *uid=* ]]; then
  print -u2 "✗ ブランチ名の \$(...) が実行された (または字として出ていない): $(print -r -- "$out" | cat -v)"
  fails=$(( fails + 1 ))
else
  print "✓ ブランチ名の \$(...) は実行されない (字として出る)"
fi
# 陽性対照: 危険な形 (値を**先に**展開して print -P へ渡す) では実際に実行される。これが無いと
# 上の検査は「そもそも検出できないから緑」= 空の主張になりうる (実測 2026-08-21: この形だと
# ブランチ名の $(id) が展開されて uid=... がプロンプトに出る)。
if [[ "$(print -P -- "$_DOTFILES_GIT_PROMPT")" != *uid=* ]]; then
  print -u2 "✗ 陽性対照が効いていない (危険な形でも実行されない = 上の検査が空回りしている)"
  fails=$(( fails + 1 ))
else
  print "✓ 陽性対照: 値を先に展開する形なら実行される (検出器は生きている)"
fi
git checkout -q main

# 配線の pin: PROMPT は値を**先に展開せず**シングルクォートの中で参照する。二重引用符や
# print -P -- "$_DOTFILES_GIT_PROMPT" 形へ変えると上の $(...) が実行される。
if ! grep -qF "PROMPT='%B%50<..<%~ %b\${_DOTFILES_GIT_PROMPT}'" "$ROOT_DIR/_zshrc"; then
  print -u2 "✗ _zshrc の PROMPT が「シングルクォート内で \${_DOTFILES_GIT_PROMPT} を参照」の形でない (値を先に展開すると $(...) が実行される)"
  fails=$(( fails + 1 ))
else
  print "✓ PROMPT はブランチ名を先に展開せず prompt 展開に任せている"
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
