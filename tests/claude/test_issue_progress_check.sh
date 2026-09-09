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
# 🚨 stderr を見る版。上の hook() は 2>/dev/null で捨てるので、**掃除の件数のような
# stderr 側の観測点は拾えない** (これで一度「掃除が動いていない」と誤診した)
hook_err() { # $1=hook $2=session
  # stdout は捨てて stderr だけを取る (順序に依存しない形で書く。SC2069)
  { printf '{"cwd":"%s","session_id":"%s"}' "$repo" "$2" | "$1" >/dev/null; } 2>&1 || true
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


# --- issue 339: 説明済みの指摘を再掲しない / 新規は単独で出る -----------------------------------
#
# 🚨 dedup が「指摘の集合」の cksum だったため、**commit を 1 つ積むだけで集合が変わり**、
# 説明済みの N 件が丸ごと再掲されていた (実測 2026-09-09: 同じリストが 3 回)。
# 108 は「後から新しく関わる番号」用。s6 の基準点より前に作っておく (基準点の時点で在って
# 未変更なら、subject に (108) を出した瞬間に**新しい 1 行**が増える)
printf '# 108 later\n' >"$repo/issues/108-feat-s.md"
git -C "$repo" add issues/108-feat-s.md && git -C "$repo" commit -qm "chore: 108 を起票"
hook "$START" s6 >/dev/null
echo x1 >"$repo/src/a.txt"; git -C "$repo" commit -qam "fix(101): 1 回目"
first=$(reason s6)
check "1 回目は 101 を指摘する" "101-feat-x.md: このセッションで 1 度も変更されていない" "$first"

# commit を積んで集合を変える (旧実装はここで丸ごと再掲した)
echo x2 >"$repo/src/a.txt"; git -C "$repo" commit -qam "fix(101): 2 回目"
check "同じ指摘は再掲しない" "" "$(reason s6)"

# 新しい番号に関わったら、その行**だけ**が出る (既知の行に埋もれない)。
# 103 は init から在って未変更なので、subject に (103) を出せば新しい指摘が 1 行増える。
echo x3 >"$repo/src/a.txt"; git -C "$repo" commit -qam "fix(108): 新しい番号"
got=$(reason s6)
check "新しい 108 の行が出る" "issues/108-feat-s.md: このセッションで 1 度も変更されていない" "$got"
check "既知の 101 の行は混ざらない" "" "$(grep -E '101-feat-x' <<<"$got" || true)"

# --- issue 339: 規約が要求する「NNN で解消」の 1 行を進捗として数える --------------------------
#
# 🚨 CLAUDE.md「Issue管理」は done へ移す commit で参照元の open issue にこの 1 行を要求するのに、
# 判定が見出しと [x] しか見ていなかったため、**規約どおり書いても未対応と判定**されていた。
hook "$START" s7 >/dev/null
printf '# 105 ref\n\n残課題: 106 待ち\n' >"$repo/issues/105-bug-v.md"
printf '# 106 target\n' >"$repo/issues/106-bug-u.md"
git -C "$repo" add -A && git -C "$repo" commit -qm "chore: 105/106 を起票"
hook "$START" s8 >/dev/null
# 🚨 **実際に書かれた文面**で試す。助詞つきの `NNN で解消` だけに限ると、
# 「106 は ② … だけ解消」のような普通の書き方を拾えない (判定を足した当日に踏んだ)。
printf '# 105 ref\n\n残課題: 106 待ち\n\n2026-09-09: **106 は ② Go の方だけ解消**（① は継続）。\n' >"$repo/issues/105-bug-v.md"
git -C "$repo" commit -qam "docs(105): 106 の完了を書き戻す"
check "「NNN … 解消」を進捗として数える" "" "$(grep -E '105-bug-v.md: 変更はあるが' <<<"$(reason s8)" || true)"

# --- issue 339: worktree の commit を「触っていない」と誤検出しない ----------------------------
#
# 🚨 この repo は worktree を既定にしているので、本体へ push+pull するまで root からは
# 「何も変わっていない」ように見え、片付けた issue が毎ターン再掲されていた。
printf '# 107 wt\n\n- [ ] a\n' >"$repo/issues/107-feat-t.md"
git -C "$repo" add issues/107-feat-t.md && git -C "$repo" commit -qm "chore: 107 を起票"
wt="$WORK/wt"
git -C "$repo" worktree add -q --detach "$wt" HEAD
hook "$START" s9 >/dev/null
printf '# 107 wt\n\n- [x] a\n\n## 進捗\n\nやった\n' >"$wt/issues/107-feat-t.md"
git -C "$wt" commit -qam "fix(107): worktree で進捗を書く"
# 🚨 「触っていない」も「進捗が増えていない」も出ないこと (worktree の commit を数えている)
check "worktree の変更を「触っていない」と言わない" "" \
  "$(grep -E '107-feat-t.md' <<<"$(reason s9)" || true)"
git -C "$repo" worktree remove --force "$wt"

# --- issue 302 ①: 期限切れの状態ファイルを掃除する ---------------------------------------------
#
# 🚨 判定は「**掃除が実際に消した件数**」で見る。0 件を成功にしない
# (verify-execution-not-just-exit-code.md)。実測 2026-09-09: 導入 3 日で 174 ファイル溜まっていた。
sweep_dir="$WORK/sweep"; mkdir -p "$sweep_dir"
old_head="$sweep_dir/aaaa.head"; old_rep="$sweep_dir/bbbb.reported"
new_head="$sweep_dir/cccc.head"; other="$sweep_dir/keep.txt"; sub="$sweep_dir/sub"
: > "$old_head"; : > "$old_rep"; : > "$new_head"; : > "$other"; mkdir -p "$sub"; : > "$sub/deep.head"
# 20 日前にする (find -mtime +14 が拾う)
touch -t "$(date -v-20d +%Y%m%d%H%M 2>/dev/null || date -d '20 days ago' +%Y%m%d%H%M)" \
  "$old_head" "$old_rep" "$other" "$sub/deep.head"
sweep_out=$(CLAUDE_ISSUE_PROGRESS_DIR="$sweep_dir" hook_err "$START" s10)

check "掃除した件数を報告する" "状態ファイルを 2 件掃除した" "$sweep_out"
[ -f "$old_head" ] && { echo "NG: 古い .head が残っている"; fails=$((fails+1)); }
[ -f "$old_rep" ]  && { echo "NG: 古い .reported が残っている"; fails=$((fails+1)); }
# 🚨 **対象を拡張子で絞っていること**。state_dir は env で差し替えられる (テストが実際にやる) ので、
# 広い削除にすると差し替え先の中身を巻き込む
[ -f "$other" ] || { echo "NG: 対象外の keep.txt を消した (削除が拡張子で絞られていない)"; fails=$((fails+1)); }
[ -f "$sub/deep.head" ] || { echo "NG: サブディレクトリまで潜って消した (-maxdepth 1 が効いていない)"; fails=$((fails+1)); }
[ -f "$new_head" ] || { echo "NG: 新しい .head を消した"; fails=$((fails+1)); }

# 掃除するものが無ければ黙る (毎回ノイズを出さない)
check "掃除 0 件なら黙る" "" "$(CLAUDE_ISSUE_PROGRESS_DIR="$WORK/sweep2" hook_err "$START" s11)"

# --- issue 302 ②: session_id をパス構成要素として検証する ---------------------------------------
#
# 🚨 jq が無い環境の sed 経路は `"` `,` `}` しか落とさず **`/` と `..` を通す**。
# 書き先が `$state_dir/$session_id.head` なので、通すと状態ディレクトリの外へ書ける。
esc_dir="$WORK/esc"; mkdir -p "$esc_dir"
for bad in "../escaped" "a/b" "" "x;y"; do
  CLAUDE_ISSUE_PROGRESS_DIR="$esc_dir" hook "$START" "$bad" >/dev/null 2>&1 || true
done
if [ -e "$WORK/escaped.head" ] || [ -n "$(find "$WORK" -maxdepth 1 -name '*.head' 2>/dev/null)" ]; then
  echo "NG: 不正な session_id で状態ディレクトリの外へ書いた"; fails=$((fails+1))
fi
if [ -n "$(find "$esc_dir" -type f 2>/dev/null)" ]; then
  echo "NG: 不正な session_id を通した ($(find "$esc_dir" -type f | tr '\n' ' '))"; fails=$((fails+1))
fi
# 正常な id は通ること (弾きすぎていないこと)
CLAUDE_ISSUE_PROGRESS_DIR="$esc_dir" hook "$START" "7f7c4ee0-5da8-4891-b2fa-e4ecbd1886fa" >/dev/null 2>&1
[ -f "$esc_dir/7f7c4ee0-5da8-4891-b2fa-e4ecbd1886fa.head" ] || {
  echo "NG: 正常な session_id (UUID) を弾いた"; fails=$((fails+1)); }

if [ "$fails" -eq 0 ]; then echo "OK: issue-progress hooks"; else echo "FAIL: $fails"; exit 1; fi
