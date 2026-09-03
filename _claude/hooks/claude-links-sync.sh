#!/usr/bin/env bash
#
# SessionStart フック: _claude/ の rule / skill / agent / command / hook / workflow が ~/.claude/ に
# link されているか点検し、**欠けているときだけ** 張ってその内容をコンテキストへ注入する。
#
# なぜ: per-file link 方式なので、rule を足した commit の後に ./setup.sh を忘れると、その rule は
# Claude Code から一切読まれない。既存の防御は「CLAUDE.md の規律 (忘れると止まらない)」と
# 「make test の test_claude_links_complete.sh (踏むのは次に test を回した無関係な人)」で、
# どちらも当事者に届かない (issue 160)。セッション起動という必ず通る場所で自動修復する。
# pull で別 machine から入ってきた分にも同じ経路で効く (post-commit / post-merge の git hook より
# 契機を選ばない)。
#
# 毎回 apply はしない。check (外部コマンド無し。builtin の -L / -ef 判定と subshell 2 回、実測 20ms 前後 /
# 95 件) で揃っていれば無言で exit 0。
# 破壊的操作 (dangling の削除・dir symlink の migrate・実ファイルや他ツールの link の上書き) は
# 持たない = scripts/claude_links.sh apply の契約。それらが要る状態は「張れなかった」として
# 報告し、./setup.sh を手で回すよう促す。
#
# 🚨 この起動で張った rule が**この**セッションで読み込まれているかは保証しない (rules の読み込みと
# SessionStart hook の順序は未実測)。報告文は「遅くとも次のセッションから」と書く。
#
# 出力: 欠けがあったとき / 検査できなかったときだけ emit。全部揃っていれば無出力。
# いかなる失敗でも exit 0 (hook が本体を止めない)。

set -u

# ${HOME:-}: set -u 下で HOME 未設定でも落ちず、下の「スクリプトが無い」報告へ倒す (常に exit 0 を守る)
root="${DOTFILES_ROOT:-${HOME:-}/dotfiles}"
script="$root/scripts/claude_links.sh"

lib="$(dirname "$0")/lib/issue-hooks.sh"
# shellcheck source=_claude/hooks/lib/issue-hooks.sh
if ! . "$lib" || ! command -v issue_hook_emit >/dev/null 2>&1; then
  # emit ヘルパーが無くても黙らない (SessionStart の stdout はそのままコンテキストに入る)
  issue_hook_emit() { printf '%s\n%s\n' "$1" "$2"; }
fi

if [ ! -x "$script" ]; then
  issue_hook_emit 'link 点検 (~/.claude) を省略した (hook の配線を確認する):' \
    "$script が無い / 実行できない (DOTFILES_ROOT=$root)"
  exit 0
fi

# stdin の hook JSON は使わない (cwd に依存しない。対象は常に ~/dotfiles → ~/.claude)
check_out=$("$script" check 2>&1)
check_rc=$?

case "$check_rc" in
  0) exit 0 ;;
  1) ;;
  *)
    issue_hook_emit 'link 点検 (~/.claude) ができなかった (揃っている保証は無い):' \
      "$check_out"
    exit 0
    ;;
esac

apply_out=$("$script" apply 2>&1)
apply_rc=$?

n=$(printf '%s\n' "$apply_out" | grep -c '^linked: ' || true)
report=$(
  printf '%s\n' "$apply_out"
  printf -- '---\n'
  if [ "$apply_rc" -eq 0 ]; then
    printf '%s 件を張った。張った rule はこのセッションで読まれている保証が無く、遅くとも次のセッションから有効。\n' "$n"
    printf 'dotfiles 側で消した / 改名したファイルの旧 link (dangling) はここでは掃除しない。./setup.sh が担う。\n'
  else
    printf 'refused / failed があるので ./setup.sh を手で実行する (rc=%s)。\n' "$apply_rc"
  fi
)

issue_hook_emit 'link されていない _claude/ のファイルが ~/.claude にあったので補った (Claude Code は link の無い rule / skill を読まない)。セッション冒頭で一言伝えること:' \
  "$report"
exit 0
