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
#   - 外部 URL (`http://` 等) とアンカーのみ (`#…`)。`x.md#sec` のようなアンカー付きは
#     **アンカーを落として** ファイルの実在を見る (書けば無検査になる穴を残さないため)
#   - **symlink** (`-type f` が落とす)。`issues/next/` の claim がこれで、実体は issue ディレクトリ
#     直下にある。symlink の置き場所を基準に解決すると全部切れて見える (実測 6 件が偽陽性)。
#     next/ の健全性は tests/issues/test_next_links_valid.sh が別に検査している。
#     🚨 旧運用で next/ に**実ファイル**として置かれた claim は検査する (symlink ではないので)
#   - **4 スペース字下げのコードブロック** — markdown のリスト継続行と区別できず、落とすと
#     本物のリンクまで無言で消える。落とさない側へ倒す (誤検出したら fence か inline code で囲む)
#
# 🚨 `.md` 以外への参照 (`../_nviminit.lua` 等) も**検査する**。当初「実測 0 件」と書いていたが
# **誤り**で、同じ壊れ方が 20 件現存した (`issues/done/012` に 19 / `187` に 1。2026-09-06 の
# 敵対的レビューが実測)。同一原因なので同じ検査で見る
#
# ## 検出の作法
#
#   - **深さで絞らない** (`-maxdepth` を使わない)。`issues/epic/<name>/done/` は 4 段目で、
#     深さ決め打ちの検査は新しい段を黙って対象外にする (claude-md-maintenance.md の実例)
#   - **ディレクトリ名で絞らない** (`done` / `pending` 決め打ちにしない)。予約外の綴り
#     (`closed/` 等) の配下にも md は在りうる (issue 291 でそれを迷子として一覧に出すようにした)
#   - **canary を本走査と同じ関数に通す** (extract_links / scan_file)。抽出が空を返すと
#     「違反 0 件 = 緑」になるので、既知の入力で既知の答えが出ることを先に固定する
#     (verify-execution-not-just-exit-code.md)。式をコピーして別に書かないこと — コピーすると
#     canary は「コピーしたロジック」を検査するだけになり、本走査の破損を検出しない
#   - **走査件数の下限**を置く。抽出が壊れて 0 件になっても緑になるのを塞ぐ
#   - **報告経路まで対照で通す**。canary が見るのは抽出と判定までで、その先 (BAD の分類 /
#     件数の加算 / 最後の exit 1) は無検査だった。自分自身を陽性 (切れリンク在り) / 陰性
#     (切れリンク無し) / 深さ (4 段目にだけ切れリンク) / 0 件 / 読めないファイル の 5 つの
#     fixture へ通して rc を見る (ISSUE_LINKS_SELFTEST=1 で再帰を止める)
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
# 🚨 総数の下限は**部分的な取りこぼしに構造的に反応しない** (実測 2026-09-06: 深さで絞る変異も
# フェンスの取りこぼしも、リンク総数が健全時と一致したまま素通りした)。下限は「抽出が丸ごと
# 壊れた」だけを見る安全網で、部分的な穴は上の対照 (深さ / フェンス / 0 件 / 読めない) が見る
MIN_FILES=100
MIN_LINKS=100

# extract_links はファイル本文から検査対象のリンクだけを抜く (コードフェンス / インラインコードを除く)。
# 🚨 canary と本走査の**両方**がこの関数を通る。ここを直したら canary が落ちる
extract_links() {
  # フェンスの判定は 2 条件:
  #   ① 行頭 (字下げ可) がバッククォート 3 つ、**その後にバッククォートが無い**
  #      → ```sh / ```diff / ```sh title="x" は開始、``` は終了
  #      🚨 「言語名だけ」に限定すると、info string に空白や属性が入った行を開始と読まず、
  #      閉じの ``` が開始になって**そのファイルの残り全部が無言で落ちる** (実測 2026-09-06:
  #      リンク総数が健全時と一致するので件数の下限でも見えない)
  #      🚨 バッククォート 4 つ以上の行 (```` ``` ```` = ``` 自体を本文で説明する書き方) は
  #      「その後にバッククォートが有る」ので開始にならない (issues/done/165 が実在の形)
  #   ② `~~~` フェンス (CommonMark の正当な形)
  awk '
    /^[[:space:]]*```[^`]*$/ { fence = !fence; next }
    /^[[:space:]]*~~~/       { fence = !fence; next }
    fence { next }
    { gsub(/`[^`]*`/, ""); print }
  ' "$1" |
    grep -oE '\]\((\.\./)*[A-Za-z0-9_./-]+\.[A-Za-z0-9]+(#[A-Za-z0-9_-]+)?\)' |
    sed 's/^](//; s/)$//; s/#.*$//' || true
}

# scan_file は 1 ファイルの各リンクを "OK|file|link" / "BAD|file|link" で出す。
# 🚨 件数を変数で数えない: 呼び出し側は $(...) で受けるので**サブシェル**になり、中で増やした
# カウンタは消える (最初にそう書いて canary が「0 件」で落ちた。判定不能を緑にしない形が効いた)。
# 数えるのは呼び出し側で、出力行から数える
scan_file() {
  local f="$1" d link
  d=$(dirname "$f")
  # 🚨 読めないファイルを「リンク 0 件 = 合格」に畳まない (判定不能は第 3 の結果)
  if [ ! -r "$f" ]; then
    printf 'UNREADABLE|%s|\n' "$f"
    return 0
  fi
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
mkdir -p "$canary_dir/done" "$canary_dir/epic/x/done"
: > "$canary_dir/010-feat-target.md"
cat > "$canary_dir/done/011-feat-canary.md" <<'CANARY'
- 解決するリンク: [target](../010-feat-target.md)
- 切れているリンク: [moved](010-feat-target.md)
- インラインコードの例示: `](path.md)` は書式の説明なので数えない

```diff
+[fenced](../nonexistent-in-fence.md)
```

```sh title="info string に空白と属性が入る形"
[fenced2](../nonexistent-in-fence2.md)
```
```` ``` ```` は ``` 自体を本文で説明する書き方 (行頭がバッククォート 4 つ。フェンスではない)
- フェンス判定が反転していたら、この先は全部読み飛ばされる: [after](../nonexistent-after.md)
- .md 以外も見る: [script](../nonexistent.sh)
- アンカー付きは落として見る: [anchor](../010-feat-target.md#section)
CANARY
# 🚨 深さで絞らないことを canary で固定する (issues/epic/<name>/done/ は 4 段目。
# -maxdepth を足す変異は、実データに epic が 1 件も無いと素通りする — 敵対レビュー 2026-09-06)
printf '[deep](../../../nonexistent-deep.md)\n' > "$canary_dir/epic/x/done/900-feat-deep.md"
canary_out=$(
  find "$canary_dir" -name '*.md' -type f -print | sort | while IFS= read -r cf; do scan_file "$cf"; done
)
canary_all=$(printf '%s' "$canary_out" | grep -c . || true)
canary_bad=$(printf '%s' "$canary_out" | grep -c '^BAD|' || true)
if [ "$canary_all" -ne 6 ]; then
  printf '✗ canary で数えたリンクが 6 件でない (フェンス / インラインコード / 深さの扱いがずれている): %d (フェンス / インラインコードの除去がずれている)\n%s\n' \
    "$canary_all" "$canary_out" >&2
  exit 1
fi
if [ "$canary_bad" -ne 4 ] ||
  ! grep -q '^BAD|.*|010-feat-target.md$' <<< "$canary_out" ||
  ! grep -q '^BAD|.*|\.\./nonexistent-after.md$' <<< "$canary_out" ||
  ! grep -q '^BAD|.*|\.\./nonexistent.sh$' <<< "$canary_out" ||
  ! grep -q '^BAD|.*|\.\./\.\./\.\./nonexistent-deep.md$' <<< "$canary_out"; then
  printf '✗ canary の判定が想定と違う (切れている 4 件だけを検出するはず):\n%s\n' "$canary_out" >&2
  exit 1
fi

# --- 陽性 / 陰性対照: **報告経路 (BAD の分類 → 件数 → exit 1) まで**を自分で通す ------------
# 🚨 canary が通すのは抽出と判定までで、その先 (case BAD|* / bad の加算 / 最後の exit 1) は
# 何も検査していなかった。3 つの変異 (分類の綴りを変える / 加算を +0 にする / exit 0 にする) が
# すべて素通りした (2026-09-06 の敵対的レビュー P1-1)。ここで自分自身を呼んで rc を見る
if [ "${ISSUE_LINKS_SELFTEST:-}" != "1" ]; then
  if ISSUE_LINKS_SELFTEST=1 "$0" "$canary_dir" >/dev/null 2>&1; then
    printf '✗ 陽性対照が緑になった (切れリンクを持つ canary を通してしまう = 報告経路が壊れている)\n' >&2
    exit 1
  fi
  # 深さの対照: **4 段目にしか切れリンクが無い** fixture。ここが緑なら深さで絞られている
  # (canary の epic ファイルだけでは、浅い側の違反が rc=1 を作るので -maxdepth の変異が素通りする)
  deep_dir=$(mktemp -d)
  mkdir -p "$deep_dir/epic/x/done"
  : > "$deep_dir/010-feat-target.md"
  printf '[deep](../../../nonexistent-deep.md)\n' > "$deep_dir/epic/x/done/900-feat-deep.md"
  if ISSUE_LINKS_SELFTEST=1 "$0" "$deep_dir" >/dev/null 2>&1; then
    printf '✗ 4 段目 (issues/epic/<name>/done/) の切れリンクを見逃した (深さで絞っている)\n' >&2
    rm -rf "$deep_dir"; exit 1
  fi
  rm -rf "$deep_dir"

  # 0 件の対照: 対象が 1 件も無いのを合格にしていないか (抽出が壊れたときの形)
  empty_dir=$(mktemp -d)
  if ISSUE_LINKS_SELFTEST=1 "$0" "$empty_dir" >/dev/null 2>&1; then
    printf '✗ 対象 0 件を合格にしている\n' >&2
    rm -rf "$empty_dir"; exit 1
  fi
  rm -rf "$empty_dir"

  # 読めないファイルの対照: 判定不能を緑に畳んでいないか (§2「沈黙 = 成功」)
  unread_dir=$(mktemp -d)
  printf '[ok](x.md)\n' > "$unread_dir/010-feat-unreadable.md"
  chmod 000 "$unread_dir/010-feat-unreadable.md"
  if [ -r "$unread_dir/010-feat-unreadable.md" ]; then
    printf '! 読めないファイルの対照を飛ばした (root で走っている): %s\n' "$unread_dir" >&2
  elif ISSUE_LINKS_SELFTEST=1 "$0" "$unread_dir" >/dev/null 2>&1; then
    chmod 644 "$unread_dir/010-feat-unreadable.md"; rm -rf "$unread_dir"
    printf '✗ 読めないファイルを合格にしている (判定不能を緑に畳んでいる)\n' >&2
    exit 1
  fi
  chmod 644 "$unread_dir/010-feat-unreadable.md" 2>/dev/null || true
  rm -rf "$unread_dir"

  clean_dir=$(mktemp -d)
  mkdir -p "$clean_dir/done"
  : > "$clean_dir/010-feat-target.md"
  printf '[ok](../010-feat-target.md)\n' > "$clean_dir/done/011-feat-ok.md"
  if ! ISSUE_LINKS_SELFTEST=1 "$0" "$clean_dir" >/dev/null 2>&1; then
    printf '✗ 陰性対照が赤になった (正しいリンクを落としている)\n' >&2
    rm -rf "$clean_dir"; exit 1
  fi
  rm -rf "$clean_dir"
fi

# --- 本走査 -------------------------------------------------------------------------------
# 🚨 next/ を -not -path で外さない: symlink は -type f が落とすので、外すと**旧運用で next/ に
# 実ファイルとして置かれた claim** だけが無検査になる (敵対レビュー 2026-09-06 P3-2)
files=$(find "$issues_dir" -name '*.md' -type f -print | sort) ||
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
      UNREADABLE\|*)
        printf '✗ ファイルを読めない (判定不能。合格にはしない): %s\n' "${hit#UNREADABLE|}" >&2
        bad=$((bad + 1)) ;;
    esac
  done < <(scan_file "$f")
done <<< "$files"

if [ "$file_count" -eq 0 ]; then
  printf '✗ 検査対象の md が 0 件 (走査が壊れているか、対象ディレクトリが違う): %s\n' "$issues_dir" >&2
  exit 1
fi
# 下限は repo の issues/ を見ているときだけ (canary / 変異検証の temp では効かせない)。
# 🚨 判定は文字列比較でなく実体で行う (`./issues` と書くだけで無効化される形にしない)
if [ "$(cd "$issues_dir" && pwd -P)" = "$ROOT_DIR/issues" ]; then
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
