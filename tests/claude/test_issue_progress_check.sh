#!/usr/bin/env bash
# _claude/hooks/issue-progress-start.sh (SessionStart で基準 HEAD を記録) と
# _claude/hooks/issue-progress-check.sh (Stop で関わった issue の更新漏れを block で差し戻す) の unit テスト。
#
# なぜ: この hook は「実装後に issue を更新し忘れる」を出口で止める安全機構で、壊れると黙るだけ
# (= 漏れが戻る)。判定の 2 段 (変更の有無 / チェックボックスと見出しの増減)、関連 issue の列挙、
# stop_hook_active と 1 セッション 1 回の抑制、対象外 repo の沈黙を回帰として固定する。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
START="$ROOT_DIR/_claude/hooks/issue-progress-start.sh"
CHECK="$ROOT_DIR/_claude/hooks/issue-progress-check.sh"
fails=0
if ! command -v jq >/dev/null 2>&1; then echo "SKIP: jq が無い環境"; exit 77; fi

WORK="$(mktemp -d "${TMPDIR:-/tmp}/issue-progress.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT
export CLAUDE_ISSUE_PROGRESS_DIR="$WORK/state"
repo="$WORK/repo"; mkdir -p "$repo/issues/done" "$repo/issues/next" "$repo/src"
git -C "$repo" init -q . && git -C "$repo" config user.email t@t && git -C "$repo" config user.name t
printf '# 101 feat\n\n- [ ] a\n- [ ] b\n' >"$repo/issues/101-feat-x.md"
printf '# 102 other\n\n残課題: 101 が終わったら見直す\n' >"$repo/issues/102-bug-y.md"
printf '# 103 unrelated\n' >"$repo/issues/103-docs-z.md"
echo base >"$repo/src/a.txt"
git -C "$repo" add -A && git -C "$repo" commit -qm "init"

hook() { # $1=hook $2=session $3=extra json fields (空可)
  printf '{"cwd":"%s","session_id":"%s"%s}' "$repo" "$2" "${3:-}" | "$1" 2>/dev/null || echo "__ERROR__"
}
reason() { local out; out=$(hook "$CHECK" "$1" "${2:-}"); [ -n "$out" ] || return 0; printf '%s' "$out" | jq -r 'if .decision=="block" then .reason else "__NOBLOCK__" end'; }
check() { local desc="$1" want="$2" got="$3"
  if [ -z "$want" ]; then [ -z "$got" ] && return 0; echo "NG: $desc — 無出力のはず:"; printf '%s\n' "$got"; fails=$((fails+1)); return; fi
  grep -Eq "$want" <<<"$got" && return 0
  echo "NG: $desc — /$want/ が無い:"; printf '%s\n' "${got:-(無出力)}"; fails=$((fails+1)); }

# 1. 基準点なし (start hook 未実行) → 黙る
check "基準点なしで黙る" "" "$(reason s0)"

# 2. start が HEAD を記録する (s2 は 5 で stop_hook_active を見るための同じ基準点)
hook "$START" s1 >/dev/null
hook "$START" s2 >/dev/null
check "start が root と HEAD を記録" "$(git -C "$repo" rev-parse HEAD)" "$(cat "$CLAUDE_ISSUE_PROGRESS_DIR/s1.head")"

# 3. commit 前 (関わった番号なし) → 黙る
check "番号が出ていなければ黙る" "" "$(reason s1)"

# 4. subject に (101) を持つ commit で issue 101 未変更 → block、関連 102 も列挙、103 は出ない
echo change >"$repo/src/a.txt"; git -C "$repo" commit -qam "fix(101): do x"
got=$(reason s1)
check "101 未変更を指摘" "issues/101-feat-x.md: このセッションで 1 度も変更されていない" "$got"
check "101 を参照する open 102 を列挙" "issues/102-bug-y.md: issue 101 を参照" "$got"
check "無関係な 103 は出ない" "" "$(grep -E '103-docs' <<<"$got" || true)"

# 5. 同じ指摘は 1 セッション 1 回 / stop_hook_active では黙る
check "同じ指摘は再送しない" "" "$(reason s1)"
check "stop_hook_active で黙る" "" "$(reason s2 ',"stop_hook_active":true')"
check "同じ状態でも stop_hook_active が無ければ block する (5 の対照)" "101-feat-x.md" "$(reason s2)"

# 6. 触ったが [x] も見出しも増えていない (typo 修正だけ) → 構造の指摘
sed -i '' 's/# 101 feat/# 101 feat!/' "$repo/issues/101-feat-x.md"
got=$(reason s1)
check "変更はあるが進捗が無い" "101-feat-x.md: 変更はあるが、完了チェック" "$got"

# 7. [x] が増え、102 に 1 行追記 → 黙る (未 commit の作業ツリーでも拾う)
sed -i '' 's/- \[ \] a/- [x] a/' "$repo/issues/101-feat-x.md"
printf '\n101 で解消\n' >>"$repo/issues/102-bug-y.md"
check "進捗と関連追記があれば黙る" "" "$(reason s1)"

# 8. done へ移した issue: subject の (101) で対象になり、結果見出しの追加で構造を満たす。
#    関連 102 は前セッションで追記済み (基準点より前) なので、この session では未変更 → 列挙される
git -C "$repo" add -A && git -C "$repo" commit -qm "docs: progress"
hook "$START" s3 >/dev/null
git -C "$repo" mv issues/101-feat-x.md issues/done/101-feat-x.md
printf '\n## 結果\n\n済\n' >>"$repo/issues/done/101-feat-x.md"
git -C "$repo" commit -qam "docs(101): done"
got=$(reason s3)
check "done 移動 + 結果見出しなら 101 自身は指摘しない" "" "$(grep -E '101-feat-x' <<<"$got" || true)"
check "関連 102 が未変更なら列挙" "102-bug-y.md: issue 101 を参照" "$got"
# 8b. issue ファイルを触っただけ (path 由来) の番号は作業対象に格上げしない
hook "$START" s3b >/dev/null
printf '\nmemo\n' >>"$repo/issues/102-bug-y.md"; git -C "$repo" commit -qam "docs: memo"
check "path 由来の番号だけなら黙る" "" "$(reason s3b)"

# 9. next/ の claim (symlink) も「関わった」に数える
hook "$START" s4 >/dev/null
ln -s ../103-docs-z.md "$repo/issues/next/103-docs-z.md"
check "claim 中の 103 が未変更なら指摘" "issues/103-docs-z.md: このセッションで 1 度も変更されていない" "$(reason s4)"

# 10. issues/ の無い repo では黙る
other="$WORK/plain"; mkdir -p "$other"; git -C "$other" init -q .
check "issues 無し repo で黙る" "" "$(printf '{"cwd":"%s","session_id":"s9"}' "$other" | "$CHECK" 2>/dev/null || true)"

[ "$fails" -eq 0 ] && echo "OK: issue-progress hooks" || { echo "FAIL: $fails"; exit 1; }
