# shellcheck shell=bash
# プロンプト左の git ブランチ表示 (vcs_info の置換)。
#
# なぜ vcs_info を使わないか: vcs_info は毎プロンプトで git を数回 fork するため、
# repo 内では 1 プロンプトあたり 16.9ms を消費していた (実測: 200 回平均 / M3 Max)。
# 表示に使っているのはブランチ名だけなので、.git/HEAD を zsh の組み込みだけで読めば
# fork ゼロ・0.1ms 未満で同じ文字列が作れる。`zstyle ':vcs_info:*' enable git` で
# バックエンドを絞る案は -0.7ms しか効かず (残りは git 自身の fork) 却下した。
#
# 出力する状態 (vcs_info %b の踏襲):
#   通常ブランチ    → ブランチ名
#   detached HEAD   → 短縮 SHA (7 桁)
#   rebase / merge  → "<ブランチ>|REBASE" / "|MERGE" 等のサフィックス
#
# 意図的な非対応 (vcs_info からの縮退。必要になったら再評価):
#   - dirty マーク (check-for-changes) は元々 off だったので持たない (git status = 高コスト)
#   - git 以外の VCS (hg/svn 等)。現状 git のみを使うため

# _dotfiles_git_dir は $PWD から上へ辿って git ディレクトリの実体パスを REPLY に入れる
# (見つからなければ空)。fork なし: [[ -d ]] / [[ -f ]] と組み込み read だけを使う。
_dotfiles_git_dir() {
  emulate -L zsh
  REPLY=""
  # GIT_DIR が明示されている環境 (hook スクリプト等) はそれを尊重する
  if [[ -n "$GIT_DIR" ]]; then
    REPLY="$GIT_DIR"
    return 0
  fi
  local dir="$PWD" line
  while [[ -n "$dir" ]]; do
    if [[ -d "$dir/.git" ]]; then
      REPLY="$dir/.git"
      return 0
    elif [[ -f "$dir/.git" ]]; then
      # worktree / submodule: ".git" は "gitdir: <path>" の 1 行テキスト
      line="$(<"$dir/.git")" 2>/dev/null || return 1
      line="${line#gitdir:}"
      line="${line## }"
      line="${line%%$'\n'*}"
      [[ -z "$line" ]] && return 1
      [[ "$line" != /* ]] && line="$dir/$line"
      REPLY="$line"
      return 0
    fi
    dir="${dir%/*}"   # 1 階層上へ (/ まで来ると空になりループが終わる)
  done
  return 1
}

# _dotfiles_git_branch は表示用のブランチ名 (または短縮 SHA) を REPLY に入れる。
_dotfiles_git_branch() {
  emulate -L zsh
  local gitdir head state=""
  _dotfiles_git_dir || { REPLY=""; return 1; }
  gitdir="$REPLY"
  REPLY=""

  # rebase 中は HEAD が detached になり branch 名は別ファイルにある (git 自身の作法)
  if [[ -d "$gitdir/rebase-merge" ]]; then
    [[ -r "$gitdir/rebase-merge/head-name" ]] && head="$(<"$gitdir/rebase-merge/head-name")"
    head="${head#refs/heads/}"
    state="|REBASE"
  elif [[ -d "$gitdir/rebase-apply" ]]; then
    [[ -r "$gitdir/rebase-apply/head-name" ]] && head="$(<"$gitdir/rebase-apply/head-name")"
    head="${head#refs/heads/}"
    state="|AM/REBASE"
  fi

  if [[ -z "$head" ]]; then
    [[ -r "$gitdir/HEAD" ]] || return 1
    head="$(<"$gitdir/HEAD")"
    head="${head%%$'\n'*}"
    if [[ "$head" == ref:* ]]; then
      head="${head#ref: }"
      head="${head#refs/heads/}"
    else
      head="${head[1,7]}"   # detached: 短縮 SHA
    fi
    [[ -z "$state" && -f "$gitdir/MERGE_HEAD" ]] && state="|MERGE"
    [[ -z "$state" && -f "$gitdir/CHERRY_PICK_HEAD" ]] && state="|CHERRY-PICK"
    [[ -z "$state" && -f "$gitdir/BISECT_LOG" ]] && state="|BISECT"
  fi

  [[ -z "$head" ]] && return 1
  REPLY="${head}${state}"
  return 0
}

# _dotfiles_git_prompt は PROMPT に差し込む装飾済み文字列を
# _DOTFILES_GIT_PROMPT へ入れる (precmd から呼ぶ)。repo 外では空。
# 色は vcs_info 時代の formats '%F{black}%K{green}[%b]%f%k' をそのまま踏襲する。
typeset -g _DOTFILES_GIT_PROMPT=""
_dotfiles_git_prompt() {
  # local REPLY: 中の関数が書く REPLY をこのスコープに閉じ込める (zsh の動的スコープ)。
  # precmd hook なので、グローバル REPLY を書き換えて他の hook / widget を壊さないため。
  local REPLY
  if _dotfiles_git_branch; then
    # 🚨 ブランチ名の % は %% へ畳んでから埋める。ブランチ名は .git/HEAD 由来 = 攻撃者が選べる
    # 文字列で (clone してきた repo / PR ブランチ)、prompt_subst 下ではそのまま prompt エスケープ
    # として解釈される。実測 2026-08-21: 名前 `x%F{red}%#%B` で色と bold が変わり、%f%k が
    # 打ち消しきれず ] の後まで赤が漏れ、%# で root 表示を偽装できた。
    # 任意コマンド実行は起きない (PROMPT 側が '...${_DOTFILES_GIT_PROMPT}...' とシングルクォートで
    # 埋めており、prompt 展開は 1 パスなので値の中の $(...) は再走査されない)。この配線が唯一の
    # 防波堤なので、値を先に展開して print -P へ渡す形へ変えないこと (tests/zshrc/test_git_prompt.sh
    # が両方を pin している)。
    _DOTFILES_GIT_PROMPT="%F{black}%K{green}[${REPLY//\%/%%}]%f%k"
  else
    _DOTFILES_GIT_PROMPT=""
  fi
}
