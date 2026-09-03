#!/usr/bin/env bash
# tmux: 手動復元 (resurrect restore.sh) の確認 popup ヘルパー。_tmux.conf の bind C-r から
# display-popup -E 経由で呼ばれる。誤爆防止のため gum confirm を挟む
# (実例: C-t の Ctrl を離さないまま r → C-t C-r で稼働中サーバへ手動復元が暴発 2026-07-17。
#  自動復元はサーバ起動時に走るため、稼働中サーバへの手動復元はほぼ誤爆でしか起きない)。
#
# 🚨 set -e は使わない: fail-safe は `gum confirm && restore` の && 短絡に依存しており
#    (gum 未導入なら exit 127 で復元されない)、kill_confirm と同じ構造を保つ。
set -uo pipefail

# restore.sh は plugin が set する @resurrect-restore-script-path から解決する (tt の plugin
# 未ロード検知 _tt_wait_for_restore と同じ出典。ハードコードすると vendor 移動で silent に壊れる)
restore="$(tmux show -gqv @resurrect-restore-script-path)"
if [ -z "$restore" ] || [ ! -f "$restore" ]; then
  echo "resurrect 未ロード (@resurrect-restore-script-path が空/不在)。復元できません。" >&2
  sleep 3
  exit 1
fi
# 🚨 実行は popup 内で同期しない。popup -E 内で restore.sh を走らせると「popup を閉じる /
# キー入力 = 復元プロセスの kill」になり途中死する (2026-07-30 実発: pane 60/93 で途中死し
# 6 セッション未復元 + in-progress フラグ残置)。runner (detach + 異常終了の観測) に委ねる。
gum confirm --default=false --affirmative "復元する" --negative "やめる" \
  "保存済み状態を稼働中サーバへ手動復元する？(通常は boot 時の自動復元で足りる)" \
  && {
    tmux run-shell -b "\${DOTFILES_DIR:-\$HOME/dotfiles}/scripts/tmux_restore_runner.sh"
    echo "復元をバックグラウンドで開始しました (進捗: ~/.cache/tt-restore-trigger.log)"
    sleep 1
  }
