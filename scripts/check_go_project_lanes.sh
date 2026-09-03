#!/usr/bin/env bash
# 新しい Go プロジェクトが「CI レーン無し」で入るのを止める検査 (issue 203 候補 B)。
#
# なぜ: 出典は issue 080 / 087。以前は Makefile の GO_PROJECT_DIRS を手で列挙しており、
# 追記を忘れたプロジェクトが **無音で lint / test から外れた**。Makefile 側は
# `wildcard src/*/go.mod` で解決済み (Makefile:392) だが、**CI の paths filter と
# プロジェクト側の Makefile target は今も手で用意する**ので、同じ穴が残っている。
#
# 検査する不変条件 (src/*/go.mod を出典に、3 つ):
#   1. src/<name>/Makefile に lint: と test: の両方がある
#      (Makefile の run_go_projects が `make -C <dir> lint` / `test` を呼ぶ)
#   2. .github/workflows/src_<name>.yml がある (paths filter つきの薄い caller)
#   3. その workflow の paths が src/<name>/ を含む (含まないと push で 1 度も起動しない)
#
# ⚠️ 検査できなかったときに緑を返さない。依存コマンド不在・発見 0 件はすべて失敗にする
#   (`_claude/rules/adversarial-review-own-safeguards.md` の false green)。
# ⚠️ paths の検査は「行として現れるか」の静的検査。YAML として解釈しないので、
#   コメントアウトされた paths も通ってしまう。取りこぼすより出す側へ倒す方針は変えない
#   (誤って通す形は下の quoted/unquoted 両対応で狭めてある)。
set -uo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR" || { printf '✗ repo root へ移動できない\n'; exit 1; }

fail() { printf '✗ %s\n' "$1"; exit 1; }

for c in grep find sed basename; do
  command -v "$c" >/dev/null 2>&1 || fail "$c が無い。検査できないので緑にしない"
done
grep -q probe <<< 'probe' || fail "grep が正常に動作しない"

# 出典は Makefile と同じ `src/*/go.mod` (手で列挙しない)。
# ⚠️ glob + `-f` で見る。`find` は symlink を辿らないので、`src/<name>` が symlink のとき
#    make の `wildcard` は見えるのに検査は見えない = 「出典は Makefile と同じ」が崩れる
#    (敵対的レビュー 2026-09-03 の P3-10 で実測)
projects=""
for d in src/*/; do
  [ -f "$d/go.mod" ] || continue
  projects="$projects$(basename "$d")
"
done
projects=$(printf '%s' "$projects" | sort)
[ -n "$projects" ] || fail "src/*/go.mod が 1 件も見つからない (発見の仕方が壊れている)"

bad=0 n=0
while IFS= read -r name; do
  [ -n "$name" ] || continue
  n=$((n + 1))
  mk="src/$name/Makefile"
  wf=".github/workflows/src_$name.yml"

  if [ ! -f "$mk" ]; then
    printf '✗ %s: %s が無い (make -C で lint / test を呼べない)\n' "$name" "$mk"; bad=$((bad + 1)); continue
  fi
  for t in lint test; do
    # ⚠️ 行頭の target 定義だけを見る (`.PHONY: lint test` の行や `test-foo:` に当てない)。
    #    `^$t:` だけだと **make の変数代入 `lint:=x` にも当たる** (敵対的レビュー 2026-09-03 の P2-8)
    if ! grep -qE "^$t:([[:space:]]|\$)" "$mk"; then
      printf '✗ %s: %s に %s: target が無い\n' "$name" "$mk" "$t"; bad=$((bad + 1)); continue
    fi
    # ⚠️ 名前だけあって recipe が空だと「lint が走る」を守れない (同 P3-9)。
    #    target 行の次の行がタブ始まり (recipe) か、依存を持つことを求める
    if ! awk -v t="$t" '
          $0 ~ "^" t ":" { got = 1; deps = $0; sub(/^[^:]*:[[:space:]]*/, "", deps); next }
          got { if ($0 ~ /^\t/ || deps != "") { print "ok"; exit } ; got = 0 }
        ' "$mk" | grep -q ok; then
      printf '✗ %s: %s の %s: が空 (recipe も依存も無い = 実際には何も走らない)\n' "$name" "$mk" "$t"; bad=$((bad + 1))
    fi
  done

  if [ ! -f "$wf" ]; then
    printf '✗ %s: %s が無い (push しても CI が起動しない)\n' "$name" "$wf"; bad=$((bad + 1)); continue
  fi
  # ⚠️ **`paths:` ブロックの中だけ**を見る。行だけ grep すると `paths-ignore:` の下に書かれた
  #    「その dir を除外する」設定を「含む」と読んでしまい、不変条件が反転したまま緑になる
  #    (敵対的レビュー 2026-09-03 の P2-3 で実測)。指定は quoted / unquoted の両方が使われている
  if ! awk -v want="src/$name/" '
        # paths: の行に入ったらブロック開始 (paths-ignore: は入らない)。インデントを覚える
        /^[[:space:]]*paths:[[:space:]]*$/ { inblk = 1; ind = match($0, /[^ ]/); next }
        /^[[:space:]]*paths-ignore:/ { inblk = 0; next }
        inblk {
          if ($0 ~ /^[[:space:]]*#/) next                      # コメント行は読み飛ばす
          if ($0 !~ /^[[:space:]]*-/) { inblk = 0; next }      # リストが終わったらブロック終了
          line = $0
          gsub(/^[[:space:]]*-[[:space:]]*/, "", line)
          gsub(/^['"'"'"]|['"'"'"]$/, "", line)
          if (index(line, want) == 1) { print "ok"; exit }
        }
      ' "$wf" | grep -q ok; then
    printf '✗ %s: %s の paths が src/%s/ を含まない (この変更では CI が走らない)\n' "$name" "$wf" "$name"; bad=$((bad + 1))
  fi
done <<< "$projects"

[ "$n" -gt 0 ] || fail "検査対象が 0 件 (発見が壊れている)"
if [ "$bad" -gt 0 ]; then
  printf '  直し方: src/<name>/Makefile に lint:/test: を足し、.github/workflows/src_<name>.yml を\n'
  printf '  既存の src_*.yml に倣って作る (paths に src/<name>/** と _go-project.yml を含める)\n'
  exit 1
fi
printf '✓ Go プロジェクト %s 件すべてに lint/test target と CI レーン (paths つき) がある\n' "$n"
