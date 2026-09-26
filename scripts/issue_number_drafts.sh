#!/usr/bin/env bash
# issue_number_drafts.sh — 番号の無い仮の名前で起票された issue に番号を付ける (issue 530)。
#
#   scripts/issue_number_drafts.sh [<数える ref>]     (既定の ref は origin/master)
#
# ## なぜ
#
# pro-con の PG は push しないので、PG が worktree で採番した番号は取り込まれるまで master から
# 見えず、その間に同じ番号が使われて衝突する (監査 C-071 は 6 回付け直した)。そこで PG は番号を
# 取らずに `issues/<置き場>/new-<type>-<slug>.md` で起票し、**push の直前にいる取り込みの係が
# この script で番号を付ける**。採番と push の間を縮めるのが目的で、番号を払い出す口を別に作ると
# pro-con の外で採番する人・session からその予約が見えない。
#
# ## やること
#
#   1. repo root から `issues/` 配下の `new-*.md` (実体) を集める。0 件なら何もしない
#   2. 次の番号 = working tree と <ref> の `issues/` の `NNN-*.md` の最大 + 1 (issue 規約の採番と同じ母集合)。
#      パスの辞書順に 1 つずつ振る
#   3. `git mv` で `NNN-<type>-<slug>.md` へ改名し、1 行目の `# new (` を `# NNN (` へ書き換える
#   4. 追跡している text ファイル中の旧ファイル名 (`new-<type>-<slug>.md`) を新しい名前へ置き換える
#      (同じディレクトリ内の改名なのでリンクの相対部分は変わらない)
#
# commit はしない (呼んだ側が 1 commit にして、すぐ push する)。
#
# ## 止める形 (何も動かさずに rc=1)
#
#   - 名前が `new-<type>-<slug>.md` (英小文字・数字・`-`) の形でない
#   - 同じ仮の名前が 2 か所にある (どちらへの参照か決められない)
#   - 仮の名前の symlink がある (next/ の claim。番号の無い issue は claim しない)
#   - <ref> が無い (fetch していない。origin 側を数えずに振ると衝突を作る)
set -euo pipefail
unset CDPATH

usage() {
  cat <<'EOF'
usage: scripts/issue_number_drafts.sh [<ref>]
  issues/ 配下の new-<type>-<slug>.md に番号を付けて改名し、参照を張り替える (commit はしない)。
  番号は working tree と <ref> (既定 origin/master) の issues/ の最大 + 1 から振る。
EOF
}

case "${1:-}" in -h|--help) usage; exit 0 ;; esac
[ $# -le 1 ] || { usage >&2; exit 2; }
ref="${1:-origin/master}"

root="$(git rev-parse --show-toplevel)"
cd "$root"

fail() { printf '✗ %s\n' "$1" >&2; exit 1; }

[ -d issues ] || exit 0

links="$(find issues -type l -name 'new-*.md')"
[ -z "$links" ] || fail "仮の名前の symlink がある (番号の無い issue は claim しない): $links"

drafts=()
while IFS= read -r f; do
  [ -n "$f" ] && drafts+=("$f")
done < <(find issues -type f -name 'new-*.md' | LC_ALL=C sort)
[ "${#drafts[@]}" -gt 0 ] || exit 0

git rev-parse --verify -q "$ref^{commit}" >/dev/null || fail "$ref が無い (git fetch してから回す)"

seen=""
for f in "${drafts[@]}"; do
  b="${f##*/}"
  [[ "$b" =~ ^new-[a-z]+-[a-z0-9-]+\.md$ ]] || fail "仮の名前の形でない (new-<type>-<slug>.md): $f"
  case " $seen " in *" $b "*) fail "同じ仮の名前が 2 か所にある: $b" ;; esac
  seen="$seen $b"
done

max="$({ find issues -type f -name '[0-9][0-9][0-9]-*.md' | sed 's|.*/||'
         git ls-tree -r --name-only "$ref" -- issues | sed 's|.*/||'; } |
       grep -E '^[0-9]{3}-' | cut -c1-3 | LC_ALL=C sort | tail -1 || true)"
n=$((10#${max:-0}))

for f in "${drafts[@]}"; do
  n=$((n + 1))
  num="$(printf '%03d' "$n")"
  old="${f##*/}"
  new="$num-${old#new-}"
  git mv -- "$f" "${f%/*}/$new"
  perl -pi -e 's/^# new \(/# '"$num"' (/ if $. == 1' "${f%/*}/$new"
  # 旧ファイル名を含む追跡中の text ファイルだけを書き換える (-I で binary を外す)
  while IFS= read -r g; do
    [ -n "$g" ] && OLD="$old" NEW="$new" perl -pi -e 's/\Q$ENV{OLD}\E/$ENV{NEW}/g' -- "$g"
  done < <(git grep -lIF -- "$old" || true)
  printf '%s → %s\n' "$f" "${f%/*}/$new"
done
