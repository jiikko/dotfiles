#!/bin/bash
# 使い捨て worktree の作成・後始末。source して使う。
#
# なぜ lib か: `with_fresh_worktree.sh` と `bin/mutate-verify` が同じ「作って・壊れても消す」を
# 要る。掃除の規律 (下の 🚨) は事故から学んだものばかりで、2 箇所に書けば片方が必ず腐る。
#
# 使い方:
#   . "$root/scripts/lib/worktree_scratch.sh"
#   wts_init <repo root> <prefix>      # $WTS_PATH が決まり、EXIT trap が張られる
#   wts_create [<commit-ish>]          # 既定 HEAD。作成に失敗したら非 0
#   ... $WTS_PATH で作業 ...
#   (EXIT で自動的に消える。明示するなら wts_remove "$WTS_PATH")
#
# 🚨 後始末の保証は「起動時の掃除」が持つ。trap は中断 (SIGKILL・電源断) では走らないので、
# trap だけに任せると残骸が溜まる。消すのは **自分の prefix かつ pid が生きていないもの**
# だけに限る (並行して走っている別の run の worktree を殺さないため)。

wts_init() { # $1=repo root $2=prefix
  WTS_ROOT="$1"; WTS_PREFIX="$2"; WTS_ME=$$
  # 🚨 TMPDIR は macOS では `/var/folders/…/T/` (末尾スラッシュ + /var は /private/var への symlink)。
  # そのまま繋ぐと `//` を含む未正規化パスになり、`git worktree list` が返す正規化済みパス
  # (`/private/var/…`) と**文字列が一致しない**。sweep が pkill へ渡すのは後者なので、
  # 前者の形で argv を持つ残存プロセスに当たらなくなる (red team P2-9 が実測)
  local tmpdir; tmpdir="$(cd "${TMPDIR:-/tmp}" 2>/dev/null && pwd -P)" || tmpdir="/tmp"
  WTS_PATH="$tmpdir/$WTS_PREFIX.$WTS_ME"
  # 🚨 EXIT trap は 1 本しか持てない。呼び出し側が別の後始末を足すなら、この handler から
  # 呼ぶ形にすること (後から書いた trap ... EXIT が前を黙って上書きする)
  trap 'wts_cleanup' EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM HUP
}

# 🚨 その worktree へ書き込んでいる残存プロセスを先に止める。
# 中断が SIGKILL だと script だけが死に、子の `git worktree add` は生き残って checkout を
# 続ける。消しながら書かれるので rm が "Directory not empty" で取りこぼし、残骸が固定化する
# (2026-09-03 実測)。パスは prefix + pid で一意なので、この pattern は当該 worktree を触って
# いるプロセスにしか当たらない。
wts_kill_holders() {
  pkill -TERM -f "$1" 2>/dev/null || true
  pkill -KILL -f "$1" 2>/dev/null || true
}

# worktree を消す。**先にディレクトリを消してから prune** する。
# 🚨 逆順 (git worktree remove を先に試して失敗したら rm) は半端な状態を作る: remove が
# 途中で `.git` だけ消して失敗すると、以降の remove は "validation failed ... .git does not
# exist" で拒否し、rm も "Directory not empty" で落ちて残骸が固定化する (2026-09-03 に実測)。
# パスが消えていれば prune が管理ディレクトリを回収するので、この順なら固定化しない。
wts_remove() { # $1=worktree path
  local p="$1" i err
  wts_kill_holders "$p"
  # 🚨 エラーを捨てない。捨てると「消えていないのに静かに続行」になる。
  #    git worktree add は checkout 前に worktree を登録するので、中断が checkout の
  #    途中に当たると書き込み中のツリーを消すことになり、1 回目の rm が取りこぼす
  #    (2026-09-03 実測)。数回試して、それでも残るなら理由ごと出す。
  for i in 1 2 3; do
    [ -e "$p" ] || break
    err="$(rm -rf "$p" 2>&1)" || true
    [ -e "$p" ] || break
    [ "$i" = 3 ] && [ -n "$err" ] && echo "[$WTS_PREFIX] rm が失敗: $err" >&2
  done
  git -C "$WTS_ROOT" worktree prune
  [ ! -e "$p" ]
}

wts_sweep_stale() {
  local p pid
  git -C "$WTS_ROOT" worktree list --porcelain | awk '/^worktree /{print $2}' | while IFS= read -r p; do
    case "${p##*/}" in "$WTS_PREFIX".*) ;; *) continue ;; esac
    pid="${p##*.}"
    # 数字判定は明示列挙 (範囲式 [0-9] はロケールで全角を通す)。桁上限は integer 比較の保険
    case "$pid" in ''|*[!0123456789]*) continue ;; esac
    [ "${#pid}" -le 9 ] || continue
    [ "$pid" = "$WTS_ME" ] && continue
    kill -0 "$pid" 2>/dev/null && continue   # まだ走っている run のものは触らない
    echo "[$WTS_PREFIX] 前回の残骸を掃除: $p" >&2
    wts_remove "$p"
  done
  git -C "$WTS_ROOT" worktree prune
  # 掃除しきれなかったものは黙って放置しない。
  # 🚨 **ただし rc には出さない** (stderr へ出すだけで run は続く)。掃除の失敗で変異検証そのものを
  # 止める方が害が大きいため。「判定不能を緑にしない」と書いていたが実装は警告止まりで、
  # 宣言の方が広かった (red team 3 周目 P3-3)
  # 🚨 **対象は「掃除しようとしたのに残ったもの」だけ**。prefix 一致の全件を見ると、
  # **並行して正常に走っている別 run** を「残骸」として報告する (dotfiles は並行実行が前提。
  # red team 2 周目 P2-3: 旧形は pipefail 下の `| grep -q` で死んでいて発火せず、
  # <<< 化した途端に誤報が常態化した。検査を生き返らせるときは対象集合も見直すこと)
  local stuck=""
  while IFS= read -r p; do
    case "${p##*/}" in "$WTS_PREFIX".*) ;; *) continue ;; esac
    pid="${p##*.}"
    case "$pid" in ''|*[!0123456789]*) continue ;; esac
    [ "${#pid}" -le 9 ] || continue
    [ "$pid" = "$WTS_ME" ] && continue
    kill -0 "$pid" 2>/dev/null && continue      # 走行中の別 run は残骸ではない
    stuck="${stuck}${stuck:+
}$p"
  done <<EOF
$(git -C "$WTS_ROOT" worktree list --porcelain | awk '/^worktree /{print $2}')
EOF
  if [ -n "$stuck" ]; then
    echo "[$WTS_PREFIX] 🚨 掃除できなかった worktree が残っている (git worktree list で確認):" >&2
    printf '%s\n' "$stuck" >&2
  fi
}

# 🚨 絶対パスで消す。trap は cd の後に走るので、相対パスで書くと別ディレクトリを触る
wts_cleanup() {
  local rc=$?
  cd "$WTS_ROOT" || true      # cd-rc: allow 消す対象の中に居ると rm が失敗しうるので、失敗しても続ける
  # 呼び出し側の追加後始末 (EXIT trap は 1 本しか持てないのでここから呼ぶ)
  if [ -n "${WTS_ON_CLEANUP:-}" ]; then "$WTS_ON_CLEANUP" || true; fi
  wts_remove "$WTS_PATH" || echo "[$WTS_PREFIX] 🚨 worktree を消せなかった: $WTS_PATH" >&2
  exit "$rc"
}

wts_create() { # $1=commit-ish (既定 HEAD)
  wts_sweep_stale
  # 作成に失敗したら skip せず落ちる。依存が無いときに緑を返さない規律
  git -C "$WTS_ROOT" worktree add --detach "$WTS_PATH" "${1:-HEAD}" >&2 || return 1
  [ -d "$WTS_PATH" ] || { echo "[$WTS_PREFIX] worktree を作れなかった: $WTS_PATH" >&2; return 1; }
}
