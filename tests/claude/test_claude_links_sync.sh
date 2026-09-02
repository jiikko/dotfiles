#!/usr/bin/env bash
# scripts/claude_links.sh (期待するリンク集合の出典。check / apply) と、それを SessionStart で
# 呼ぶ _claude/hooks/claude-links-sync.sh の unit テスト。偽の HOME に dotfiles/_claude と .claude を
# 組み、実 ~/.claude には触れない。
#
# なぜ: この hook は「setup.sh を忘れて rule が読まれない」を自動修復する安全機構で、壊れ方は
# 2 方向ある。(a) 素通り = 欠けているのに黙る (修復されないまま誰も気づかない) と、(b) 過剰 =
# 揃っているのに毎回 apply する / 実ファイルや他ツールの link を上書きする / dir symlink 越しに
# repo 側へ書き込む (setup.sh の migrate コメントが警告する自己参照 symlink 破壊)。両方を pin する。
# 規範: issue 160 / ~/.claude/rules/adversarial-review-own-safeguards.md
# ケース番号は 1〜13 + 6b。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/claude_links.sh"
HOOK="$ROOT_DIR/_claude/hooks/claude-links-sync.sh"
fails=0

WORK="$(mktemp -d "${TMPDIR:-/tmp}/claude-links.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

ng() { echo "NG: $*"; fails=$((fails + 1)); }

# fixture: $1 = ケース名。偽 HOME を新規に作り、DOT (偽 dotfiles) と CH (偽 ~/.claude) を設定する。
# ケース間で状態を共有しない (前のケースの残骸が次に効くと A-B が同じ結果になり誤診する)。
fixture() {
  H="$WORK/$1"
  DOT="$H/dotfiles"
  CH="$H/.claude"
  mkdir -p "$DOT/_claude/rules" "$DOT/_claude/agents" "$DOT/_claude/commands" "$DOT/_claude/hooks/lib" \
    "$DOT/_claude/skills/sk1" "$DOT/_claude/workflows" "$DOT/scripts" "$CH"
  echo r >"$DOT/_claude/rules/r1.md"
  echo a >"$DOT/_claude/agents/a1.md"
  echo c >"$DOT/_claude/commands/c1.md"
  echo h >"$DOT/_claude/hooks/h1.sh"
  echo l >"$DOT/_claude/hooks/lib/l.sh"
  echo s >"$DOT/_claude/skills/sk1/SKILL.md"
  echo w >"$DOT/_claude/workflows/w1.js"
  echo doc >"$DOT/_claude/workflows/README.md"
  # hook は $DOTFILES_ROOT/scripts/claude_links.sh を呼ぶ。実物へ link して本物を検査する
  ln -s "$SCRIPT" "$DOT/scripts/claude_links.sh"
}

# run <check|apply|list>: 偽環境でスクリプトを実行し、stdout+stderr を OUT、rc を RC に入れる
run() {
  set +e
  OUT=$(DOTFILES_ROOT="$DOT" CLAUDE_HOME="$CH" "$SCRIPT" "$1" 2>&1)
  RC=$?
  set -e
}
# hook_run: hook を SessionStart 相当で実行。additionalContext を CTX に、rc を RC に入れる
hook_run() {
  set +e
  local raw
  raw=$(printf '{"cwd":"%s"}' "$DOT" | HOME="$H" DOTFILES_ROOT="$DOT" "$HOOK" 2>&1)
  RC=$?
  set -e
  if [ -z "$raw" ]; then
    CTX=""
  elif command -v jq >/dev/null 2>&1; then
    CTX=$(printf '%s' "$raw" | jq -r '.hookSpecificOutput.additionalContext // "__NOJSON__"')
  else
    CTX="$raw"
  fi
}

# --- 1. list: 期待集合 (agents/commands/rules/hooks の 4 + hooks/lib 1 + skill 1 + js 1 = 7。README.md は含めない) ---
fixture list
run list
[ "$RC" -eq 0 ] || ng "list rc=$RC"
n=$(printf '%s\n' "$OUT" | grep -c . || true)
[ "$n" -eq 7 ] || ng "list は 7 件のはず: $n 件"$'\n'"$OUT"
grep -q "workflows/README.md" <<<"$OUT" && ng "workflows は *.js だけ。README.md を含めた"
grep -q "skills/sk1	$DOT/_claude/skills/sk1\$" <<<"$OUT" || ng "skill は末尾スラッシュ無しの dir を指す"$'\n'"$OUT"

# --- 2. check は何も作らない / 欠けがあれば rc=1 で全件列挙 ---
fixture check_only
run check
[ "$RC" -eq 1 ] || ng "check: 欠けているのに rc=$RC"
n=$(grep -c '^missing: ' <<<"$OUT" || true)
[ "$n" -eq 7 ] || ng "check: missing は 7 件のはず: $n"$'\n'"$OUT"
[ -z "$(ls -A "$CH")" ] || ng "check が ~/.claude に何か作った: $(ls -A "$CH")"

# --- 3. apply: 欠けた分を張る → check が 0 に。link 先は -ef で一致 ---
run apply
[ "$RC" -eq 0 ] || ng "apply rc=$RC"$'\n'"$OUT"
grep -q '^linked 7$' <<<"$OUT" || ng "apply は 7 件張ったと報告するはず"$'\n'"$OUT"
[ -L "$CH/rules/r1.md" ] && [ "$CH/rules/r1.md" -ef "$DOT/_claude/rules/r1.md" ] || ng "rules/r1.md が張られていない"
[ -L "$CH/skills/sk1" ] && [ "$CH/skills/sk1" -ef "$DOT/_claude/skills/sk1" ] || ng "skills/sk1 が張られていない"
[ -L "$CH/hooks/lib" ] || ng "hooks/lib (dir) が張られていない"
[ -e "$CH/workflows/README.md" ] && ng "workflows/README.md を張った"
run check
[ "$RC" -eq 0 ] && [ -z "$OUT" ] || ng "apply 後の check が 0/無言でない: rc=$RC"$'\n'"$OUT"

# --- 4. 部分欠け: 1 件だけ足すと、その 1 件だけ張る (揃っている分に触らない) ---
before=$(stat -f %m "$CH/rules/r1.md")
echo r2 >"$DOT/_claude/rules/r2.md"
sleep 1
run apply
grep -q '^linked 1$' <<<"$OUT" || ng "部分欠けで linked 1 でない"$'\n'"$OUT"
grep -q "^linked: $CH/rules/r2.md" <<<"$OUT" || ng "r2.md を張ったと報告していない"
after=$(stat -f %m "$CH/rules/r1.md")
[ "$before" = "$after" ] || ng "揃っている r1.md を張り直した (mtime が変わった)"

# --- 5. 別物を指す repo 由来の link (改名後の stale) は張り替える ---
fixture stale
run apply
mv "$DOT/_claude/rules/r1.md" "$DOT/_claude/rules/r1-renamed.md"
ln -sfn "$DOT/_claude/rules/gone.md" "$CH/rules/r1-renamed.md"   # repo 配下を指すが dangling
run apply
[ "$RC" -eq 0 ] || ng "stale 張り替え rc=$RC"$'\n'"$OUT"
[ "$CH/rules/r1-renamed.md" -ef "$DOT/_claude/rules/r1-renamed.md" ] || ng "stale link を張り替えていない"
[ -L "$CH/rules/r1.md" ] || ng "旧 link r1.md は消さない (掃除は setup.sh の責務) はずが無い"

# --- 6. 上書き拒否: 実ファイル / _claude 以外を指す symlink。rc=2、中身は無傷、他は張る ---
fixture refuse
mkdir -p "$CH/rules"; echo mine >"$CH/rules/r1.md"   # ユーザーの実ファイル
mkdir -p "$CH/agents" "$H/other"; echo other >"$H/other/a1.md"
ln -s "$H/other/a1.md" "$CH/agents/a1.md"   # 他ツールの link
run apply
[ "$RC" -eq 2 ] || ng "refuse: rc=$RC (2 のはず)"$'\n'"$OUT"
[ "$(cat "$CH/rules/r1.md")" = mine ] || ng "実ファイル rules/r1.md を上書きした"
[ "$(readlink "$CH/agents/a1.md")" = "$H/other/a1.md" ] || ng "他ツールの link agents/a1.md を上書きした"
grep -q "refused: $CH/rules/r1.md" <<<"$OUT" || ng "実ファイル拒否を報告していない"$'\n'"$OUT"
grep -q "refused: $CH/agents/a1.md" <<<"$OUT" || ng "他ツール link 拒否を報告していない"$'\n'"$OUT"
[ "$CH/commands/c1.md" -ef "$DOT/_claude/commands/c1.md" ] || ng "拒否した以外の分 (commands/c1.md) を張っていない"
grep -q '^linked 5$' <<<"$OUT" || ng "拒否 2 件を除く 5 件を張ったはず"$'\n'"$OUT"

# --- 6b. link 先が symlink でない実ディレクトリ (skills/sk1) も拒否する。⚠️ ln -sfn は dest が実ディレクトリ
#         だと -n が効かず dest/<basename> として中に潜り込む (BSD ln 実測)。ガードはこの 1 行だけ ---
fixture refuse_dir
mkdir -p "$CH/skills/sk1"
run apply
[ "$RC" -eq 2 ] || ng "refuse_dir: rc=$RC (2 のはず)"$'\n'"$OUT"
grep -q "refused: $CH/skills/sk1 " <<<"$OUT" || ng "実ディレクトリ skills/sk1 の拒否を報告していない"$'\n'"$OUT"
[ -e "$CH/skills/sk1/sk1" ] && ng "ln -sfn が実ディレクトリの中に潜り込んだ (skills/sk1/sk1 ができた)"
[ -L "$CH/skills/sk1" ] && ng "実ディレクトリ skills/sk1 を symlink に置き換えた"

# --- 7. dir symlink (旧形式) なら 1 件も張らずに rc=2。repo 側に自己参照 link を作らない ---
fixture dirlink
ln -s "$DOT/_claude/rules" "$CH/rules"
run apply
[ "$RC" -eq 2 ] || ng "dirlink: rc=$RC (2 のはず)"$'\n'"$OUT"
grep -q 'refused: .*dir symlink' <<<"$OUT" || ng "dirlink 拒否を報告していない"$'\n'"$OUT"
[ "$(ls "$DOT/_claude/rules")" = "r1.md" ] || ng "repo 側 rules/ に何か書かれた: $(ls "$DOT/_claude/rules")"
[ -L "$DOT/_claude/rules/r1.md" ] && ng "r1.md が自己参照 symlink に置き換わった"
[ -e "$CH/agents" ] && ng "dirlink 拒否時に他 dir (agents) を作った"

# --- 8. 検査不能 (~/.claude が無い / _claude が無い) は rc=3。0 に丸めない ---
fixture nohome
rmdir "$CH"
run check
[ "$RC" -eq 3 ] || ng "CLAUDE_HOME 無しで rc=$RC (3 のはず)"
grep -q 'cannot check' <<<"$OUT" || ng "検査不能を報告していない"
fixture noroot
rm -rf "$DOT/_claude"
run check
[ "$RC" -eq 3 ] || ng "_claude 無しで rc=$RC (3 のはず)"

# --- 9. hook: 揃っていれば無出力 (毎セッションのノイズにしない) ---
fixture hook_ok
run apply
hook_run
[ "$RC" -eq 0 ] || ng "hook rc=$RC"
[ -z "$CTX" ] || ng "揃っているのに hook が出力した:"$'\n'"$CTX"

# --- 10. hook: 欠けがあれば張って報告する。報告に張った path が出る ---
fixture hook_fix
hook_run
[ "$RC" -eq 0 ] || ng "hook(fix) rc=$RC"
grep -q '補った' <<<"$CTX" || ng "hook が補ったことを報告していない:"$'\n'"$CTX"
grep -q "linked: $CH/rules/r1.md" <<<"$CTX" || ng "hook 報告に張った path が無い:"$'\n'"$CTX"
grep -q '7 件を張った' <<<"$CTX" || ng "hook 報告の件数が違う:"$'\n'"$CTX"
[ "$CH/rules/r1.md" -ef "$DOT/_claude/rules/r1.md" ] || ng "hook が実際には張っていない"
hook_run
[ -z "$CTX" ] || ng "2 回目の hook が出力した (毎回 apply している):"$'\n'"$CTX"

# --- 11. hook: 張れないものがあれば setup.sh を促す。rc は 0 のまま (本体を止めない) ---
fixture hook_refuse
mkdir -p "$CH/rules"; echo mine >"$CH/rules/r1.md"
hook_run
[ "$RC" -eq 0 ] || ng "hook(refuse) rc=$RC (常に 0 のはず)"
grep -q 'refused:' <<<"$CTX" || ng "hook が refused を伝えていない:"$'\n'"$CTX"
grep -q 'setup.sh を手で実行' <<<"$CTX" || ng "hook が setup.sh を促していない:"$'\n'"$CTX"

# --- 12. hook: 検査不能 (~/.claude 無し) でも黙らない、rc 0 ---
fixture hook_nohome
rmdir "$CH"
hook_run
[ "$RC" -eq 0 ] || ng "hook(nohome) rc=$RC"
grep -q 'できなかった' <<<"$CTX" || ng "hook が検査不能を伝えていない:"$'\n'"$CTX"
[ -e "$CH" ] && ng "検査不能なのに ~/.claude を作った"

# --- 13. hook: スクリプトが無ければ配線ミスとして報告する (無言で exit 0 しない) ---
fixture hook_noscript
rm "$DOT/scripts/claude_links.sh"
hook_run
[ "$RC" -eq 0 ] || ng "hook(noscript) rc=$RC"
grep -q '省略した' <<<"$CTX" || ng "hook がスクリプト不在を伝えていない:"$'\n'"$CTX"

if [ "$fails" -ne 0 ]; then
  echo "FAIL: $fails 件"
  exit 1
fi
echo "OK: claude_links.sh + claude-links-sync.sh (14 ケース)"
