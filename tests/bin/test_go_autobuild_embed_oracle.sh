#!/usr/bin/env bash
# go_autobuild の //go:embed の展開を、本物の go list の EmbedFiles と突き合わせる。
# test_go_autobuild.sh の表は手で選んだ pattern と期待値で、Go の path.Match と zsh の glob の意味の差は、手で選ばなかった形で出る。
# ここでは pattern ごとに package を 1 つ作り、記録は production の
# _go_autobuild_record_embeds に本物の go で書かせ、_go_autobuild_embed_files の展開を go の EmbedFiles と比べる。
# 判定: Go が拾うものを拾い損ねない / Go より多く拾ってよいのは . か _ で始まる名前だけ (lib は再ビルドが増えるだけの側へ広げて拾う)。
# 受け入れた不一致 ([\]-^]・NFD・**/x。bin/lib/go_autobuild.zsh の _go_autobuild_go_glob の上) は pattern に入れない。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
LIB="$ROOT_DIR/bin/lib/go_autobuild.zsh"

if ! command -v go >/dev/null 2>&1; then
  echo "skipped: go が無い (本物の go list を oracle にするテスト)"
  exit 77
fi

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT
# 手元の toolchain をそのまま使う (go.mod の go 行で別の版を取りに行かない)。継承した workspace・flag に左右されない
export GOTOOLCHAIN=local GOWORK=off GOFLAGS=

fail() { echo "✗ $*" >&2; exit 1; }
ok() { echo "✓ $*"; }

M="$TMP_DIR/mod"
FIX="$TMP_DIR/fixture"
SRC="$TMP_DIR/src"   # 記録 (.autobuild.embeds) の置き場。package の下に置くと `*` の pattern が記録を拾う
mkdir -p "$M" "$FIX/sub/deep" "$FIX/sub/.hd" "$FIX/sub/_ud" "$FIX/s" "$FIX/d(1)" "$SRC"
printf 'module example.com/m\n\ngo 1.22\n' > "$M/go.mod"
for f in a.txt ab.txt abc.txt b.txt c.txt 'x y.txt' ab .hid.txt _u.txt 'p(1).md' p1.md x-y xay xby 'x[y' 'あ.txt' 'a]' \
  sub/s1.md sub/.dot.md sub/_u.md sub/deep/d.md sub/.hd/h.md sub/_ud/u.md s/Zx 's/]x' 's/^x' s/ax 'd(1)/f.txt'; do
  : > "$FIX/$f"
done

# 1 行 = 1 pattern。go が拒む pattern (構文の誤り・当たらない・. の要素) も混ぜてある (拒んだものは比べずに数だけ出す)
PATTERNS=(
  '*.txt' '*' 'a*' '?.txt' '??.txt' '[abc].txt' '[a-c]*.txt' '[^a]*.txt' '[!a]*.txt' 'a\b' '*\.txt' 'a\b*' '\a*.txt'
  'a\b.tx?' '[a-b]*.txt' '[^a-b]*.txt' '[a!]b*' 'x[a\-z]y' 'x\[y' 'x[[]y' '[^a].txt' 'x y.txt' 'x?y.txt' 'x[ ]y.txt'
  'p(1).md' 'p[(]1[)].md' '*(1)*' 'p\(1\).md' 'sub' 'all:sub' 'sub/*' 'sub/*.md' 'sub/deep' 'sub/*/d.md' 'all:sub/*'
  's' 's/*' 's/[Z-^]x' 's/\]x' 's/\^x' 's/[\^]x' 's/[^^]x' 's/[\]]x' 's/[a\]]x' 's/[]x]x' '[\!a]*.txt' 'x[\[a]y' '[\[:alpha:]]' 'あ.txt' '[あ].txt' '[^あ]*.txt' '?.*'
  'd(1)' 'd(1)/*' 'd[(]1[)]' '.hid.txt' '_u.txt' '.*' '_*' 'all:.' '.' 'x[-a]y' 'x[a-]y' '[' '\' 'nomatch*'
)

i=0
for pat in "${PATTERNS[@]}"; do
  i=$((i + 1))
  name="$(printf 'p%02d' "$i")"
  mkdir -p "$M/$name"
  cp -R "$FIX/." "$M/$name/"
  printf 'package %s\n\nimport "embed"\n\n//go:embed `%s`\nvar F embed.FS\n' "$name" "$pat" > "$M/$name/p.go"
  printf '%s\t%s\n' "$name" "$pat" >> "$TMP_DIR/names"
done

# oracle: package ごとに、go が受けたか (OK / ERR) と EmbedFiles (package の dir からの相対)
go list -C "$M" -e -f '{{.Name}}{{"\t"}}{{if .Error}}ERR{{else}}OK{{end}}{{range .EmbedFiles}}{{"\t"}}{{.}}{{end}}' ./... \
  > "$TMP_DIR/oracle.tsv" || fail "go list (oracle) が落ちた"
: > "$TMP_DIR/go.txt"
okpkgs=()
rejected=()
while IFS=$'\t' read -r -a cols; do
  pkg="${cols[0]}"
  if [[ "${cols[1]}" == OK ]]; then
    okpkgs+=("$pkg")
    for f in "${cols[@]:2}"; do printf '%s/%s\n' "$pkg" "$f" >> "$TMP_DIR/go.txt"; done
  else
    rejected+=("$(awk -F'\t' -v n="$pkg" '$1 == n { print $2 }' "$TMP_DIR/names")")
  fi
done < "$TMP_DIR/oracle.tsv"
(( ${#okpkgs[@]} == ${#PATTERNS[@]} - ${#rejected[@]} )) || fail "go list の package の数が合わない (OK ${#okpkgs[@]} / 拒否 ${#rejected[@]} / pattern ${#PATTERNS[@]})"
# go が受けた pattern が少なすぎるなら、fixture か go の版の変化で比べる対象が消えている
(( ${#okpkgs[@]} >= 45 )) || fail "go が受けた pattern が ${#okpkgs[@]} 件しかない (拒否: ${rejected[*]})"

# go が受けた package だけを import する main を作り、production の記録の経路 (go list -deps) を通す
mkdir -p "$M/cmd"
{
  printf 'package main\n\nimport (\n'
  for p in "${okpkgs[@]}"; do printf '\t_ "example.com/m/%s"\n' "$p"; done
  printf ')\n\nfunc main() {}\n'
} > "$M/cmd/main.go"

zsh -c '
  unset _GO_AUTOBUILD_MOD_DIR _GO_AUTOBUILD_PKG   # 継承した --pkg 相当の指定を _go_autobuild_resolve_mod が先に読む
  source "$1"
  _go_autobuild_resolve_mod "$3"   # production と同じく mod_dir を :A で揃える (/var と /private/var で記録から外れる)
  _go_autobuild_record_embeds "$2" "$reply[1]" ./cmd; rc=$?
  (( rc == 2 )) || { print -u2 -- "記録を書かない (rc=$rc)"; exit 1; }
  _go_autobuild_embed_files "$2"
  (( ${#_go_autobuild_embed_unresolved} == 0 )) || print -ru2 -- "未解決: ${_go_autobuild_embed_unresolved[*]}"
  root="${3:A}"
  for f in "${reply[@]}"; do print -r -- "${f#$root/}"; done
' zsh "$LIB" "$SRC" "$M" > "$TMP_DIR/ours.txt" 2> "$TMP_DIR/ours.err" || fail "展開が落ちた: $(cat "$TMP_DIR/ours.err")"
[[ ! -s "$TMP_DIR/ours.err" ]] || fail "展開が 0 件の pattern を出した: $(cat "$TMP_DIR/ours.err")"
[[ "$(wc -l < "$TMP_DIR/go.txt")" -gt 0 ]] || fail "前提が崩れた: go の EmbedFiles が 0 件"

LC_ALL=C sort -u "$TMP_DIR/go.txt" > "$TMP_DIR/go.s"
LC_ALL=C sort -u "$TMP_DIR/ours.txt" > "$TMP_DIR/ours.s"
# pattern の名前を添えて出す (p07/x → p07 `[a-c]*.txt`: x)
explain() { while IFS= read -r l; do printf '  %s `%s`: %s\n' "${l%%/*}" "$(awk -F'\t' -v n="${l%%/*}" '$1 == n { print $2 }' "$TMP_DIR/names")" "${l#*/}"; done; }
miss="$(LC_ALL=C comm -23 "$TMP_DIR/go.s" "$TMP_DIR/ours.s")"
[[ -z "$miss" ]] || fail "Go が embed するファイルを拾い損ねた (embed したファイルだけを直しても作り直さない):
$(explain <<< "$miss")"
# 余分は、package の dir より下のどこかの要素が . か _ で始まるものだけ (Go はディレクトリを辿るとき、その下ごと飛ばす)
extra="$(LC_ALL=C comm -13 "$TMP_DIR/go.s" "$TMP_DIR/ours.s" | grep -Ev '^[^/]+/(.*/)?[._]' || true)"
[[ -z "$extra" ]] || fail "Go が embed しないファイルを拾った (. か _ で始まる名前以外。pattern の写しの誤り):
$(explain <<< "$extra")"
ok "embed の展開が go list の EmbedFiles を全部含み、余分は . か _ で始まる名前だけ (go が受けた pattern ${#okpkgs[@]} 件・ファイル $(wc -l < "$TMP_DIR/go.s" | tr -d ' ') 件。go が拒んだ ${#rejected[@]} 件: ${rejected[*]})"
