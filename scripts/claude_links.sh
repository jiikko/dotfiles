#!/usr/bin/env bash
# claude_links.sh — _claude/ の agent / command / rule / hook / skill / workflow を ~/.claude/ へ
# 1 ファイルずつ symlink する。「期待するリンク集合」の唯一の出典で、setup.sh (apply) と
# SessionStart hook の _claude/hooks/claude-links-sync.sh (check → 欠けたときだけ apply) が呼ぶ。
# 集合の方針を変えたら tests/claude/test_claude_links_complete.sh (独立したオラクル) も直す。
#
# 使い方: claude_links.sh list | check | apply
#   list  : 期待するリンクを "<link>\t<実体>" で列挙する
#   check : 欠けている / 別物を指しているリンクを列挙する。何も変更しない。
#           exit 0 = 全部揃っている / 1 = 欠けあり / 3 = 検査できない (root や ~/.claude が無い)
#   apply : 欠けている分だけ ln -sfn で張る。exit 0 = 張った (0 件でも 0) / 2 = 張れないものがあった / 3 = 検査できない
#
# apply が **やらないこと** (setup.sh 側の責務。hook から自動で走るので破壊的操作を持たない):
#   - dangling link の削除、dir symlink の migrate。~/.claude/<dir> が dir symlink のままなら
#     全件を張らずに exit 2 する (ln -sfn がリンク先 = repo 側へ書き込み、元ファイルを
#     自己参照 symlink で壊す。setup.sh の migrate コメント参照)
#   - link 先に **symlink でない実ファイル** があるとき、または **_claude/ 以外を指す symlink**
#     (他ツールが置いたもの。ディレクトリ丸ごと link を捨てた理由 = a7e9b29 の衝突がこれ)
#     があるときの上書き。どちらも exit 2 で報告だけする
#
# env: DOTFILES_ROOT (既定 ~/dotfiles) / CLAUDE_HOME (既定 ~/.claude)
# ⚠️ root の既定を「このスクリプトの場所」にしない。worktree から実行すると ~/.claude が
# worktree を指し、worktree を消した瞬間に全 link が dangling になる。setup.sh と同じ ~/dotfiles 固定。
#
# hooks の link は現状どこからも読まれていない (hook の起動経路は _claude/settings.json の
# command に書いた dotfiles の実体パスだけ。issue 142 の実測)。それでも張るのは、置き場所に
# 依存しない安定パスを用意しておくため。読む側が現れたら hooks/lib の link も必須になる
# (hook は lib を dirname "$0" で解決し、$0 は symlink を解決しない)。
# workflows は Workflow ツールが scriptPath で参照する .js だけ (CLAUDE.md 等は ~/.claude に不要)。

set -u
shopt -s nullglob

# ${HOME:-}: set -u 下で HOME 未設定でも落ちず、preflight の「検査できない」(exit 3) へ倒す
ROOT="${DOTFILES_ROOT:-${HOME:-}/dotfiles}"
CLAUDE_HOME="${CLAUDE_HOME:-${HOME:-}/.claude}"
DIRS="agents commands rules hooks skills workflows"

usage() {
  echo "usage: ${0##*/} list | check | apply" >&2
  exit 64
}

# 検査できない環境は「揃っている」に丸めない (exit 3)。
preflight() {
  if [ ! -d "$ROOT/_claude" ]; then
    echo "cannot check: $ROOT/_claude が無い (DOTFILES_ROOT を確認する)" >&2
    return 3
  fi
  if [ ! -d "$CLAUDE_HOME" ]; then
    echo "cannot check: $CLAUDE_HOME が無い" >&2
    return 3
  fi
  return 0
}

expected_links() {
  local d f
  for d in agents commands rules hooks; do
    for f in "$ROOT/_claude/$d"/*; do
      [ -e "$f" ] || continue
      printf '%s\t%s\n' "$CLAUDE_HOME/$d/${f##*/}" "$f"
    done
  done
  for d in "$ROOT/_claude/skills"/*/; do
    d=${d%/}
    [ -d "$d" ] || continue
    printf '%s\t%s\n' "$CLAUDE_HOME/skills/${d##*/}" "$d"
  done
  for f in "$ROOT/_claude/workflows"/*.js; do
    [ -e "$f" ] || continue
    printf '%s\t%s\n' "$CLAUDE_HOME/workflows/${f##*/}" "$f"
  done
}

# 欠けている / 別物を指している link を "<link>\t<実体>" で出す。
drift() {
  local link target
  while IFS=$'\t' read -r link target; do
    if [ ! -L "$link" ] || [ ! "$link" -ef "$target" ]; then
      printf '%s\t%s\n' "$link" "$target"
    fi
  done < <(expected_links)
}

cmd_check() {
  local out
  out=$(drift)
  [ -z "$out" ] && return 0
  printf '%s\n' "$out" | while IFS=$'\t' read -r link target; do
    echo "missing: $link -> $target"
  done
  return 1
}

cmd_apply() {
  local d link target cur err tries rc=0 n=0
  for d in $DIRS; do
    if [ -L "$CLAUDE_HOME/$d" ]; then
      echo "refused: $CLAUDE_HOME/$d が dir symlink のまま (旧形式)。./setup.sh で migrate してから" >&2
      return 2
    fi
  done
  for d in $DIRS; do
    mkdir -p "$CLAUDE_HOME/$d" || return 2
  done
  while IFS=$'\t' read -r link target; do
    [ -n "$link" ] || continue
    if [ -e "$link" ] && [ ! -L "$link" ]; then
      echo "refused: $link は symlink でない実ファイル。上書きしない (手で退避してから ./setup.sh)" >&2
      rc=2
      continue
    fi
    if [ -L "$link" ]; then
      cur=$(readlink "$link" || true)
      # 空 = -L を見た直後に並行プロセスが消した (TOCTOU)。「他ツールの link」ではないので張りに進む
      case "$cur" in
        '' | "$ROOT"/_claude/*) ;;
        *)
          echo "refused: $link -> $cur は _claude/ 以外を指す (他ツールの link と衝突)。上書きしない" >&2
          rc=2
          continue
          ;;
      esac
    fi
    # ⚠️ 成否は ln の exit code でなく結果の状態 (-ef) で判定する。BSD ln -f は symlink() が EEXIST の
    # とき unlink → symlink を retry する非アトミックな実装で、複数セッションが同時に起動して同じ
    # link を張ると、片方の 2 回目 symlink() が EEXIST で非 0 になる (2-way で 57%、実測 2026-09-02)。
    # 最終状態は両者とも同じ target なので、状態が合っていれば linked。合っていなければ ln の stderr を出す。
    # expected_links は glob 時点のスナップショットなので、張る直前に並行セッションが target を git mv
    # していれば dangling ができる。それも -ef で failed になり、呼び出し側が ./setup.sh (掃除) へ誘導する
    # さらに、自分の ln が成功した直後でも、他方が unlink 済み・symlink 未作成の瞬間を見ると link が
    # 「無い」ので、状態確認は数回やり直す (窓はマイクロ秒。5 並行 x 20 回で 1/100 を実測、retry 後 0)
    tries=0
    while :; do
      err=$(ln -sfn "$target" "$link" 2>&1 || true)
      if [ -L "$link" ] && [ "$link" -ef "$target" ]; then
        echo "linked: $link -> $target"
        n=$((n + 1))
        break
      fi
      tries=$((tries + 1))
      if [ "$tries" -ge 5 ]; then
        echo "failed: ln -sfn $target $link${err:+ ($err)}" >&2
        rc=2
        break
      fi
    done
  done < <(drift)
  echo "linked $n"
  return "$rc"
}

[ $# -eq 1 ] || usage
case "$1" in
  list) preflight || exit $?; expected_links ;;
  check) preflight || exit $?; cmd_check ;;
  apply) preflight || exit $?; cmd_apply ;;
  *) usage ;;
esac
