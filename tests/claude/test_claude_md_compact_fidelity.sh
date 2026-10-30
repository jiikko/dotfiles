#!/usr/bin/env bash
# _claude/skills/claude-md-compact/fidelity.py (指示ファイルを縮めた前後で消えた規範を出す関門) の契約を fixture の repo で固定する。
#
# 守るもの: 移さずに消した部品は rc=1 / rationale へ移した部品は通す / 別のファイルの元から在った記述では消失を隠さない
# (行を書き直しても隠さない) / 移した 1 個で 2 か所の消失を覆わない / 箇条書きの継続行にある短い否定の語の消失を読む候補に出す /
# 実例の括弧の中の規範を消したら読む候補に出す / 縮めた後の rev を --base に渡したら rc=2 (一部の対象だけなら警告) / サブディレクトリから打っても同じ結果。
# shellcheck disable=SC2016  # バッククォートは Markdown のコードスパンの字で、展開させない
set -uo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
FID="$ROOT_DIR/_claude/skills/claude-md-compact/fidelity.py"
fails=0
ok()  { printf '  ✓ %s\n' "$1"; }
bad() { printf '  ✗ %s\n' "$1"; fails=$((fails + 1)); }

WORK="$(mktemp -d "${TMPDIR:-/tmp}/test-claude-md-compact-fidelity.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

# 新しい fixture の repo: rules/a.md・rules/b.md・rationale/a.md を 1 commit 目に置く
new_repo() {
  R="$WORK/$1"
  mkdir -p "$R/rules" "$R/rationale"
  git -C "$R" init -q
  cat > "$R/rules/a.md" <<'EOF'
# a

## ルール

- **作業ツリーを残さない**。`git worktree remove` で消す
  (push を確かめてから)
  残さない。
- 待ちは 10 秒までにする (retro 709: 長い待ちで止まった。実測: 待つ前に必ず状態を読む)
- 本体は `scripts/x.sh` を通す
- 共有の tree では確かめてから編集する。書かない。
- tmp の外へは書かない。
EOF
  cat > "$R/rules/b.md" <<'EOF'
# b

- 本体は `scripts/x.sh` を通す。ここは古い言い回し
EOF
  printf '# a の根拠\n' > "$R/rationale/a.md"
  git -C "$R" add -A && git -C "$R" -c user.email=t@t -c user.name=t commit -qm base
}
run() {  # $1=cwd 残り=fidelity の引数 → RC と OUT
  local d="$1"; shift
  RC=0
  OUT="$(cd "$d" && python3 -I "$FID" "$@" 2>&1)" || RC=$?
}
edit() { python3 -I -c 'import sys,pathlib; p=pathlib.Path(sys.argv[1]); s=p.read_text(); assert sys.argv[2] in s, sys.argv[2]; p.write_text(s.replace(sys.argv[2], sys.argv[3], 1))' "$@"; }

new_repo moved
edit "$R/rules/a.md" "(retro 709: 長い待ちで止まった。実測: 待つ前に必ず状態を読む)" ""
printf -- '- retro 709: 長い待ちで止まった。実測: 待つ前に必ず状態を読む\n' >> "$R/rationale/a.md"
run "$R" --base HEAD --dest rationale -- rules/a.md
if [ "$RC" -eq 0 ]; then ok '実例の括弧を rationale へそのまま移したら通す (rc=0)'
else bad "移した実例で緑にならない (rc=$RC): $OUT"; fi

new_repo dropped
edit "$R/rules/a.md" "(retro 709: 長い待ちで止まった。実測: 待つ前に必ず状態を読む)" ""
run "$R" --base HEAD --dest rationale -- rules/a.md
if [ "$RC" -eq 1 ] && grep -q '消えた部品: retro709' <<<"$OUT"; then ok '移さずに消した部品は rc=1 で出す'
else bad "移さずに消した部品が出ない (rc=$RC): $OUT"; fi

new_repo hidden-by-rewrite
edit "$R/rules/a.md" "- 本体は \`scripts/x.sh\` を通す" ""
edit "$R/rules/b.md" "ここは古い言い回し" "ここは新しい言い回し"
run "$R" --base HEAD --dest rules/b.md -- rules/a.md
if [ "$RC" -eq 1 ] && grep -q '消えた部品: `scripts/x.sh`' <<<"$OUT"; then ok '移し先の元から在った行を書き直しても、消失を隠さない'
else bad "書き直した行で消失が隠れる (rc=$RC): $OUT"; fi

new_repo one-covers-two
edit "$R/rules/a.md" "- 本体は \`scripts/x.sh\` を通す" ""
edit "$R/rules/b.md" "- 本体は \`scripts/x.sh\` を通す。ここは古い言い回し" "- ここは古い言い回し"
printf -- '- 本体は `scripts/x.sh` を通す\n' >> "$R/rationale/a.md"
run "$R" --base HEAD --dest rationale -- rules/a.md rules/b.md
if [ "$RC" -eq 1 ] && grep -q '`scripts/x.sh` (-1)' <<<"$OUT"; then ok '移した 1 個で 2 か所の消失を覆わない'
else bad "1 個の移動が 2 か所を覆う (rc=$RC): $OUT"; fi

new_repo short-negation
edit "$R/rules/a.md" "$(printf '\n  残さない。')" ""
run "$R" --base HEAD -- rules/a.md
if [ "$RC" -eq 3 ] && grep -q '読む候補' <<<"$OUT"; then ok '継続行の短い否定の語 (残さない) の消失を読む候補に出す (rc=3)'
else bad "短い否定の語の消失を見逃す (rc=$RC): $OUT"; fi

new_repo short-sentence
edit "$R/rules/a.md" "編集する。書かない。" "編集する。"
run "$R" --base HEAD -- rules/a.md
if [ "$RC" -eq 3 ] && grep -q '書かない' <<<"$OUT"; then ok '同じ行の短い否定の文 (書かない。) の消失を、同じ語が別の行に在っても読む候補に出す'
else bad "同じ行の短い否定の文の消失を見逃す (rc=$RC): $OUT"; fi

new_repo norm-in-example
edit "$R/rules/a.md" "。実測: 待つ前に必ず状態を読む)" ")"
printf -- '- retro 709: 長い待ちで止まった\n' >> "$R/rationale/a.md"
run "$R" --base HEAD --dest rationale -- rules/a.md
if [ "$RC" -eq 3 ] && grep -q '待つ前に必ず状態を読む' <<<"$OUT"; then ok '実例の括弧の中の規範を消したら読む候補に出す'
else bad "括弧の中の規範の消失を見逃す (rc=$RC): $OUT"; fi

new_repo same
run "$R" --base HEAD -- rules/a.md
if [ "$RC" -eq 2 ] && grep -q '今の版が同じ' <<<"$OUT"; then ok '--base と今の版が全部同じなら rc=2 (縮めた後の rev を渡した)'
else bad "同じ版で rc=2 にならない (rc=$RC): $OUT"; fi

new_repo partly-same
edit "$R/rules/a.md" "(retro 709: 長い待ちで止まった。実測: 待つ前に必ず状態を読む)" ""
run "$R" --base HEAD -- rules/a.md rules/b.md
if [ "$RC" -eq 1 ] && grep -q 'rules/b.md は HEAD と今の版が同じ' <<<"$OUT"; then ok '一部の対象だけ同じなら、その対象を警告に出して残りを照合する'
else bad "一部だけ同じ対象の扱いが違う (rc=$RC): $OUT"; fi

new_repo subdir
edit "$R/rules/a.md" "(retro 709: 長い待ちで止まった。実測: 待つ前に必ず状態を読む)" ""
run "$R/rules" --base HEAD -- a.md
if [ "$RC" -eq 1 ] && grep -q '消えた部品: retro709' <<<"$OUT"; then ok 'サブディレクトリから打っても cwd からの相対で読む'
else bad "サブディレクトリからで結果が変わる (rc=$RC): $OUT"; fi

if [ "$fails" -ne 0 ]; then
  printf '✗ claude-md-compact の関門の契約が %d 件崩れている\n' "$fails"
  exit 1
fi
printf '✓ claude-md-compact の関門の契約がすべて成り立つ\n'
