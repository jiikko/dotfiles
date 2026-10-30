#!/usr/bin/env bash
# _claude/skills/claude-md-compact/fidelity.py (指示ファイルを縮めた前後で消えた規範を出す関門) の契約を fixture の repo で固定する。
#
# 守るもの: 移さずに消した部品は rc=1 / rationale へ移した部品は通す / 別のファイルの元から在った記述では消失を隠さない
# (行を書き直しても隠さない) / 移した 1 個で 2 か所の消失を覆わない / 箇条書きの継続行にある短い否定の語の消失を読む候補に出す /
# 実例の括弧の中の規範を消したら読む候補に出す / 縮めた後の rev を --base に渡したら rc=2 (一部の対象だけなら警告) / サブディレクトリから打っても同じ結果 /
# 改名は `旧=新`・統合して消したものは `旧=` で照合し、片方にしか無いパスを素で渡したら rc=2。
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

# 改名 (git mv) したファイル: `旧=新` で、改名しない場合と同じ結果になる (見出しの「<rev> の版:」だけが違う)
new_repo plain-drop
edit "$R/rules/a.md" "(retro 709: 長い待ちで止まった。実測: 待つ前に必ず状態を読む)" ""
run "$R" --base HEAD --dest rationale -- rules/a.md
want_rc=$RC; want=$(grep -v '^== ' <<<"$OUT")
new_repo renamed
git -C "$R" mv rules/a.md rules/c.md
edit "$R/rules/c.md" "(retro 709: 長い待ちで止まった。実測: 待つ前に必ず状態を読む)" ""
run "$R" --base HEAD --dest rationale -- rules/a.md=rules/c.md
got=$(grep -v '^== ' <<<"$OUT")
if [ "$RC" -eq "$want_rc" ] && [ "$got" = "$want" ] && grep -q '^== rules/c.md (<rev> の版: rules/a.md)' <<<"$OUT"; then ok '改名したファイルを `旧=新` で渡すと、改名しない場合と同じ結果になる'
else bad "改名で結果が変わる (rc=$RC 期待 $want_rc): $OUT"; fi

# 移し先に素の旧パスを一緒に渡しても 1 本として数える (<rev> の部品を 2 回数えない・今の版の空を照合先に入れない)
run "$R" --base HEAD --dest rationale rules/a.md -- rules/a.md=rules/c.md
got=$(grep -v '^== ' <<<"$OUT")
if [ "$RC" -eq "$want_rc" ] && [ "$got" = "$want" ]; then ok '`旧=新` と、移し先に素の旧パスを一緒に渡しても、1 本として数える'
else bad "旧パスを 2 回数える (rc=$RC): $OUT"; fi

# 片方にしか無いパスを素で渡したら、数え進めずに rc=2 と `旧=新` の案内
run "$R" --base HEAD -- rules/c.md
if [ "$RC" -eq 2 ] && grep -q '旧パス=新パス' <<<"$OUT"; then ok '新しいパスだけを渡すと rc=2 と `旧=新` の案内'
else bad "新しいパスだけで案内が出ない (rc=$RC): $OUT"; fi
run "$R" --base HEAD -- rules/a.md
if [ "$RC" -eq 2 ] && grep -q '旧パス=新パス' <<<"$OUT"; then ok '古いパスだけを渡すと、全部消えたと数えずに rc=2 と案内'
else bad "古いパスだけで全部消えたと数える (rc=$RC): $OUT"; fi

# 消して中身を移し先へ統合したファイルは `旧=` で渡す
new_repo merged
cat "$R/rules/a.md" >> "$R/rules/b.md"
git -C "$R" rm -q rules/a.md
run "$R" --base HEAD --dest rules/b.md -- rules/a.md=
if [ "$RC" -eq 0 ]; then ok '消して移し先へ統合したファイルを `旧=` で渡すと通す (rc=0)'
else bad "統合したファイルが通らない (rc=$RC): $OUT"; fi
run "$R" --base HEAD -- rules/a.md=
if [ "$RC" -eq 1 ]; then ok '`旧=` で移し先を渡さなければ、消えた部品として出す (rc=1)'
else bad "移し先なしの統合を通す (rc=$RC): $OUT"; fi

# 移し先のディレクトリを改名したら `旧=新` で、同じ相対パスどうしを組にする
new_repo dest-renamed
edit "$R/rules/a.md" "(retro 709: 長い待ちで止まった。実測: 待つ前に必ず状態を読む)" ""
git -C "$R" mv rationale why
printf -- '- retro 709: 長い待ちで止まった。実測: 待つ前に必ず状態を読む\n' >> "$R/why/a.md"
run "$R" --base HEAD --dest rationale=why -- rules/a.md
if [ "$RC" -eq 0 ]; then ok '改名した移し先のディレクトリを `旧=新` で渡すと、移した実例を見つけて通す'
else bad "改名した移し先で通らない (rc=$RC): $OUT"; fi
run "$R" --base HEAD --dest why -- rules/a.md
if [ "$RC" -eq 0 ]; then ok '改名した移し先を新しい名前だけで渡しても、今の版の中身を照合先にする'
else bad "新しい名前だけの移し先で通らない (rc=$RC): $OUT"; fi

# 改名の組でないものを `=` で組にしたら rc=2 (無関係な今のファイル・空の前の版を照合先に入れて消失を隠さない)
new_repo not-a-rename
edit "$R/rules/a.md" "- 本体は \`scripts/x.sh\` を通す" ""
run "$R" --base HEAD -- rules/a.md=rules/b.md
if [ "$RC" -eq 2 ] && grep -q 'にまだ在る' <<<"$OUT"; then ok '今の木に残る旧パスを `旧=新` で渡すと rc=2'
else bad "改名でない組を通す (rc=$RC): $OUT"; fi
git -C "$R" mv rules/a.md rules/c.md
run "$R" --base HEAD -- rules/a.md=rules/b.md
if [ "$RC" -eq 2 ] && grep -q '同じファイル' <<<"$OUT"; then ok '新パスが <rev> にも在る (改名先でない) 組は rc=2'
else bad "<rev> に在る新パスを改名先として通す (rc=$RC): $OUT"; fi
run "$R" --base HEAD --dest zzz=rules/b.md -- rules/a.md=rules/c.md
if [ "$RC" -eq 2 ] && grep -q 'zzz が HEAD に無い' <<<"$OUT"; then ok '移し先の旧パスが <rev> に無い組は rc=2 (打ち間違いで既存の中身を新しい文にしない)'
else bad "<rev> に無い旧パスの移し先を通す (rc=$RC): $OUT"; fi
run "$R" --base HEAD -- =rules/c.md
if [ "$RC" -eq 2 ] && grep -q '左に' <<<"$OUT"; then ok '`=新` (旧パスが空) は rc=2'
else bad "旧パスが空の組を通す (rc=$RC): $OUT"; fi

# 移し先のディレクトリが改名先を含むとき、素の新パスを 2 本目にしない
run "$R" --base HEAD --dest rules -- rules/a.md=rules/c.md
want_rc=$RC
if [ "$RC" -ne 2 ] && ! grep -q '2 つの組' <<<"$OUT"; then ok '改名先を含む移し先のディレクトリを渡しても、使い方の誤りにしない'
else bad "改名先を含む移し先で rc=2 (rc=$RC): $OUT"; fi
run "$R" --base HEAD -- rules/a.md=rules/c.md
alone_rc=$RC; alone=$OUT
run "$R" --base HEAD -- rules/a.md rules/a.md=rules/c.md
if [ "$RC" -eq "$alone_rc" ] && [ "$OUT" = "$alone" ]; then ok '素の旧パスと `旧=新` を対象に並べても、組だけを渡したのと同じ結果 (2 回数えない・落ちない)'
else bad "素の旧パスと組を並べた対象で結果が変わる (rc=$RC 期待 $alone_rc): $OUT"; fi

# 同じ新パスへ 2 つの組を向けたら rc=2
git -C "$R" rm -q rules/b.md
run "$R" --base HEAD -- rules/a.md=rules/c.md rules/b.md=rules/c.md
if [ "$RC" -eq 2 ] && grep -q '2 つの組' <<<"$OUT"; then ok '同じ新パスへ 2 つの組を向けたら rc=2'
else bad "同じ新パスへの 2 つの組を通す (rc=$RC): $OUT"; fi

# 名前に `=` を含む実在のファイルは、素のパスとして読む
new_repo eq-name
edit "$R/rules/a.md" "(retro 709: 長い待ちで止まった。実測: 待つ前に必ず状態を読む)" ""
printf -- '- retro 709: 長い待ちで止まった。実測: 待つ前に必ず状態を読む\n' > "$R/rationale/k=v.md"
run "$R" --base HEAD --dest rationale/k=v.md -- rules/a.md
if [ "$RC" -eq 0 ]; then ok '名前に `=` を含む実在のファイルは素のパスとして読む'
else bad "= を含むファイル名を組に割る (rc=$RC): $OUT"; fi

# 表記の揺れ (./ ・末尾の /) で改名先を 2 本目にして、今の版の部品を倍に数えない
new_repo spelled-twice
printf -- '- もう一度 `scripts/x.sh` を通す\n' >> "$R/rules/a.md"
git -C "$R" -c user.email=t@t -c user.name=t commit -qam twice
git -C "$R" mv rules/a.md rules/c.md
edit "$R/rules/c.md" "- もう一度 \`scripts/x.sh\` を通す" ""
run "$R" --base HEAD -- rules/a.md=rules/c.md
want_rc=$RC
for spelled in ./rules/c.md rules/./c.md rules/c.md/; do
  run "$R" --base HEAD --dest "$spelled" -- rules/a.md=rules/c.md
  if [ "$RC" -eq "$want_rc" ] && [ "$RC" -eq 1 ]; then ok "移し先に改名先を $spelled と書いても 2 回数えない (rc=1 のまま)"
  else bad "表記の揺れで改名先を 2 回数える ($spelled, rc=$RC): $OUT"; fi
done

# 大小を区別しないファイルシステムで、<rev> に在るファイルを大小違いの名前で改名先にしても通さない
new_repo case-new
cp "$R/rules/a.md" "$R/rules/b.md"
git -C "$R" -c user.email=t@t -c user.name=t commit -qam same
git -C "$R" rm -q rules/a.md
for alias in rules/B.md Rules/b.md lnk/b.md; do
  [ "$alias" = lnk/b.md ] && ln -s rules "$R/lnk"
  run "$R" --base HEAD -- "rules/a.md=$alias"
  if [ "$RC" -eq 2 ] && grep -q '同じファイル' <<<"$OUT"; then ok "<rev> に在る rules/b.md を $alias と書いた改名先は rc=2 (実体で比べる)"
  else bad "別の字面で既存のファイルを改名先にできる ($alias, rc=$RC): $OUT"; fi
done

# 大小だけを変えた改名は通す
new_repo case-only
git -C "$R" mv rules/a.md rules/A.md
edit "$R/rules/A.md" "(retro 709: 長い待ちで止まった。実測: 待つ前に必ず状態を読む)" ""
run "$R" --base HEAD -- rules/a.md=rules/A.md
if [ "$RC" -eq 1 ] && grep -q '消えた部品: retro709' <<<"$OUT"; then ok '大小だけを変えた改名を照合する'
else bad "大小だけの改名が照合されない (rc=$RC): $OUT"; fi

# ディレクトリを対象に渡したら、tree の一覧を本文として読まずに rc=2
new_repo dir-target
git -C "$R" rm -q -r rules
run "$R" --base HEAD -- rules=
if [ "$RC" -eq 2 ]; then ok 'ディレクトリを対象に `旧=` で渡したら rc=2 (tree の一覧を本文にしない)'
else bad "ディレクトリの対象で tree の一覧を読む (rc=$RC): $OUT"; fi

# git が引用する名前 (core.quotePath=true の日本語・どの設定でも引用される `"`) の <rev> のファイルも、改名先として通さない
new_repo quoted
git -C "$R" config core.quotePath true
cp "$R/rules/b.md" "$R/rules/日本.md"; cp "$R/rules/b.md" "$R/rules/q\"x.md"
git -C "$R" add -A && git -C "$R" -c user.email=t@t -c user.name=t commit -qm quoted
git -C "$R" rm -q rules/a.md
for alias in 'rules/日本.md' 'rules/q"x.md'; do
  run "$R" --base HEAD -- "rules/a.md=$alias"
  if [ "$RC" -eq 2 ] && grep -q '同じファイル' <<<"$OUT"; then ok "git が引用する名前 ($alias) の <rev> のファイルを改名先にしたら rc=2"
  else bad "引用される名前の既存のファイルを改名先にできる ($alias, rc=$RC): $OUT"; fi
done

# <rev> の木に UTF-8 でない名前があっても、改名の組の判定を落とさない (macOS では作れないので、木を直接組んで <rev> にする)
new_repo non-utf8
blob=$(printf 'x\n' | git -C "$R" hash-object -w --stdin)
tree=$( { git -C "$R" ls-tree HEAD; printf '100644 blob %s\tbad\377.md\n' "$blob"; } | git -C "$R" mktree)
git -C "$R" update-ref refs/heads/bad "$(git -C "$R" commit-tree "$tree" -p HEAD -m bad)"
git -C "$R" mv rules/a.md rules/z.md
edit "$R/rules/z.md" "(retro 709: 長い待ちで止まった。実測: 待つ前に必ず状態を読む)" ""
run "$R" --base bad -- rules/a.md=rules/z.md
if [ "$RC" -eq 1 ] && grep -q '消えた部品: retro709' <<<"$OUT"; then ok '<rev> に UTF-8 でない名前があっても、改名の組を照合する'
else bad "UTF-8 でない名前で改名の照合が落ちる (rc=$RC): $OUT"; fi

if [ "$fails" -ne 0 ]; then
  printf '✗ claude-md-compact の関門の契約が %d 件崩れている\n' "$fails"
  exit 1
fi
printf '✓ claude-md-compact の関門の契約がすべて成り立つ\n'
