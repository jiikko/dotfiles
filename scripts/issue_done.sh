#!/usr/bin/env bash
# issue_done.sh — issue を done/ へ送る 4 手順を 1 コマンドに寄せる (issue 347)。
#
#   scripts/issue_done.sh <NNN> [<NNN>...]
#
# ## なぜ 1 本に寄せるか
#
# done/ へ移すには複数の操作が要るのに、**どれも人が覚えているだけ**だった。実測 2026-09-09
# (retro 345): 1 セッションで 12 件を送り、**4 回**取りこぼした (claim symlink の消し忘れ 2 回 /
# 相対リンクの depth 調整 2 回)。うち 1 回は CI の Tests を赤くした (issue 313)。
# 検査 (tests/issues/test_*.sh) は「落とした後」にしか気づかせてくれない。
#
# ## やること (4 つ。issue 347 は 3 つと書いていたが、実測すると 4 つ目が要る)
#
#   1. 実体を見つけて対応する done/ へ `git mv`
#      (global issue → issues/done/ / group issue → issues/epic/<name>/done/。issue 291)
#   2. `next/` の claim symlink があれば `git rm` (残すと dangling で test_next_links_valid.sh が赤)
#   3. 移した本文の相対リンクを**新しい位置から**張り直す (issues/ 起点で書かれているので 1 段ずれる)
#   4. 🚨 **他の issue から張られている「この issue へのリンク」も張り直す**。issue 347 の本文には
#      無いが、実測すると必要 (例: issues/345 が `](347-….md)` で参照しており、347 を動かすと
#      その 1 本が切れて test_issue_links_valid.sh が赤くなる)。3 だけでは CI は緑にならない
#
# ## 3 と 4 は同じ計算の裏表なので 1 つの awk に寄せた
#
# 「ファイル F の中のリンク L」は dir(F) からの相対。やることは常に
#   ① L を **解決前のディレクトリ**で絶対化して指し先 T を出す
#   ② T が今回動かす issue そのものなら移動後のパスへ差し替える
#   ③ **解決後のディレクトリ**から T への相対に書き直す
# だけで、移動した当人 (①と③のディレクトリが違う) と他のファイル (同じ) を同じコードが扱う。
# 素朴に「`](../` を `](../../` へ置換」にすると、`](done/312-x.md)` のような**下向きの
# リンク**を落とす (実在する形。issues/340 が done/312 を指している)。
#
# ## 安全機構 (adversarial-review-own-safeguards.md)
#
#   - **canary**: 本走査と同じ awk に既知の入力を通し、既知の答えが出ることを**触る前に**固定する。
#     抽出が空を返すと「1 件も書き換えないまま成功」になるので、そこを塞ぐ
#   - **baseline**: 触る前にリンク検査を回し、**既に赤いなら着手しない** (判定不能を緑に畳まない)
#   - **rollback**: 移動後の検査が赤なら、やった操作を逆順に戻す (半端な状態で終わらせない)。
#     戻しは `git checkout --` を使わず**逆操作を明示**する (並行セッションの未コミット変更を
#     巻き込まないため。mutation-verify-new-tests.md「復元の作法」)
#
# ## 変異検証で red を確認した箇所 (2026-09-10)
#
#   手順 2 を外す → test_next_links_valid.sh が red / 手順 3 を外す・手順 4 を外す →
#   test_issue_links_valid.sh が red。詳細は tests/issues/test_issue_done.sh。
#
# 検査対象ディレクトリは第 1 引数ではなく **ISSUES_DIR** で差し替える (テスト・変異検証用)。
# ISSUES_DIR を repo 外の fixture へ向けると、その fixture の git repo に対して操作する。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ISSUES_DIR="${ISSUES_DIR:-$ROOT_DIR/issues}"
while [ "${ISSUES_DIR%/}" != "$ISSUES_DIR" ]; do ISSUES_DIR="${ISSUES_DIR%/}"; done

usage() {
  cat >&2 <<'USAGE'
usage: scripts/issue_done.sh <NNN> [<NNN>...]

  issue を done/ へ送る (移動 / claim symlink の削除 / 本文リンクの張り直し /
  他 issue からの参照の張り直し) を 1 コマンドで行い、リンク検査が落ちたら移動を戻す。

  ISSUES_DIR=<dir>   検査・操作の対象ディレクトリ (既定: <repo>/issues)
USAGE
}

# ---------------------------------------------------------------------------
# リンク書き換えの本体。canary と本走査の**両方**がこの awk を通る。
#   -v old_base : そのファイルの中のリンクを解決するディレクトリ (repo 相対)
#   -v new_base : 書き直すときの起点ディレクトリ (repo 相対)。動かした当人だけ old と違う
#   -v moved_old / -v moved_new : 今回動かす issue の repo 相対パス (空なら差し替えなし)
# 出力は書き換え後の全文。1 本でも書き換えたら stderr へ "REWROTE" を出す (呼び出し側が数える)。
# ---------------------------------------------------------------------------
AWK_REBASE='
function normalize(p,   n, i, a, st, top, out) {
  n = split(p, a, "/")
  top = 0
  for (i = 1; i <= n; i++) {
    if (a[i] == "" || a[i] == ".") continue
    if (a[i] == "..") {
      if (top > 0 && st[top] != "..") { top-- } else { st[++top] = ".." }
      continue
    }
    st[++top] = a[i]
  }
  out = ""
  for (i = 1; i <= top; i++) out = (out == "" ? st[i] : out "/" st[i])
  return out
}
function relpath(target, base,   nt, nb, t, b, i, up, out) {
  if (base == "") return target
  nt = split(target, t, "/")
  nb = split(base, b, "/")
  i = 1
  while (i <= nt && i <= nb && t[i] == b[i]) i++
  out = ""
  for (up = i; up <= nb; up++) out = out "../"
  for (; i <= nt; i++) out = out t[i] (i < nt ? "/" : "")
  return out
}
function newlink(lnk,   frag, path, t, nl) {
  frag = ""
  if (index(lnk, "#") > 0) { frag = substr(lnk, index(lnk, "#")); path = substr(lnk, 1, index(lnk, "#") - 1) }
  else path = lnk
  if (path == "") return lnk
  t = normalize(old_base "/" path)
  # repo の外へ出るリンクは触らない (計算の外なので、黙って壊すより残す)
  if (substr(t, 1, 3) == "../" || t == "..") return lnk
  if (moved_old != "" && t == moved_old) t = moved_new
  else if (old_base == new_base) return lnk   # 動かした当人以外は、指し先が変わらないなら触らない
  nl = relpath(t, new_base)
  if (nl == "") return lnk
  return nl frag
}
function process(seg,   out, rest, pre, whole, lnk, nl) {
  out = ""; rest = seg
  while (match(rest, /\]\((\.\.\/)*[A-Za-z0-9_.\/-]+\.[A-Za-z0-9]+(#[A-Za-z0-9_-]+)?\)/)) {
    pre   = substr(rest, 1, RSTART - 1)
    whole = substr(rest, RSTART, RLENGTH)
    rest  = substr(rest, RSTART + RLENGTH)
    lnk   = substr(whole, 3, length(whole) - 3)
    nl    = newlink(lnk)
    if (nl != lnk) changed++
    out = out pre "](" nl ")"
  }
  return out rest
}
# インラインコード (`…`) の中は書式の説明であって参照ではない (test_issue_links_valid.sh と同じ扱い)
function rewrite_line(line,   out, i, j) {
  out = ""
  while ((i = index(line, "`")) > 0) {
    j = index(substr(line, i + 1), "`")
    if (j == 0) break
    out = out process(substr(line, 1, i - 1)) substr(line, i, j + 1)
    line = substr(line, i + j + 1)
  }
  return out process(line)
}
# コードフェンスの中も同じ理由で触らない (判定は test_issue_links_valid.sh の extract_links と同形)
/^[[:space:]]*```[^`]*$/ { fence = !fence; print; next }
/^[[:space:]]*~~~/       { fence = !fence; print; next }
fence { print; next }
{ print rewrite_line($0) }
END { if (changed > 0) print "REWROTE " changed > "/dev/stderr" }
'

# rebase_file <file> <old_base> <new_base> <moved_old> <moved_new>
# 書き換えたら 0、変化が無ければ 1 を返す。書き込みは一時ファイル経由 (途中で死んでも半端に残さない)
rebase_file() {
  local f="$1" old_base="$2" new_base="$3" moved_old="$4" moved_new="$5" tmp rc=0
  tmp="$(mktemp "${TMPDIR:-/tmp}/issue_done.XXXXXX")"
  awk -v old_base="$old_base" -v new_base="$new_base" \
      -v moved_old="$moved_old" -v moved_new="$moved_new" \
      "$AWK_REBASE" "$f" > "$tmp" 2>"$tmp.err" || { rm -f "$tmp" "$tmp.err"; return 2; }
  if grep -q '^REWROTE ' "$tmp.err" 2>/dev/null; then
    cat "$tmp" > "$f"
  else
    rc=1
  fi
  rm -f "$tmp" "$tmp.err"
  return "$rc"
}

# ---------------------------------------------------------------------------
# canary — 本走査と同じ awk に既知の入力を通し、既知の答えが出ることを触る前に固定する。
# 🚨 式をコピーして別に書かないこと (コピーすると本走査の破損を検出しない)。
# ---------------------------------------------------------------------------
canary() {
  local got want
  got="$(printf '%s\n' \
    '上向き: [rule](../_claude/rules/x.md)' \
    '下向き: [old](done/312-x.md)' \
    '当人へ: [self](345-retro.md)' \
    'アンカー: [a](../docs/y.md#sec)' \
    '外部: [u](https://example.com/z.md)' \
    'インライン: `](path.md)` は書式の説明' \
    '```sh' \
    'フェンス内: [f](../_claude/rules/x.md)' \
    '```' \
    | awk -v old_base="issues" -v new_base="issues/done" \
          -v moved_old="issues/345-retro.md" -v moved_new="issues/done/345-retro.md" \
          "$AWK_REBASE" 2>/dev/null)"
  want='上向き: [rule](../../_claude/rules/x.md)
下向き: [old](312-x.md)
当人へ: [self](345-retro.md)
アンカー: [a](../../docs/y.md#sec)
外部: [u](https://example.com/z.md)
インライン: `](path.md)` は書式の説明
```sh
フェンス内: [f](../_claude/rules/x.md)
```'
  if [ "$got" != "$want" ]; then
    printf '✗ canary: リンク書き換えの結果が想定と違う (書き換えロジックが壊れている)\n' >&2
    diff <(printf '%s\n' "$want") <(printf '%s\n' "$got") >&2 || true
    return 1
  fi
  # 逆向き (動かした当人以外のファイルから、動いた issue を指す) も同じ awk で固定する
  got="$(printf '%s\n' '参照: [x](347-x.md) と [y](340-y.md)' \
    | awk -v old_base="issues" -v new_base="issues" \
          -v moved_old="issues/347-x.md" -v moved_new="issues/done/347-x.md" \
          "$AWK_REBASE" 2>/dev/null)"
  if [ "$got" != '参照: [x](done/347-x.md) と [y](340-y.md)' ]; then
    printf '✗ canary: 他ファイルからの参照の張り直しが想定と違う: %s\n' "$got" >&2
    return 1
  fi
  return 0
}

run_link_tests() {
  local out rc=0
  out="$("$ROOT_DIR/tests/issues/test_issue_links_valid.sh" "$ISSUES_DIR" 2>&1)" || rc=1
  printf '%s\n' "$out" > "$TESTLOG/links.txt"
  out="$("$ROOT_DIR/tests/issues/test_next_links_valid.sh" "$ISSUES_DIR" 2>&1)" || rc=1
  printf '%s\n' "$out" > "$TESTLOG/next.txt"
  return "$rc"
}

[ $# -gt 0 ] || { usage; exit 2; }
for n in "$@"; do
  case "$n" in
    [0-9][0-9][0-9]) ;;
    -h|--help) usage; exit 0 ;;
    *) printf '✗ issue 番号は 3 桁で指定する: %s\n' "$n" >&2; exit 2 ;;
  esac
done
[ -d "$ISSUES_DIR" ] || { printf '✗ 対象ディレクトリが無い: %s\n' "$ISSUES_DIR" >&2; exit 1; }

canary || exit 1

TESTLOG="$(mktemp -d "${TMPDIR:-/tmp}/issue_done_log.XXXXXX")"
trap 'rm -rf "$TESTLOG"' EXIT

# repo 相対のパスを組み立てるための起点 (ISSUES_DIR が repo 外の fixture でも動くように、
# 「ISSUES_DIR の親」を repo root と見なす)
BASE_DIR="$(cd "$ISSUES_DIR/.." && pwd -P)"
ISSUES_REL="$(basename "$ISSUES_DIR")"
GIT_DIR_ARG=(-C "$BASE_DIR")

# --- baseline: 触る前に赤いなら着手しない (自分の変更のせいに見える赤を作らない) -----------
if ! run_link_tests; then
  printf '✗ 着手前のリンク検査が既に赤い。先にそちらを直すこと (何も動かしていない)\n' >&2
  cat "$TESTLOG/links.txt" "$TESTLOG/next.txt" >&2
  exit 1
fi

for num in "$@"; do
  # --- 実体を 1 件に確定させる (done/ に在るものは対象外) ---------------------------------
  matches="$(find "$ISSUES_DIR" -type f -name "$num-*.md" -print | grep -v '/done/' | sort || true)"
  count="$(printf '%s' "$matches" | grep -c . || true)"
  if [ "$count" -eq 0 ]; then
    printf '✗ issue %s の実体が見つからない (既に done/ に在る?)\n' "$num" >&2; exit 1
  fi
  if [ "$count" -ne 1 ]; then
    printf '✗ issue %s の実体が %d 件ある (番号の衝突):\n%s\n' "$num" "$count" "$matches" >&2; exit 1
  fi
  src="$matches"
  base="$(basename "$src")"
  srcdir="$(dirname "$src")"

  # group root = ISSUES_DIR か ISSUES_DIR/epic/<name>。done は必ずその直下 (issue 291)
  rel_srcdir="${srcdir#"$ISSUES_DIR"}"; rel_srcdir="${rel_srcdir#/}"
  case "$rel_srcdir" in
    '' | pending | waiting | next) group_root="$ISSUES_DIR" ;;
    epic/*/pending | epic/*/waiting | epic/*/next)
      group_root="$ISSUES_DIR/$(printf '%s' "$rel_srcdir" | cut -d/ -f1,2)" ;;
    epic/*) group_root="$ISSUES_DIR/$rel_srcdir" ;;
    *) printf '✗ issue %s の置き場所を解釈できない: %s\n' "$num" "$srcdir" >&2; exit 1 ;;
  esac
  destdir="$group_root/done"
  dest="$destdir/$base"
  [ ! -e "$dest" ] || { printf '✗ 移動先に同名が既に在る: %s\n' "$dest" >&2; exit 1; }

  old_base="$ISSUES_REL${srcdir#"$ISSUES_DIR"}"
  new_base="$ISSUES_REL${destdir#"$ISSUES_DIR"}"
  moved_old="$old_base/$base"
  moved_new="$new_base/$base"

  # --- 手順 1: 移動 ------------------------------------------------------------------------
  mkdir -p "$destdir"
  git "${GIT_DIR_ARG[@]}" mv -- "$src" "$dest"
  undo_mv=1

  # --- 手順 2: claim symlink の削除 ---------------------------------------------------------
  claim="$group_root/next/$base"
  undo_claim=0
  if [ -L "$claim" ]; then
    git "${GIT_DIR_ARG[@]}" rm -q --force -- "$claim"
    undo_claim=1
  fi

  # --- 手順 3: 移した本文のリンクを新しい位置から張り直す -----------------------------------
  cp -- "$dest" "$TESTLOG/moved.bak"
  rebase_file "$dest" "$old_base" "$new_base" "$moved_old" "$moved_new" || true

  # --- 手順 4: 他の md からの「この issue への参照」を張り直す --------------------------------
  : > "$TESTLOG/touched.txt"
  idx=0
  while IFS= read -r other; do
    [ -n "$other" ] || continue
    [ "$other" != "$dest" ] || continue
    other_base="$ISSUES_REL${other#"$ISSUES_DIR"}"; other_base="$(dirname "$other_base")"
    idx=$((idx + 1))
    cp -- "$other" "$TESTLOG/bak.$idx"
    if rebase_file "$other" "$other_base" "$other_base" "$moved_old" "$moved_new"; then
      printf '%s\t%s\n' "$idx" "$other" >> "$TESTLOG/touched.txt"
    else
      rm -f -- "$TESTLOG/bak.$idx"
    fi
  done < <(find "$ISSUES_DIR" -type f -name '*.md' -print | sort)

  # --- 検査。落ちたら逆順に戻す -------------------------------------------------------------
  if run_link_tests; then
    touched="$(grep -c . "$TESTLOG/touched.txt" || true)"
    printf '✓ issue %s -> %s' "$num" "${dest#"$BASE_DIR/"}"
    [ "$undo_claim" -eq 1 ] && printf ' / claim symlink を削除'
    [ "$touched" -gt 0 ] && printf ' / 参照を張り直した md %s 本' "$touched"
    printf '\n'
  else
    printf '✗ リンク検査が落ちたので移動を戻す (issue %s)\n' "$num" >&2
    cat "$TESTLOG/links.txt" "$TESTLOG/next.txt" >&2
    # 🚨 逆操作は `&&` で繋がない。`set -e` の下では AND リストの最後の要素にだけ errexit が
    # 効くので、途中の 1 つ (例: 既に在る symlink への ln) が落ちると**残りの戻しが走らない**。
    # 実測 2026-09-10: 変異 [no-unclaim] で「claim は消えていないのに戻そうとして ln が失敗 →
    # git mv の戻しに到達せず、done/ に置き去り」が出た。各手順を独立した if で回し、
    # 失敗しても次へ進んで最後に報告する。
    rb_fail=0
    while IFS=$'\t' read -r bak_idx other; do
      [ -n "$other" ] || continue
      cp -- "$TESTLOG/bak.$bak_idx" "$other" || rb_fail=1
    done < "$TESTLOG/touched.txt"
    cp -- "$TESTLOG/moved.bak" "$dest" || rb_fail=1
    if [ "$undo_claim" -eq 1 ] && [ ! -e "$claim" ]; then
      # git rm は空になった next/ ごと消すので、ディレクトリから作り直す
      mkdir -p "$(dirname "$claim")" || rb_fail=1
      if ln -s "../$base" "$claim"; then
        git "${GIT_DIR_ARG[@]}" add -- "$claim" || rb_fail=1
      else
        rb_fail=1
      fi
    fi
    if [ "$undo_mv" -eq 1 ] && [ -e "$dest" ]; then
      git "${GIT_DIR_ARG[@]}" mv -f -- "$dest" "$src" || rb_fail=1
    fi
    if [ "$rb_fail" -ne 0 ]; then
      printf '🚨 rollback が完全には戻せなかった。git status を見て手で直すこと\n' >&2
      exit 2
    fi
    exit 1
  fi
done
