#!/usr/bin/env bash
# scripts/tmux_git_popup.sh の unit テスト (PATH stub 方式。実 git / fzf / gum / 実 repo に
# 一切触れない)。固定する不変条件:
#   - toggle の判定: worktree 側に変更があれば add、staged のみなら restore --staged
#     (「確認した行」と「操作するパス」の一致: rename は新パス側・quote 付きパスは剥ぐ)
#   - commit の fail-safe: staged が無ければ commit しない・メッセージ空なら commit しない
#   - repo 外では fzf を起動せず非 0 で終わる
#   - fzf の配線: log/changes の遷移、toggle/commit/全add/diff の bind とインクリメンタル UI
#     (--ansi/preview) が
#     揃っていること (壊れても平常時に気づきにくい配線の回帰ガード)
set -euo pipefail
unset CDPATH
unset TMUX TMUX_PANE 2>/dev/null || true

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/tmux_git_popup.sh"
TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

[[ -x "$SCRIPT" ]] || { printf '✗ スクリプトが存在しない/実行不可: %s\n' "$SCRIPT"; exit 1; }

CALLS="$TMP_DIR/calls.log"
export CALLS
: > "$CALLS"

# CI キャッシュ (ci_status_lines) を実 ~/.cache から隔離する
export XDG_CACHE_HOME="$TMP_DIR/cache"

mkdir -p "$TMP_DIR/bin" "$TMP_DIR/bin_nogum"

# stub git: 呼び出しを記録し、テストごとの応答は環境変数で制御する
#   STUB_IN_REPO=0    → rev-parse --is-inside-work-tree が失敗 (repo 外)
#   STUB_HAS_STAGED=1 → diff --cached --quiet が非 0 (staged あり)
cat > "$TMP_DIR/bin/git" <<'EOS'
#!/bin/sh
echo "git $*" >> "$CALLS"
case "$*" in
  *"--is-inside-work-tree"*) [ "${STUB_IN_REPO:-1}" = 1 ] && exit 0 || exit 1 ;;
  *"rev-parse --abbrev-ref HEAD"*) echo master ;;
  *"diff --cached --quiet"*) [ "${STUB_HAS_STAGED:-0}" = 1 ] && exit 1 || exit 0 ;;
  *"status --short"*) [ "${STUB_CLEAN:-0}" = 1 ] || printf '?? new.txt\n M mod.txt\n' ;;
  *"--symbolic-full-name"*) [ "${STUB_NO_UPSTREAM:-0}" = 1 ] && exit 1; echo origin/master ;;
  *"rev-list --left-right --count"*) printf '0\t2\n' ;;
  *"rev-list --count"*) echo "${STUB_AHEAD:-20}" ;;
  *"log --oneline"*) echo "abc1234 fake log" ;;
  *"show "*) echo "GITSHOW-DIFF-BODY" ;;  # logpreview の diff 部 (順序検証用マーカー)
esac
exit 0
EOS

# stub fzf: 引数を記録し、STUB_FZF_KEY があれば --expect のキーとして 1 行目に出す。
# STUB_FZF_KEY_SEQUENCE は fzf 呼数ごとのキーを空白区切りで指定する。
cat > "$TMP_DIR/bin/fzf" <<'EOS'
#!/bin/sh
# 引数は 1 行に平坦化して記録する (changes の header は改行入り 2 行のため、素の $* だと
# 1 起動が複数行になり「fzf 起動行」単位の抽出が壊れる)。
printf 'fzf %s\n' "$(printf '%s' "$*" | tr '\n' ' ')" >> "$CALLS"
cat > /dev/null
count=$(grep -c '^fzf ' "$CALLS" 2>/dev/null || echo 0)
if [ -n "${STUB_FZF_KEY_SEQUENCE:-}" ]; then
  key=$(printf '%s\n' "$STUB_FZF_KEY_SEQUENCE" | awk -v n="$count" '{print $(n)}')
else
  key=${STUB_FZF_KEY:-}
fi
printf '%s\n' "$key"
exit 0
EOS

# stub gum: input はテスト制御のメッセージを返す (STUB_GUM_MSG)
cat > "$TMP_DIR/bin/gum" <<'EOS'
#!/bin/sh
echo "gum $*" >> "$CALLS"
[ "$1" = confirm ] && exit "${STUB_CONFIRM_RC:-0}"
printf '%s\n' "${STUB_GUM_MSG:-}"
EOS

# stub gh: check-runs API を叩かれたら jq 整形済み相当 (state<TAB>name) を返す。
# 実際の gh は --jq で整形するが、stub は最終形だけ返せば ci_status_lines の描画を検証できる。
cat > "$TMP_DIR/bin/gh" <<'EOS'
#!/bin/sh
echo "gh $*" >> "$CALLS"
case "$*" in
  *check-runs*) printf 'success\tbuild\nfailure\tlint\nin_progress\tdeploy\n' ;;
esac
exit 0
EOS

# stub sleep: テストを待たせない
cat > "$TMP_DIR/bin/sleep" <<'EOS'
#!/bin/sh
exit 0
EOS

chmod +x "$TMP_DIR/bin/"*
cp "$TMP_DIR/bin/git" "$TMP_DIR/bin/fzf" "$TMP_DIR/bin/sleep" "$TMP_DIR/bin_nogum/"
chmod +x "$TMP_DIR/bin_nogum/"*

source "$ROOT_DIR/tests/tmux/lib/stub_assert_helper.sh"
STUB="$TMP_DIR/bin:/usr/bin:/bin"
STUB_NOGUM="$TMP_DIR/bin_nogum:/usr/bin:/bin"

echo "## main: fzf の配線"
reset_calls
# log を確認してから changes に一度遷移し、両 mode の配線を検査する。
STUB_FZF_KEY_SEQUENCE='ctrl-l' run "$STUB" "$SCRIPT"
[[ "$RC" == 0 ]] || { echo "✗ main が rc=$RC"; cat "$CALLS"; exit 1; }
log_line=$(grep -n 'git log --oneline' "$CALLS" | head -n1 | cut -d: -f1)
status_line=$(grep -n 'status --short' "$CALLS" | head -n1 | cut -d: -f1)
# log が先出し = git log --oneline が changes の list(status --short)より前に呼ばれる
[[ -n "$log_line" && -n "$status_line" && "$log_line" -lt "$status_line" ]] || {
  echo "✗ log が先出しになっていない (log 行=$log_line status 行=$status_line)"; cat "$CALLS"; exit 1;
}
echo "✓ popup は最初に git log (log 画面) を開く (changes より前)"
assert_called "git rev-parse --is-inside-work-tree" "repo 判定を行う"
assert_called "--ansi" "fzf はインクリメンタル UI (--ansi) で起動される"
assert_called "--expect=ctrl-l" "fzf は C-l を log 遷移キーとして捕捉する"

# log mode 専用の配線は「log 用 fzf 起動行」= --preview に git show を持つ行に対して検査する。
# assert_called は全 fzf 行を横断するため、changes mode にしか無い bind でも通ってしまう穴を塞ぐ。
log_fzf=$(grep '^fzf ' "$CALLS" | grep -F 'git show' | head -n1)
[[ -n "$log_fzf" ]] || { echo "✗ log mode の fzf 起動が記録されていない"; cat "$CALLS"; exit 1; }
for pat in 'ctrl-g:abort' 'ctrl-b:execute' '--expect=ctrl-l' 'logpreview {1}' 'enter:execute(git show'; do
  case "$log_fzf" in
    *"$pat"*) echo "✓ log mode fzf に $pat が配線されている" ;;
    *) echo "✗ log mode fzf に $pat が無い: $log_fzf"; exit 1 ;;
  esac
done

# changes mode 専用の配線は「changes 用 fzf 起動行」= --preview に \"$self\" preview を持つ行で検査する。
changes_fzf=$(grep '^fzf ' "$CALLS" | grep -F 'preview {}' | head -n1)
[[ -n "$changes_fzf" ]] || { echo "✗ changes mode の fzf 起動が記録されていない"; cat "$CALLS"; exit 1; }
for pat in 'ctrl-g:abort' '--expect=ctrl-l' 'toggle {}' 'git add -A' 'ctrl-o:execute' 'ctrl-b:execute' 'preview {}'; do
  case "$changes_fzf" in
    *"$pat"*) echo "✓ changes mode fzf に $pat が配線されている" ;;
    *) echo "✗ changes mode fzf に $pat が無い: $changes_fzf"; exit 1 ;;
  esac
done

echo ""
echo "## main: log と changes を C-l で往復する"
reset_calls
# 1 回目の log fzf で changes へ → 2 回目の changes fzf で log へ → 3 回目で閉じる
STUB_FZF_KEY_SEQUENCE='ctrl-l ctrl-l' run "$STUB" "$SCRIPT"
[[ "$RC" == 0 ]] || { echo "✗ ラウンドトリップで rc=$RC"; cat "$CALLS"; exit 1; }
log_count=$(grep -c 'git log --oneline' "$CALLS" || true)
status_count=$(grep -c 'status --short' "$CALLS" || true)
[[ "$log_count" -ge 2 && "$status_count" -ge 1 ]] || {
  echo "✗ C-l の log↔changes 遷移が確認できない"; cat "$CALLS"; exit 1;
}
echo "✓ log→changes→log の C-l 遷移"
assert_called "fzf" "changes 画面では fzf を起動する"

echo ""
echo "## logpreview: 選択コミットの CI job (glog 風) + git show"
reset_calls
rm -rf "$TMP_DIR/cache"  # cache を消して必ず gh を叩かせる
RUN_OUT="$TMP_DIR/logpreview.out" run "$STUB" "$SCRIPT" logpreview abc1234
[[ "$RC" == 0 ]] || { echo "✗ logpreview rc=$RC"; cat "$CALLS"; exit 1; }
assert_called "gh api" "CI 取得に gh api を叩く (glog は使わず popup で gh を直接)"
assert_called "git show" "diff は git show で出す"
# ANSI を剥いで state↔job 名の対応・記号・並びを厳密に検証する
esc=$(printf '\033')
lp_plain=$(sed "s/${esc}\[[0-9;]*m//g" "$TMP_DIR/logpreview.out")
# gh stub は success/build・failure/lint・in_progress/deploy を返す → ✓/✗/● に対応
expected=$'─── CI ───\n  ✓ build\n  ✗ lint\n  ● deploy'
case "$lp_plain" in
  *"$expected"*) echo "✓ CI ブロックは state↔job 対応どおり (✓ build / ✗ lint / ● deploy)" ;;
  *) echo "✗ CI ブロックの並び/対応が違う:"; printf '%s\n' "$lp_plain"; exit 1 ;;
esac
# CI ブロックは git show の diff (GITSHOW-DIFF-BODY) より前に出る
ci_pos=$(printf '%s\n' "$lp_plain" | grep -n '─── CI ───' | head -1 | cut -d: -f1)
show_pos=$(printf '%s\n' "$lp_plain" | grep -n 'GITSHOW-DIFF-BODY' | head -1 | cut -d: -f1)
[[ -n "$ci_pos" && -n "$show_pos" && "$ci_pos" -lt "$show_pos" ]] || {
  echo "✗ CI が git show より後ろ (ci 行=$ci_pos show 行=$show_pos)"; printf '%s\n' "$lp_plain"; exit 1;
}
echo "✓ CI ブロックは git show の diff より前に出る"
# 2 回目は cache が効いて gh を叩かない
reset_calls
RUN_OUT=/dev/null run "$STUB" "$SCRIPT" logpreview abc1234
assert_not_called "gh api" "60 秒以内の再表示は cache を使い gh を叩かない"

echo ""
echo "## main: changes へ移動後、working tree が clean ならサマリ画面"
reset_calls
STUB_CLEAN=1 STUB_FZF_KEY=ctrl-l run "$STUB" "$SCRIPT" < /dev/null
[[ "$RC" == 0 ]] || { echo "✗ clean 時に rc=$RC"; cat "$CALLS"; exit 1; }
assert_called "git log --oneline" "最初に log 一覧を表示する"
assert_called "fzf" "log/changes は fzf で表示する"
assert_called "rev-list --left-right --count" "upstream との ahead/behind を判定する"
assert_called "rev-list --count --max-count=20" "未 push ありならドットグラフ用に直近 commit 数を取る"
assert_not_called "gum" "clean 画面は素の ANSI で描く (gum 非依存)"

echo ""
echo "## main: repo 外では起動しない"
reset_calls
STUB_IN_REPO=0 run "$STUB" "$SCRIPT"
[[ "$RC" != 0 ]] || { echo "✗ repo 外で rc=0"; exit 1; }
echo "✓ repo 外は非 0 で終了"
assert_not_called "fzf" "repo 外では fzf を起動しない"

echo ""
echo "## toggle: stage/unstage の判定とパス解決"
reset_calls
run "$STUB" "$SCRIPT" toggle "?? new.txt"
assert_called "git add -- new.txt" "untracked → add"

reset_calls
run "$STUB" "$SCRIPT" toggle " M mod.txt"
assert_called "git add -- mod.txt" "worktree 変更 → add"

reset_calls
run "$STUB" "$SCRIPT" toggle "MM both.txt"
assert_called "git add -- both.txt" "staged+worktree 混在 → 残りを add"

reset_calls
run "$STUB" "$SCRIPT" toggle "M  staged.txt"
assert_called "git restore --staged -- staged.txt" "staged のみ → unstage"

reset_calls
run "$STUB" "$SCRIPT" toggle "R  old.txt -> new-name.txt"
assert_called "git restore --staged -- new-name.txt" "rename は新パス側を操作"

reset_calls
run "$STUB" "$SCRIPT" toggle '?? "a b.txt"'
assert_called 'git add -- a b.txt' "quote 付きパスは剥いで操作"

reset_calls
run "$STUB" "$SCRIPT" toggle ""
assert_not_called "git add" "空行 (リスト空) では何もしない"

echo ""
echo "## commit: fail-safe"
reset_calls
STUB_HAS_STAGED=0 run "$STUB" "$SCRIPT" commit
assert_not_called "git commit" "staged が無ければ commit しない"

reset_calls
STUB_HAS_STAGED=1 STUB_GUM_MSG="fix: テスト" run "$STUB" "$SCRIPT" commit
assert_called "git commit -m fix: テスト" "gum のメッセージで commit する"

reset_calls
STUB_HAS_STAGED=1 STUB_GUM_MSG="" run "$STUB" "$SCRIPT" commit
assert_not_called "git commit" "メッセージ空なら commit しない"

echo ""
echo "## push サブコマンド"
reset_calls
run "$STUB" "$SCRIPT" push < /dev/null
[[ "$RC" == 0 ]] || { echo "✗ push が rc=$RC"; cat "$CALLS"; exit 1; }
assert_called "git push" "push サブコマンドは git push を実行する"

reset_calls
STUB_AHEAD=0 run "$STUB" "$SCRIPT" push < /dev/null
assert_not_called "git push" "未 push コミットが無ければ push せずメッセージを出す"

reset_calls
STUB_NO_UPSTREAM=1 run "$STUB" "$SCRIPT" push < /dev/null
assert_not_called "git push" "upstream が無ければ push せずメッセージを出す"

reset_calls
run "$STUB" "$SCRIPT" push < /dev/null
assert_called "gum confirm --default=false" "push 前に gum confirm (デフォルト No) を挟む"

reset_calls
STUB_CONFIRM_RC=1 run "$STUB" "$SCRIPT" push < /dev/null
assert_not_called "git push" "confirm 拒否なら push しない"

reset_calls
run "$STUB_NOGUM" "$SCRIPT" push <<< "n"
assert_not_called "git push" "gum 無し: read fallback で n なら push しない"

reset_calls
run "$STUB_NOGUM" "$SCRIPT" push <<< "y"
assert_called "git push" "gum 無し: read fallback で y なら push する"

echo ""
echo "## commit: gum 未導入の degrade (read fallback)"
reset_calls
STUB_HAS_STAGED=1 run "$STUB_NOGUM" "$SCRIPT" commit <<< "chore: fallback"
assert_called "git commit -m chore: fallback" "gum 無しでは read でメッセージを受ける"

echo ""
echo "[test-git-popup] all assertions passed"
