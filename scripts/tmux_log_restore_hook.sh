#!/usr/bin/env bash
# resurrect の復元 hook (pre-restore-all / post-restore-all) から、観測ログへの書き込みだけを
# 引き受ける。tmux オプションの設定 (@tt-restore-in-progress / -duration / -complete) は
# _tmux.conf 側にインラインで残してある。
#
# なぜスクリプトに切り出したか (issue 079):
#   scripts/CLAUDE.md の不変条件「共有の観測ログ (~/.cache/tt-restore-trigger.log) に書く
#   スクリプトは default socket ゲート (tt_on_default_server) を通す」に対し、_tmux.conf の
#   2 つの hook だけが**唯一の例外**として無ゲートで書いていた。-L の隔離テストサーバも同じ
#   conf を source して同じ hook を持つため、テストの復元が本番ログへ restore-start /
#   restore-end を注入し、**watchdog の死因分類 (verdict) を汚す**。この 2 行はまさに verdict が
#   相関を取っている行そのもの。tmux_log_conf_source.sh が同じ理由で先に抽出されている。
#
# なぜ tmux オプションの設定まで持ってこないか:
#   @tt-restore-complete は「復元が最後まで到達した」の唯一の証拠で、tmux_restore_runner.sh の
#   途中死判定の入力になっている。conf のインラインは**ファイルの存在に依存しない**が、
#   スクリプトへ寄せるとパス解決の失敗が「復元は成功したのに途中死と記録される」に化ける。
#   ログはゲートしたい / オプションは絶対に落としたくない、で要求が逆なので分けてある。
#
# 無音契約: hook 経路なのでどんな失敗でも無音で exit 0 (scripts/CLAUDE.md 参照)。
set -uo pipefail
unset CDPATH

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
# shellcheck source=scripts/lib/tmux_resurrect_guards.sh
. "$SCRIPT_DIR/lib/tmux_resurrect_guards.sh"

TT_TRIGGER_LOG="${TT_TRIGGER_LOG:-$HOME/.cache/tt-restore-trigger.log}"
# 復元の所要時間だけを並べる人間用の診断ログ (自動の読み手は無い)。書き手はここ 1 箇所。
TT_DURATION_LOG="${TT_DURATION_LOG:-$HOME/.cache/tt-restore-duration.log}"

tt_on_default_server || exit 0

case "${1:-}" in
  pre)
    tt_trigger_log "restore-start epoch=$(date +%s)"
    ;;
  post)
    # 所要時間は conf 側が @tt-restore-duration に入れてある (ここでは読むだけ)。
    dur="$(tmux show -gqv @tt-restore-duration 2>/dev/null)"
    case "$dur" in ''|*[!0-9]*) dur=0 ;; esac
    { mkdir -p "$(dirname "$TT_DURATION_LOG")" \
        && printf '%s\trestore=%ss\n' "$(date +%FT%T)" "$dur" >> "$TT_DURATION_LOG"; } 2>/dev/null || true
    tt_trigger_log "restore-end rc=0 epoch=$(date +%s)"
    ;;
  *)
    exit 0   # 未知の引数は無音で無視する (hook の失敗で復元を止めない)
    ;;
esac
exit 0
