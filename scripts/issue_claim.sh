#!/usr/bin/env bash
# issue_claim.sh — issue に着手の claim を付けて push するまでを 1 コマンドで行う (issue 764)。
#
#   scripts/issue_claim.sh <NNN>      (cwd の repo の issues/ にある issue)
#
# claim は「next/ の目印 (symlink) + 本文冒頭の担当者バナー + その 2 つだけの commit + 即 push」の 1 組
# (_claude/issue-rules.d/claim-issue-in-next-and-push.md)。手で打つと 4 つの操作に分かれ、途中で崩れる
# (2026-10-15: 先頭の git pull が detached HEAD で失敗し、&& で繋いだ本文の書き出しが走らず、symlink だけの commit ができた)。
#
# ## 一時 worktree で行う
#
# origin/<既定のブランチ> から一時 worktree を切り、その中で claim → commit → `push origin HEAD:<既定>` をして、worktree を捨てる。
#   - 手元の checkout の状態 (detached HEAD・未 push の作業 commit・古い本文・他セッションの未コミット変更) に左右されない。
#     手元の木は触らないので、claim は後から rebase / pull で入る
#   - 失敗したら worktree を捨てるだけで、手元にも origin にも何も残らない (commit 後の reset が要らない)
#   - non-fast-forward で弾かれたら取り直して作り直す (上限 ISSUE_CLAIM_ATTEMPTS、既定 3)
#   - pre-push で落ちたら --no-verify は使わずに止める (issue を触る push は、無関係な issue の赤でも止まる)
#
# 目印とバナーを置くのは bin/issue-claim (src/glogx/cmd/issue-claim。glogx の viewer の `n` と同じ issues.MoveToSubdir)。
# 既に claim 済み・直下に無い・done・番号の衝突はそちらが拒否する (rc=1)。
#
# 環境変数: ISSUE_CLAIM_BRANCH (push 先。既定は origin/HEAD が指すブランチ) / ISSUE_CLAIM_ATTEMPTS
#   ISSUE_CLAIM_TEST_BEFORE_PUSH: テスト専用。commit の後・push の前に bash -c で走らせる (origin を先へ進めて non-fast-forward を作る)
#
# exit code: 0 = claim を push した / 1 = claim しなかった (拒否・push の失敗。何も残っていない) / 2 = 引数・環境の誤り /
#   3 = push の後に、claim が origin の対象のブランチに載ったかを確かめられない (push は rc=0 なのに見えない = pushurl が別の repo /
#       fetch の失敗。claim は残っているかもしれない)
# SIGINT / SIGTERM は push の途中で受けても push の結果を確かめてから終わる (載っていれば rc=0 と結果を出す)。
# SIGKILL では一時 worktree が $TMPDIR に残る (git worktree prune では消えない。手で `git worktree remove` する)
set -euo pipefail
unset CDPATH
# 🚨 hook などから継承した GIT_DIR / GIT_WORK_TREE は `git -C` より優先され、claim を別の repo に書く
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_COMMON_DIR GIT_OBJECT_DIRECTORY GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_NAMESPACE

# 中断の合図を受けた後の失敗は、表示の理由より中断を優先して中断の rc で終わる (子が合図で死んで別の理由に見えるため)
die() {
  printf 'issue_claim: %s\n' "$1" >&2
  if [ -n "${sig:-}" ] && [ "${2:-2}" != 3 ]; then printf 'issue_claim: 中断した\n' >&2; exit "$sig"; fi
  exit "${2:-2}"
}

[ $# -eq 1 ] || die "usage: scripts/issue_claim.sh <NNN>"
case "$1" in -h | --help) sed -n '2,/^set -euo/p' "$0" | sed '$d; s/^# \{0,1\}//'; exit 0 ;; esac
num="$1"
[[ "$num" =~ ^[0-9]{3,}$ ]] || die "issue 番号は 3 桁以上の数字で指定する: $num"

TOOL_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)" # bin/issue-claim の置き場 (dotfiles)
repo="$(git rev-parse --show-toplevel 2>/dev/null)" || die "git の repo の中で実行する"
[ -d "$repo/issues" ] || die "issues/ が無い: $repo"

# shellcheck source=bin/lib/go_tool.sh
. "$TOOL_ROOT/bin/lib/go_tool.sh" || die "$TOOL_ROOT/bin/lib/go_tool.sh を読めない"
go_tool_resolve ISSUE_CLAIM "$TOOL_ROOT" glogx/cmd/issue-claim issue-claim help || die "issue-claim を用意できない"

branch="${ISSUE_CLAIM_BRANCH:-}"
if [ -z "$branch" ]; then
  # 今の既定を origin に聞く (refs/remotes/origin/HEAD は clone の時点の値で、既定が変わっても更新されない)
  branch="$(git -C "$repo" ls-remote --symref origin HEAD 2>/dev/null | awk '$1 == "ref:" && $3 == "HEAD" { sub("^refs/heads/", "", $2); print $2; exit }')"
fi
if [ -z "$branch" ]; then
  branch="$(git -C "$repo" symbolic-ref -q --short refs/remotes/origin/HEAD 2>/dev/null || true)"
  branch="${branch#origin/}"
fi
[ -n "$branch" ] || die "push 先のブランチが決まらない (origin/HEAD が無い)。ISSUE_CLAIM_BRANCH で指定する"
# fetch は push 先のブランチだけに絞る: 関係の無い ref の更新の失敗 (別の fetch が持つ lock 等) で全体が rc≠0 になり、
# 載ったかを確かめられなくなる (敵対レビュー 2026-10-15 の 3 周目で再現)。設定の refspec が対象を含まない clone でも追跡 ref が新しくなる
fetch_branch() { git -C "$repo" fetch -q origin "+refs/heads/$branch:refs/remotes/origin/$branch"; }
fetch_branch || die "origin の $branch を fetch できない" 1
git -C "$repo" rev-parse -q --verify "refs/remotes/origin/$branch" >/dev/null || die "origin/$branch が無い"
attempts="${ISSUE_CLAIM_ATTEMPTS:-3}"
[[ "$attempts" =~ ^[1-9][0-9]?$ ]] || die "ISSUE_CLAIM_ATTEMPTS は 1〜99: $attempts"

# 🚨 消すのは自分が作った一時 worktree だけ。パスは作った時点の値を控え、走査で探さない
work="$(mktemp -d "${TMPDIR:-/tmp}/issue_claim.XXXXXX")"
wt=""
drop_worktree() {
  [ -n "$wt" ] || return 0
  case "$wt" in "$work"/*) ;; *) printf 'issue_claim: 🚨 想定外の worktree のパスなので消さない: %s\n' "$wt" >&2; return 1 ;; esac
  git -C "$repo" worktree remove --force -- "$wt" >/dev/null 2>&1 || rm -rf -- "$wt"
  git -C "$repo" worktree prune >/dev/null 2>&1 || true
  wt=""
}
# shellcheck disable=SC2329 # trap 経由の間接呼び出し (SC2329 は直接呼び出ししか見ない)
cleanup() { drop_worktree || true; git -C "$repo" worktree prune >/dev/null 2>&1 || true; rm -rf -- "$work"; }
trap cleanup EXIT
# シグナルは印だけを立てる。push の最中に受けても push の結果を確かめてから終わる (受けた瞬間に exit すると、
# claim は origin に載っているのに無言で失敗を返す。敵対レビュー 2026-10-15 で再現)。区切りごとに check_signal で終わる
sig=""
trap 'sig=130' INT
trap 'sig=143' TERM
pushed=0
nothing_pushed() { if [ "$pushed" -eq 1 ]; then echo "確かめた時点では origin に載っていない"; else echo "何も push していない"; fi; }
check_signal() { [ -z "$sig" ] || { printf 'issue_claim: 中断した (%s)\n' "$(nothing_pushed)" >&2; exit "$sig"; }; }

for ((i = 1; i <= attempts; i++)); do
  pushed=0
  wt="$work/wt$i"
  git -C "$repo" worktree add -q --detach "$wt" "origin/$branch" >/dev/null 2>"$work/add.err" ||
    { cat "$work/add.err" >&2; wt=""; die "一時 worktree を作れない" 1; }

  claim_rc=0
  "$ISSUE_CLAIM" "$wt/issues" "$num" >"$work/claim.out" 2>"$work/claim.err" || claim_rc=$?
  if [ "$claim_rc" -ne 0 ]; then
    cat "$work/claim.err" >&2
    die "issue $num を claim しなかった (何も push していない)" 1
  fi

  check_signal
  # commit に入れるのは issue-claim が置いた 2 つのパス (本文と目印) だけ。`add -A` にすると、新品の checkout でも
  # 改行コードの正規化や filter で変更扱いになったファイルを巻き込む (敵対レビュー 2026-10-15 で再現)
  paths=()
  while IFS= read -r p; do [ -n "$p" ] && paths+=("issues/$p"); done <"$work/claim.out"
  [ "${#paths[@]}" -eq 2 ] || die "issue-claim の出力が想定 (本文と目印の 2 つ) と違う (何も push していない)" 1
  git -C "$wt" add -- "${paths[@]}" || die "claim の変更を add できない (何も push していない)" 1
  if [ "$(git -C "$wt" diff --cached --name-only | sort)" != "$(printf '%s\n' "${paths[@]}" | sort)" ]; then
    git -C "$wt" diff --cached --name-only >&2
    die "claim の commit に入る変更が本文と目印の 2 つではない (何も push していない)" 1
  fi
  # Claim-Id: run ごとに sha を一意にする。同じホスト・同じ秒の二重 claim で commit が丸ごと同じになり、後の側の push が
  # 「up-to-date」で通って両方が claim したと報告するのを防ぐ (敵対レビュー 2026-10-15 の 3 周目で再現)
  git -C "$wt" commit -q -F - >/dev/null <<EOF || die "claim を commit できない (何も push していない)" 1
claim: issue $num に着手

Claim-Id: $(hostname -s 2>/dev/null || echo host)-$$-$RANDOM$RANDOM
EOF
  sha="$(git -C "$wt" rev-parse HEAD)" || die "claim の commit を読めない (何も push していない)" 1

  if [ -n "${ISSUE_CLAIM_TEST_BEFORE_PUSH:-}" ]; then
    bash -c "$ISSUE_CLAIM_TEST_BEFORE_PUSH"
  fi
  check_signal

  push_rc=0
  git -C "$wt" push origin "HEAD:$branch" >"$work/push.log" 2>&1 || push_rc=$?
  # 🚨 push の rc に関係なく、まず origin の対象のブランチに claim の commit が載ったかを確かめる。rc=0 でも載らない
  # (pushurl が別の repo) ことがあり、rc≠0 でも載っている (ref を更新した後に Ctrl-C や切断で push が死ぬ) ことがある
  # (敵対レビュー 2026-10-15 の 2 周目で再現)。載ったかで分け、そのあとで中断・再試行・失敗を決める
  landed=unknown
  pushed=1 # 以降の「載っていない」は「確かめた時点では」(ssh の切断などで後から載ることがある)
  if fetch_branch 2>/dev/null; then
    if git -C "$repo" merge-base --is-ancestor "$sha" "refs/remotes/origin/$branch" 2>/dev/null; then landed=yes; else landed=no; fi
  fi
  if [ "$landed" = yes ]; then
    printf '✓ issue %s を claim して origin/%s へ push した (%s)\n' "$num" "$branch" "$(git -C "$repo" log -1 --format='%h %s' "$sha")"
    sed 's/^/  /' "$work/claim.out"
    printf '  手元の checkout には未反映。作業を始める前に rebase / pull で取り込む (claim は一時 worktree で行った)\n'
    [ "$push_rc" -eq 0 ] || printf '  (push は rc=%d で終わったが、claim は載っている)\n' "$push_rc"
    [ -z "$sig" ] || printf '  (push の途中で中断の合図を受けたが、claim は載っている)\n'
    sig="" # 載った結果を中断の rc で上書きしない
    exit 0
  fi
  if [ "$landed" = unknown ]; then
    cat "$work/push.log" >&2
    die "push は rc=$push_rc で終わったが、origin の $branch を fetch できず載ったかを確かめられない (claim が載ったかは未確認: $sha)" 3
  fi
  if [ "$push_rc" -eq 0 ]; then
    die "push は rc=0 だったが origin/$branch に claim の commit ($sha) が見えない (pushurl が別の repo など。claim は push 先に残っているかもしれない)" 3
  fi
  check_signal
  # non-fast-forward の判定は git の文言だけで見る (この repo の pre-push は通る push でも `pre-push:` の行を出す)
  if grep -q -e '(non-fast-forward)' -e '(fetch first)' "$work/push.log"; then
    printf 'issue_claim: origin/%s が先へ進んでいた。取り直して再試行する (%d/%d)\n' "$branch" "$i" "$attempts" >&2
    drop_worktree
    continue
  fi
  cat "$work/push.log" >&2
  die "push できなかった (pre-push の検査など。--no-verify は使わない。$(nothing_pushed))" 1
done
die "$attempts 回とも origin が先へ進んでいて push できなかった (何も push していない)" 1
