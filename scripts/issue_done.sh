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
# ## 手順 4 の走査は **repo 全体** (2026-09-10 の敵対レビュー P1-2)
#
# `issues/` の中だけを走査していたら、`docs/nvim-ruby-lsp.md` のように**外から issue を指す
# 参照**が黙って切れた (実測: 332 を done へ送ったときに切れたリンクが現存していた)。
# しかも 2 本のリンク検査は `issues/` しか見ないので **rc=0 の ✓ のまま壊れる**。
# よって走査は repo 全体へ広げ、**「移動前のパスを指す参照が repo に 0 件」**を移動後の
# 事後条件として自前で確かめる (検査側の射程は issue 293 の判断どおり issues/ に閉じたまま)。
#
# ## 変異検証で red を確認した箇所 (2026-09-10)
#
#   手順 2 を外す → test_next_links_valid.sh が red / 手順 3 を外す・手順 4 を外す →
#   test_issue_links_valid.sh が red / 手順 4 の走査を issues/ に狭める → 事後条件が red。
#   詳細は tests/issues/test_issue_done.sh。
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
# shellcheck disable=SC2016  # awk のプログラム本文。$1 等はシェルに展開させない
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
# report モード: そのリンクが want (repo 相対) を指しているか。書き換えと同じ解決経路を通す
function resolves_to(lnk, w,   path, t) {
  path = (index(lnk, "#") > 0) ? substr(lnk, 1, index(lnk, "#") - 1) : lnk
  if (path == "") return 0
  t = normalize(old_base "/" path)
  return (t == w)
}
function process(seg,   out, rest, pre, whole, lnk, nl) {
  out = ""; rest = seg
  while (match(rest, /\]\((\.\.\/)*[A-Za-z0-9_.\/-]+\.[A-Za-z0-9]+(#[A-Za-z0-9_-]+)?\)/)) {
    pre   = substr(rest, 1, RSTART - 1)
    whole = substr(rest, RSTART, RLENGTH)
    rest  = substr(rest, RSTART + RLENGTH)
    lnk   = substr(whole, 3, length(whole) - 3)
    if (report) { if (resolves_to(lnk, want)) print "HIT " lnk; out = out pre whole; continue }
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
# report モードは走査だけで、本文は出さない (HIT 行だけを返す)
function emit(l) { if (!report) print l }
# コードフェンスの中も同じ理由で触らない (判定は test_issue_links_valid.sh の extract_links と同形)
/^[[:space:]]*```[^`]*$/ { fence = !fence; emit($0); next }
/^[[:space:]]*~~~/       { fence = !fence; emit($0); next }
fence { emit($0); next }
{ emit(rewrite_line($0)) }
END { if (!report && changed > 0) print "REWROTE " changed > "/dev/stderr" }
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
  # shellcheck disable=SC2016  # canary の入力。バッククォート内の `](path.md)` を
  #   リテラルで渡すのが主眼 (展開させると検査対象の形が変わる)
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
    '~~~' \
    'チルダフェンス内: [t](../_claude/rules/x.md)' \
    '~~~' \
    | awk -v old_base="issues" -v new_base="issues/done" \
          -v moved_old="issues/345-retro.md" -v moved_new="issues/done/345-retro.md" \
          "$AWK_REBASE" 2>/dev/null)"
  # shellcheck disable=SC2016  # fixture の期待値。リテラルとして比較する
  want='上向き: [rule](../../_claude/rules/x.md)
下向き: [old](312-x.md)
当人へ: [self](345-retro.md)
アンカー: [a](../../docs/y.md#sec)
外部: [u](https://example.com/z.md)
インライン: `](path.md)` は書式の説明
```sh
フェンス内: [f](../_claude/rules/x.md)
```
~~~
チルダフェンス内: [t](../_claude/rules/x.md)
~~~'
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
  # 動かした issue と無関係なリンクは、冗長な書き方 (`./` 入り) でも触らない。
  # 触ると移動と無関係な差分が commit に混ざる (敵対レビュー P2-2)
  got="$(printf '%s\n' '無関係: [z](./done/311-b.md)' \
    | awk -v old_base="issues" -v new_base="issues" \
          -v moved_old="issues/347-x.md" -v moved_new="issues/done/347-x.md" \
          "$AWK_REBASE" 2>/dev/null)"
  if [ "$got" != '無関係: [z](./done/311-b.md)' ]; then
    printf '✗ canary: 無関係なリンクを書き換えている: %s\n' "$got" >&2
    return 1
  fi
  # report モード (事後条件のオラクル) も同じ awk で固定する
  got="$(printf '%s\n' '[a](347-x.md) [b](done/311-b.md)' \
    | awk -v old_base="issues" -v new_base="issues" -v report=1 -v want="issues/347-x.md" \
          "$AWK_REBASE" 2>/dev/null)"
  if [ "$got" != 'HIT 347-x.md' ]; then
    printf '✗ canary: report モードが想定と違う: %s\n' "$got" >&2
    return 1
  fi
  return 0
}

# 追跡されているか (追跡外へ git mv / git rm を打つと rc=128 で落ちる)
tracked() { git "${GIT_DIR_ARG[@]}" ls-files --error-unmatch -- "$1" >/dev/null 2>&1; }

# repo 相対のディレクトリ名 (awk の old_base / new_base に渡す形)
relbase() { local d; d="$(cd "$(dirname "$1")" && pwd -P)"; printf '%s' "${d#"$BASE_DIR/"}"; }

# 手順 4 / 事後条件の母集合: repo 全体から「そのファイル名を含むテキストファイル」
scan_candidates() {
  grep -rlF --binary-files=without-match --exclude-dir=.git -- "$1" "$BASE_DIR" 2>/dev/null | sort || true
}

# 事後条件のオラクル: want (repo 相対) を指す参照がまだ残っていれば列挙する。
# 判定は本走査と同じ awk (report モード) を通す — 式をコピーすると本走査の破損を検出しない
stale_ref_report() {
  local needle="$1" want="$2" f hit
  while IFS= read -r f; do
    [ -n "$f" ] || continue
    while IFS= read -r hit; do
      [ -n "$hit" ] || continue
      printf '%s: %s\n' "${f#"$BASE_DIR/"}" "${hit#HIT }"
    done < <(awk -v old_base="$(relbase "$f")" -v new_base="$(relbase "$f")" \
                 -v report=1 -v want="$want" "$AWK_REBASE" "$f" 2>/dev/null)
  done < <(scan_candidates "$needle")
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
# 🚨 --help は番号検証より先に見る。後ろに置くと `issue_done.sh 347 --help` が
# 「347 を処理せず rc=0」= 成功に見える無操作になる (敵対レビュー P3-3)
for n in "$@"; do
  case "$n" in -h|--help) usage; exit 0 ;; esac
done
for n in "$@"; do
  case "$n" in
    [0-9][0-9][0-9]) ;;
    *) printf '✗ issue 番号は 3 桁で指定する: %s\n' "$n" >&2; exit 2 ;;
  esac
done
[ -d "$ISSUES_DIR" ] || { printf '✗ 対象ディレクトリが無い: %s\n' "$ISSUES_DIR" >&2; exit 1; }

canary || exit 1

TESTLOG="$(mktemp -d "${TMPDIR:-/tmp}/issue_done_log.XXXXXX")"

# in_flight は「移動してから判定を通るまで」の間だけ 1。この窓で異常終了 (git の rc=128 /
# Ctrl+C / kill) すると、rollback を通らずに**半端な状態 + バックアップ消滅**で終わっていた
# (敵対レビュー P1-1 / P1-3。どちらも実測で再現)。判定の else 節だけでなく trap からも
# 同じ rollback を呼ぶ。
in_flight=0
undo_mv=0
undo_claim=0
claim_tracked=0

# 🚨 逆操作は `&&` で繋がない。`set -e` の下では AND リストの最後の要素にだけ errexit が
# 効くので、途中の 1 つ (例: 既に在る symlink への ln) が落ちると**残りの戻しが走らない**。
# 実測 2026-09-10: 変異 [no-unclaim] で「claim は消えていないのに戻そうとして ln が失敗 →
# git mv の戻しに到達せず、done/ に置き去り」が出た。各手順を独立した if で回し、
# 失敗しても次へ進んで最後に報告する。
rollback() {
  local rb_fail=0 bak_idx other
  while IFS=$'\t' read -r bak_idx other; do
    [ -n "$other" ] || continue
    cp -- "$TESTLOG/bak.$bak_idx" "$other" || rb_fail=1
  done < "$TESTLOG/touched.txt"
  [ ! -f "$TESTLOG/moved.bak" ] || cp -- "$TESTLOG/moved.bak" "$dest" || rb_fail=1
  if [ "$undo_claim" -eq 1 ] && [ ! -e "$claim" ]; then
    mkdir -p "$(dirname "$claim")" || rb_fail=1   # git rm は空になった next/ ごと消す
    if ln -s "../$base" "$claim"; then
      [ "$claim_tracked" -eq 0 ] || git "${GIT_DIR_ARG[@]}" add -- "$claim" || rb_fail=1
    else
      rb_fail=1
    fi
  fi
  if [ "$undo_mv" -eq 1 ] && [ -e "$dest" ]; then
    if tracked "$dest"; then git "${GIT_DIR_ARG[@]}" mv -f -- "$dest" "$src" || rb_fail=1
    else mv -f -- "$dest" "$src" || rb_fail=1; fi
  fi
  in_flight=0
  if [ "$rb_fail" -ne 0 ]; then
    printf '🚨 rollback が完全には戻せなかった。git status を見て手で直すこと\n' >&2
    return 1
  fi
  return 0
}

on_exit() {
  local rc=$?
  if [ "$in_flight" -eq 1 ]; then
    printf '\n🚨 判定の前に中断された (rc=%s)。移動を戻す (issue %s)\n' "$rc" "${num:-?}" >&2
    rollback || true
  fi
  rm -rf "$TESTLOG"
}
trap on_exit EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

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
  # 🚨 `grep -v '/done/'` を絶対パスに当てない。**repo の置き場所**に /done/ が含まれていると
  # 実体を全部落として「見つからない」になる (敵対レビュー P3-1)。判定は ISSUES_DIR からの相対で行う
  matches="$(find "$ISSUES_DIR" -type f -name "$num-*.md" -print |
    while IFS= read -r m; do
      rel="${m#"$ISSUES_DIR"/}"
      case "/$rel" in */done/*) continue ;; esac
      printf '%s\n' "$m"
    done | sort || true)"
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
  # 🚨 `epic/*)` は `epic/900/blocked` にも一致する。そのまま group_root にすると
  # `epic/900/blocked/done/` という**契約に無い 1 段深い done** へ黙って入る (敵対レビュー P2-3)。
  # epic は固定 2 段 (issues/README.md) なので、段数まで見て弾く
  case "$rel_srcdir" in
    '' | pending | waiting | next) group_root="$ISSUES_DIR" ;;
    epic/*/pending | epic/*/waiting | epic/*/next)
      group_root="$ISSUES_DIR/$(printf '%s' "$rel_srcdir" | cut -d/ -f1,2)" ;;
    epic/*/*) printf '✗ issue %s: epic は固定 2 段。予約外の深い置き場は手で直すこと: %s\n' "$num" "$srcdir" >&2; exit 1 ;;
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
  # ここから判定を通るまでが「半端な状態になりうる窓」。trap もこのフラグを見る
  : > "$TESTLOG/touched.txt"; rm -f "$TESTLOG/moved.bak"
  undo_mv=0; undo_claim=0; claim_tracked=0
  in_flight=1
  mkdir -p "$destdir"
  if tracked "$src"; then git "${GIT_DIR_ARG[@]}" mv -- "$src" "$dest"; else mv -- "$src" "$dest"; fi
  undo_mv=1

  # --- 手順 2: claim symlink の削除 ---------------------------------------------------------
  # 🚨 claim は **追跡外であるのが通常**。glogx の issues viewer の `n` (src/glogx/issues/move.go
  # の placeNextLink) は os.Symlink するだけで git add しない。追跡外へ `git rm` を打つと
  # rc=128 で落ち、set -e でそのまま死んで **移動済みのまま dangling claim が残る**
  # (敵対レビュー P1-1 / 実測で再現)。追跡の有無で分ける
  claim="$group_root/next/$base"
  undo_claim=0
  if [ -L "$claim" ]; then
    claim_tracked=0
    if tracked "$claim"; then git "${GIT_DIR_ARG[@]}" rm -q --force -- "$claim"; claim_tracked=1
    else rm -f -- "$claim"; fi
    undo_claim=1
  fi

  # --- 手順 3: 移した本文のリンクを新しい位置から張り直す -----------------------------------
  cp -- "$dest" "$TESTLOG/moved.bak"
  rebase_file "$dest" "$old_base" "$new_base" "$moved_old" "$moved_new" || true

  # --- 手順 4: 他の md からの「この issue への参照」を張り直す --------------------------------
  # 🚨 走査は **repo 全体**。issues/ に閉じると、外から issue を指す参照 (docs/ など) が
  # 黙って切れる (敵対レビュー P1-2。実測: docs/nvim-ruby-lsp.md が 332 の移動で切れていた)。
  # 母集合は「ファイル名を含むファイル」に絞る (awk は `](…)` の形しか書き換えないので、
  # コメント等に名前が出るだけのファイルを通しても無害)
  idx=0
  while IFS= read -r other; do
    [ -n "$other" ] || continue
    [ "$other" != "$dest" ] || continue
    other_base="$(relbase "$other")"
    idx=$((idx + 1))
    cp -- "$other" "$TESTLOG/bak.$idx"
    if rebase_file "$other" "$other_base" "$other_base" "$moved_old" "$moved_new"; then
      printf '%s\t%s\n' "$idx" "$other" >> "$TESTLOG/touched.txt"
    else
      rm -f -- "$TESTLOG/bak.$idx"
    fi
  done < <(scan_candidates "$base")

  # --- 検査。落ちたら逆順に戻す -------------------------------------------------------------
  # 事後条件を 2 つ持つ:
  #   ① 2 本のリンク検査 (issues/ の中。合格判定の正本)
  #   ② **移動前のパスを指す参照が repo に 1 件も残っていない** — 検査は issues/ しか見ないので、
  #      手順 4 を repo 全体へ広げたぶんのオラクルは自前で持つ (敵対レビュー P1-2)
  verdict=0
  run_link_tests || verdict=1
  stale_ref_report "$base" "$moved_old" > "$TESTLOG/stale.txt" || true
  [ ! -s "$TESTLOG/stale.txt" ] || verdict=1

  if [ "$verdict" -eq 0 ]; then
    in_flight=0
    touched="$(grep -c . "$TESTLOG/touched.txt" || true)"
    printf '✓ issue %s -> %s' "$num" "${dest#"$BASE_DIR/"}"
    [ "$undo_claim" -eq 1 ] && printf ' / claim symlink を削除'
    [ "$touched" -gt 0 ] && printf ' / 参照を張り直したファイル %s 本' "$touched"
    printf '\n'
  else
    printf '✗ 事後条件が満たされないので移動を戻す (issue %s)\n' "$num" >&2
    cat "$TESTLOG/links.txt" "$TESTLOG/next.txt" >&2
    [ ! -s "$TESTLOG/stale.txt" ] ||
      { printf '✗ 移動前のパスを指したままの参照:\n' >&2; cat "$TESTLOG/stale.txt" >&2; }
    rollback
    exit 1
  fi
done
