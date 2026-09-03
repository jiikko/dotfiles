#!/usr/bin/env bash
# _claude/hooks/human-tasks-due.sh (SessionStart で人間タスクの未完了・期限を注入する hook) の
# unit テスト。合成した hook JSON を stdin で流し、報告内容を pin する。
#
# なぜ: このフックは「人間しかできない作業が忘れられる」を止めるための検査で、壊れても
# 静かに黙るだけなので気づけない (= 期限切れが誰にも見えない状態に戻る)。実測で見つかった
# 欠陥を回帰として固定する: カテゴリの部分一致誤検出 / 依存コマンド失敗を「期限なし」と誤報 /
# pending の取りこぼし / 「検査できなかった」の沈黙。
# 規範: issues/README.md「`期限:`」、~/.claude/CLAUDE.md「Issue管理」
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/human-tasks-due.sh"
fails=0

if ! command -v jq >/dev/null 2>&1; then
  echo "SKIP: jq が無い環境 (hook 自体は素の stdout へフォールバックする)"
  exit 77
fi

WORK="$(mktemp -d "${TMPDIR:-/tmp}/human-tasks-due.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

repo="$WORK/repo"
mkdir -p "$repo/issues/pending" "$repo/issues/done"
git -C "$repo" init -q .

mkissue() { # $1=相対パス $2=メタ行 (空可)
  printf '# t\n\n起票日: 2026-08-01\n%s\n' "$2" >"$repo/issues/$1"
}

# 出力本文 (additionalContext) を取り出す。hook が黙ったときは空文字。
report() { # 追加の env は呼び出し側で env ... を前置する
  local out
  out="$(printf '{"cwd":"%s"}' "$repo" | "$@" "$HOOK" 2>/dev/null)" || { echo "__ERROR__"; return; }
  [ -n "$out" ] || return 0
  printf '%s' "$out" | jq -r '.hookSpecificOutput.additionalContext // "__NOJSON__"'
}

check() { # $1=説明 $2=期待パターン (grep -E) $3=本文。空パターンは「無出力」を期待
  local desc="$1" want="$2" got="$3"
  if [ -z "$want" ]; then
    [ -z "$got" ] && return 0
    echo "NG: $desc — 何も出さないはずが出力された:"; printf '%s\n' "$got"; fails=$((fails + 1)); return
  fi
  # 🚨 `printf … | grep -q` のパイプに戻さないこと。grep -q は一致した瞬間に exit するため
  # 書き手が SIGPIPE/EPIPE を受け、`set -o pipefail` 下では**一致していてもパイプライン全体が
  # 非 0** になる。判定が反転し、正しい実装に対してランダムに NG を出す (CI 実測 2026-08-22:
  # run 32570242557。出力に期待パターンが載っているのに NG + "printf: write error: Broken pipe")。
  grep -Eq "$want" <<<"$got" && return 0
  echo "NG: $desc — /$want/ が出力に無い:"; printf '%s\n' "${got:-(無出力)}"; fails=$((fails + 1))
}

# --- 1. 対象なし: human も期限も無ければ黙る (毎セッションのノイズにしない) ---
mkissue "010-docs-something.md" ""
check "対象なしで黙る" "" "$(report env)"

# --- 2. カテゴリはファイル名 position 2 で判定する (部分一致で誤検出しない) ---
# 実測 2026-08-20: `*-human-*` だとスラッグ側に同語を含む別カテゴリを拾っていた
mkissue "011-docs-mutation-human-review-notes.md" ""
check "スラッグ側の human を誤検出しない" "" "$(report env)"

# --- 3. 期限切れ / 期限間近 / 期限なし / 書式不正 を分類する ---
mkissue "012-human-old.md" "期限: 2026-08-01"
mkissue "013-human-nodate.md" ""
mkissue "014-human-broken.md" "期限: 8/25"
got="$(report env)"
check "期限切れを出す" '期限切れ 2026-08-01.*012-human-old' "$got"
check "期限なしを出す (human は期限必須)" '期限なし.*013-human-nodate' "$got"
check "書式不正を出す (黙って捨てない)" '書式不正.*014-human-broken' "$got"
check "未完了件数を数える" '未完了の human タスク issue: 3 件' "$got"

# --- 4. done/ は拾わない。pending/ は拾うが件数には数えない ---
mkissue "015-human-finished.md" "期限: 2026-08-02"
mv "$repo/issues/015-human-finished.md" "$repo/issues/done/"
printf '# t\n\n期限: 2026-08-01\n' >"$repo/issues/pending/016-human-blocked.md"
got="$(report env)"
check "done/ は対象外" "" "$(printf '%s' "$got" | grep -E '015-human-finished' || true)"
check "pending/ の期限切れは出す" '期限切れ 2026-08-01.*016-human-blocked.*\[保留\]' "$got"
check "pending は未完了件数に入れない" '未完了の human タスク issue: 3 件' "$got"

# --- 5. 依存コマンドが壊れたら「期限なし」と誤報せず「抽出失敗」と言う ---
mkdir -p "$WORK/badgrep"
printf '#!/bin/sh\nexit 2\n' >"$WORK/badgrep/grep"
chmod +x "$WORK/badgrep/grep"
check "grep 失敗を抽出失敗として出す" '抽出失敗' "$(report env PATH="$WORK/badgrep:$PATH")"

# --- 6. date が +3 日を計算できないときは「判定を省略した」と明記する ---
mkdir -p "$WORK/baddate"
cat >"$WORK/baddate/date" <<'EOF'
#!/bin/sh
case "$1" in -v* | -d) exit 1 ;; esac
exec /bin/date "$@"
EOF
chmod +x "$WORK/baddate/date"
check "date 非対応を明記する" '「期限間近」の判定は省略' "$(report env PATH="$WORK/baddate:$PATH")"

# --- 6b. 「(うち期限に余裕あり N 件)」は unread と同じ母集団 (human かつ pending 以外) ---
# 回帰 2026-08-21: later はカテゴリも pending も問わず加算していたため、規約準拠のデータだけで
# 「未完了 1 件 (うち期限に余裕あり 2 件)」= 部分集合でない表示が出た。
# 🚨 上のケース群が積んだ issue と混ざらないよう、この検査だけ独立の repo で行う。
pop="$WORK/pop"
mkdir -p "$pop/issues/pending"
git -C "$pop" init -q .
far=2099-01-01
near="$(date -v+1d +%F 2>/dev/null || date -d '+1 day' +%F)"
printf '# t\n\n期限: %s\n' "$near" >"$pop/issues/093-human-near.md"
printf '# t\n\n期限: %s\n' "$far" >"$pop/issues/094-docs-far.md"
printf '# t\n\n期限: %s\n' "$far" >"$pop/issues/pending/095-human-far.md"
pop_out="$(printf '{"cwd":"%s"}' "$pop" | "$HOOK" 2>/dev/null || true)"
pop_ctx="$(printf '%s' "$pop_out" | jq -r '.hookSpecificOutput.additionalContext // ""' 2>/dev/null || true)"
check "未完了件数は human かつ pending 以外だけ" '未完了の human タスク issue: 1 件' "$pop_ctx"
if grep -q '余裕あり' <<<"$pop_ctx"; then
  echo "NG: later が unread と別母集団 (非 human / pending を数えている):"
  printf '%s\n' "$pop_ctx"
  fails=$((fails + 1))
fi

# --- 7. git 管理外では何もしない ---
# 🚨 cwd に `/` を渡す形にしないこと。`/issues` が無いので**後段の「issues/ が無ければ諦める」
# ガードが黙らせているだけ**で、git ガードの有無を区別できない (実測 2026-08-21: git ガードを
# `root="$cwd"` に変えても、非 git で `exit 3` する実装に変えても緑だった = 観点 7 は空回り)。
# 差が出る形にする: **issues/ と human issue を持つが git 管理外**のディレクトリを作り、
# GIT_CEILING_DIRECTORIES で上位への遡上を止める (実 git がそこを非 git として扱う)。
nogit="$WORK/nogit"
mkdir -p "$nogit/issues"
printf '# t\n\n起票日: 2026-08-01\n期限: 2026-08-01\n' >"$nogit/issues/090-human-x.md"
# 🚨 rc も見ること。`|| true` で捨てると「黙って何もしない (正)」と「異常終了した (誤)」を
# 区別できない (実測 2026-08-21: 非 git で exit 3 する変異が緑のまま通った)。
out="$(printf '{"cwd":"%s"}' "$nogit" | GIT_CEILING_DIRECTORIES="$WORK" "$HOOK" 2>/dev/null)"
rc=$?
check "git 管理外で黙る" "" "$out"
if [ "$rc" -ne 0 ]; then
  echo "NG: git 管理外で異常終了した (rc=$rc)。無出力でも非 0 は hook 契約違反"
  fails=$((fails + 1))
fi
# 陽性対照: 同じディレクトリを git 管理下にすると (= git ガードだけが変わると) 報告が出る。
# これが無いと「issues/ の中身が悪くて黙った」と区別できない
git -C "$nogit" init -q .
out="$(printf '{"cwd":"%s"}' "$nogit" | GIT_CEILING_DIRECTORIES="$WORK" "$HOOK" 2>/dev/null || true)"
ctx="$(printf '%s' "$out" | jq -r '.hookSpecificOutput.additionalContext // ""' 2>/dev/null || true)"
check "陽性対照: git 管理下にすれば同じ issues/ を報告する (差が git ガードだけである証跡)" \
  '090-human-x' "$ctx"

if [ "$fails" -gt 0 ]; then
  echo "FAIL: human-tasks-due.sh のテストが $fails 件失敗"
  exit 1
fi
echo "OK: human-tasks-due.sh (7 観点)"
