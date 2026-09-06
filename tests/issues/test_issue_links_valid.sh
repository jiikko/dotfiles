#!/usr/bin/env bash
# issues/ 配下の md が本文から張る **repo 内の .md への相対リンク**が、全部解決することを検査する。
#
# なぜ: issue の本文リンクは `issues/` 直下を起点に書かれているので、`git mv issues/NNN-x.md
# issues/done/` した瞬間に全部 1 段ずれる (issue 293。2026-09-06 の実測で 192 件 / 79 ファイル)。
# done/ への移動は日常操作なので頻度が高く、しかも切れても誰も痛くないので放置される。
# group issue の完了先が `issues/epic/<name>/done/` になった (issue 291) ぶん段数の種類も増えた。
#
# ## 脅威モデル (何を止めるか)
#
# 「issue を別の状態ディレクトリへ**移したせいで**本文のリンクが切れる」を止める。うっかり
# 起きる典型形が対象で、意図的な迂回 (リンクを文字列連結で組む等) は相手にしない。
#
# ## 検出しない形 (意図的。ここに該当するものは review の責務)
#
#   - コードフェンス (``` 〜 ```) の中とインラインコード (`…`) の中 — 例示であって参照ではない。
#     実測 2 件が該当する (165 の ```diff に貼られた CLAUDE.md の抜粋 / 263 の `](path.md)` という
#     書式の説明)。これを検出すると「直せない指摘」が毎回出る
#   - 外部 URL (`http://` 等)・アンカーのみ (`#…`)・`.md` 以外への参照 (画像・スクリプト)。
#     293 の壊れ方は「issue 間 / rule への md リンク」だけで起きており、実測でも .md 以外は 0 件。
#     広げると「検出しない形」の議論が増えるわりに止まる事故が増えない
#   - `issues/next/` 配下 — claim の **symlink** で、実体は issue ディレクトリ直下にある。
#     symlink の置き場所を基準に解決すると全部切れて見える (実測 6 件が偽陽性)。next/ の健全性は
#     tests/issues/test_next_links_valid.sh が別に検査している (二重にはならない)
#
# ## 検出の作法
#
#   - **深さで絞らない** (`-maxdepth` を使わない)。`issues/epic/<name>/done/` は 4 段目で、
#     深さ決め打ちの検査は新しい段を黙って対象外にする (claude-md-maintenance.md の実例)
#   - **ディレクトリ名で絞らない** (`done` / `pending` 決め打ちにしない)。予約外の綴り
#     (`closed/` 等) の配下にも md は在りうる (issue 291 でそれを迷子として一覧に出すようにした)
#   - **canary を本走査と同じ関数に通す** (extract_links / check_file)。抽出が空を返すと
#     「違反 0 件 = 緑」になるので、既知の入力で既知の答えが出ることを先に固定する
#     (verify-execution-not-just-exit-code.md)。式をコピーして別に書かないこと — コピーすると
#     canary は「コピーしたロジック」を検査するだけになり、本走査の破損を検出しない
#   - **走査件数の下限**を置く。抽出が壊れて 0 件になっても緑になるのを塞ぐ
#
# 変異検証のため、検査対象ディレクトリを第 1 引数で差し替えられる (canary は常に自前の temp で走る)。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR" || exit 1

issues_dir="${1:-issues}"
while [ "${issues_dir%/}" != "$issues_dir" ]; do issues_dir="${issues_dir%/}"; done
if [ ! -d "$issues_dir" ]; then
  printf '✗ 検査対象ディレクトリが無い: %s\n' "$issues_dir" >&2
  exit 1
fi

# 本走査で使う下限。issues/ の実測 (2026-09-06: md 298 本 / リンク 357 本) に対して、
# 半分を切ったら「抽出が壊れた」と読む。実数に近づけると新しい issue のたびに更新が要るので、
# 桁が落ちたことだけを見る
MIN_FILES=100
MIN_LINKS=100

# extract_links はファイル本文から検査対象のリンクだけを抜く (コードフェンス / インラインコードを除く)。
# 🚨 canary と本走査の**両方**がこの関数を通る。ここを直したら canary が落ちる
extract_links() {
  # 🚨 フェンスと見なすのは「バッククォート 3 つ + 言語名だけ」の行に限る。`^```` で判定すると、
  # ```` ``` ```` のような **4 連バッククォートのインラインコード**(``` そのものを本文で説明する
  # 書き方) を開始フェンスと読んでしまい、そこから先が全部フェンス内になって検査から落ちる
  # (実測 2026-09-06: issues/done/165 がこの形で、切れたリンク 1 本を最初の実装が見逃した)
  awk '
    /^[[:space:]]*```[A-Za-z0-9_-]*[[:space:]]*$/ { fence = !fence; next }
    fence { next }
    { gsub(/`[^`]*`/, ""); print }
  ' "$1" | grep -oE '\]\((\.\./)*[A-Za-z0-9_./-]+\.md\)' | sed 's/^](//; s/)$//' || true
}

# scan_file は 1 ファイルの各リンクを "OK|file|link" / "BAD|file|link" で出す。
# 🚨 件数を変数で数えない: 呼び出し側は $(...) で受けるので**サブシェル**になり、中で増やした
# カウンタは消える (最初にそう書いて canary が「0 件」で落ちた。判定不能を緑にしない形が効いた)。
# 数えるのは呼び出し側で、出力行から数える
scan_file() {
  local f="$1" d link
  d=$(dirname "$f")
  while IFS= read -r link; do
    [ -n "$link" ] || continue
    if [ -e "$d/$link" ]; then
      printf 'OK|%s|%s\n' "$f" "$link"
    else
      printf 'BAD|%s|%s\n' "$f" "$link"
    fi
  done < <(extract_links "$f")
}

# --- canary: 既知の入力で既知の答えが出ることを、本走査の前に固定する ---------------------
canary_dir=$(mktemp -d)
trap 'rm -rf "$canary_dir"' EXIT
mkdir -p "$canary_dir/done"
: > "$canary_dir/010-feat-target.md"
cat > "$canary_dir/done/011-feat-canary.md" <<'CANARY'
- 解決するリンク: [target](../010-feat-target.md)
- 切れているリンク: [moved](010-feat-target.md)
- インラインコードの例示: `](path.md)` は書式の説明なので数えない

```diff
+[fenced](../nonexistent-in-fence.md)
```

ここは ```` ``` ```` を本文で説明する行 (フェンスの開始ではない)
- フェンス判定が反転していたら、この先は全部読み飛ばされる: [after](../nonexistent-after.md)
CANARY
canary_out=$(scan_file "$canary_dir/done/011-feat-canary.md" || true)
canary_all=$(printf '%s' "$canary_out" | grep -c . || true)
canary_bad=$(printf '%s' "$canary_out" | grep -c '^BAD|' || true)
if [ "$canary_all" -ne 3 ]; then
  printf '✗ canary で数えたリンクが 3 件でない: %d (フェンス / インラインコードの除去がずれている)\n%s\n' \
    "$canary_all" "$canary_out" >&2
  exit 1
fi
if [ "$canary_bad" -ne 2 ] ||
  ! grep -q '^BAD|.*|010-feat-target.md$' <<< "$canary_out" ||
  ! grep -q '^BAD|.*|\.\./nonexistent-after.md$' <<< "$canary_out"; then
  printf '✗ canary の判定が想定と違う (切れている 2 件だけを検出するはず):\n%s\n' "$canary_out" >&2
  exit 1
fi

# --- 本走査 -------------------------------------------------------------------------------
files=$(find "$issues_dir" -name '*.md' -type f -not -path "$issues_dir/next/*" -print | sort) ||
  { printf '✗ find が失敗した\n' >&2; exit 1; }
file_count=$(printf '%s' "$files" | grep -c . || true)

bad=0
checked_links=0
while IFS= read -r f; do
  [ -n "$f" ] || continue
  while IFS= read -r hit; do
    [ -n "$hit" ] || continue
    checked_links=$((checked_links + 1))
    case "$hit" in
      BAD\|*)
        printf '✗ リンクが解決しない: %s\n' "${hit#BAD|}" >&2
        bad=$((bad + 1)) ;;
    esac
  done < <(scan_file "$f")
done <<< "$files"

if [ "$issues_dir" = "issues" ]; then
  if [ "$file_count" -lt "$MIN_FILES" ] || [ "$checked_links" -lt "$MIN_LINKS" ]; then
    printf '✗ 走査件数が少なすぎる (抽出が壊れた疑い): md %d 本 / リンク %d 本 (下限 %d / %d)\n' \
      "$file_count" "$checked_links" "$MIN_FILES" "$MIN_LINKS" >&2
    exit 1
  fi
fi

if [ "$bad" -gt 0 ]; then
  printf '✗ issue の相対リンク: %d 件が解決しない (md %d 本 / リンク %d 本を検査)\n' \
    "$bad" "$file_count" "$checked_links" >&2
  printf '  直し方: リンクは**そのファイルの位置**からの相対で書く。issues/done/NNN.md から\n' >&2
  printf '  _claude/rules/x.md を指すなら ../../_claude/rules/x.md (issues/epic/<name>/done/ なら 4 段)\n' >&2
  exit 1
fi
printf '✓ issue の相対リンク: md %d 本 / リンク %d 本を検査、切れなし (%s)\n' \
  "$file_count" "$checked_links" "$issues_dir"
