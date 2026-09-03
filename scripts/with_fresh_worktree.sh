#!/bin/bash
# 新品チェックアウト (= git に追跡されているものだけがある状態) で任意のコマンドを回す。
#
# 用途: 「手元には在るが git に載っていないもの」への依存を push 前に炙り出す (issue 132)。
# ignore されているもの (tmp/) だけでなく、空ディレクトリ (issues/next/) や untracked も
# 同じ形で壊れるので、判定は「ignore か」ではなく「git に載っているか」で行う必要がある。
# git worktree はまさにその状態を作るので、環境差の再現に使う。
#
# 使い方: scripts/with_fresh_worktree.sh <command> [args...]
#   worktree の中へ cd してから <command> を実行する。入口は Makefile の test-fresh。
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
prefix="dotfiles-fresh"
me=$$
wt="${TMPDIR:-/tmp}/$prefix.$me"

# 🚨 後始末の保証は「起動時の掃除」が持つ。trap は中断 (SIGKILL・電源断) では走らないので、
# trap だけに任せると残骸が溜まる。消すのは **自分の prefix かつ pid が生きていないもの**
# だけに限る (並行して走っている別の run の worktree を殺さないため)。
sweep_stale() {
  local p pid
  git -C "$root" worktree list --porcelain | awk '/^worktree /{print $2}' | while IFS= read -r p; do
    case "${p##*/}" in "$prefix".*) ;; *) continue ;; esac
    pid="${p##*.}"
    # 数字判定は明示列挙 (範囲式 [0-9] はロケールで全角を通す)。桁上限は integer 比較の保険
    case "$pid" in ''|*[!0123456789]*) continue ;; esac
    [ "${#pid}" -le 9 ] || continue
    [ "$pid" = "$me" ] && continue
    kill -0 "$pid" 2>/dev/null && continue   # まだ走っている run のものは触らない
    echo "[fresh] 前回の残骸を掃除: $p" >&2
    remove_worktree "$p"
  done
  git -C "$root" worktree prune
  # 掃除しきれなかったものは黙って放置しない (「判定不能」を緑にしない)
  if git -C "$root" worktree list --porcelain | awk '/^worktree /{print $2}' |
       grep -q "/$prefix\."; then
    echo "[fresh] 🚨 掃除できなかった worktree が残っている (git worktree list で確認):" >&2
    git -C "$root" worktree list | grep "$prefix\." >&2 || true
  fi
}

# worktree を消す。**先にディレクトリを消してから prune** する。
# 🚨 逆順 (git worktree remove を先に試して失敗したら rm) は半端な状態を作る: remove が
# 途中で `.git` だけ消して失敗すると、以降の remove は "validation failed ... .git does not
# exist" で拒否し、rm も "Directory not empty" で落ちて残骸が固定化する (2026-09-03 に実測)。
# パスが消えていれば prune が管理ディレクトリを回収するので、この順なら固定化しない。
# 🚨 その worktree へ書き込んでいる残存プロセスを先に止める。
# 中断が SIGKILL だと script だけが死に、子の `git worktree add` は生き残って checkout を
# 続ける。消しながら書かれるので rm が "Directory not empty" で取りこぼし、残骸が固定化する
# (2026-09-03 実測)。パスは prefix + pid で一意なので、この pattern は当該 worktree を触って
# いるプロセスにしか当たらない。
kill_holders() {
  local p="$1"
  pkill -TERM -f "$p" 2>/dev/null || true
  pkill -KILL -f "$p" 2>/dev/null || true
}

remove_worktree() {
  local p="$1" i err
  kill_holders "$p"
  # 🚨 エラーを捨てない。捨てると「消えていないのに静かに続行」になる。
  #    git worktree add は checkout 前に worktree を登録するので、中断が checkout の
  #    途中に当たると書き込み中のツリーを消すことになり、1 回目の rm が取りこぼす
  #    (2026-09-03 実測)。数回試して、それでも残るなら理由ごと出す。
  for i in 1 2 3; do
    [ -e "$p" ] || break
    err="$(rm -rf "$p" 2>&1)" || true
    [ -e "$p" ] || break
    [ "$i" = 3 ] && [ -n "$err" ] && echo "[fresh] rm が失敗: $err" >&2
  done
  git -C "$root" worktree prune
  [ ! -e "$p" ]
}

# 🚨 絶対パスで消す。trap は cd の後に走るので、相対パスで書くと別ディレクトリを触る
cleanup() {
  local rc=$?
  cd "$root" || true          # 消す対象の中に居ると rm が失敗しうる
  remove_worktree "$wt" || echo "[fresh] 🚨 worktree を消せなかった: $wt" >&2
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP

sweep_stale

# 作成に失敗したら skip せず落ちる (set -e)。依存が無いときに緑を返さない規律
git -C "$root" worktree add --detach "$wt" HEAD >&2
[ -d "$wt" ] || { echo "[fresh] worktree を作れなかった: $wt" >&2; exit 1; }

cd "$wt"
# 親 make の jobserver を持ち込まない (再帰 make の警告と取り合いを避ける)
unset MAKEFLAGS MAKELEVEL
"$@"
