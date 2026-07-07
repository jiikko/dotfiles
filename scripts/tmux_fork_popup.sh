#!/bin/sh
# claude-fork セッション (Claude 会話を --fork-session でフォークした detached セッション) を
# popup で attach する。_tmux.conf の `bind b` (C-t b) から display-popup -E 経由で呼ばれる前提。
#
# 設計:
# - claude-fork が存在すれば attach する (status-left を「🌿 FORK」表示に上書きしてから)。
# - 無ければ「/fork-scratch を実行して」と案内して閉じる。**空セッションは作らない**
#   (scratch の bind t は has-session||new-session で作るが、fork は「claude を resume した
#    セッション」でなければ無意味なので、ここで素の new-session すると空シェルの偽 fork に
#    なってしまう。作成は Claude 会話側の /fork-scratch の責務に一本化する)。
# - status-left の上書きはこの session 単位 (tmux set -t claude-fork)。scratch は global の
#   status-left format (_tmux.conf:90) を session 名で分岐させて帯を出すが、それは scratch の帯が
#   毎秒点滅する=動的評価 (status-interval=1 + パリティ #()) を要するため。fork の帯は静的なので
#   global format に分岐を足す (= 最も壊れやすい format 行を触る) 必要がなく、session option の
#   上書きで十分。よって機構が非対称なのは意図的 (静的 vs 点滅の差)。global format に触れないので
#   他 session の status-left にも影響しない (blast radius 最小)。popup は本スクリプト経由でしか
#   開かず、毎回 attach 直前にここで set するので「再 fork で override が消える」失敗モードは
#   実害化しない (開くたびに緑帯が再設定される)。FORK 中は緑帯で scratch (点滅ピンク/赤) とも
#   通常セッション (青) とも一目で区別できる。
#
# ⚠️ `new-session -d -A` は使わない: 既存セッションに -A を打つと popup 内の最初の C-t b
#    (close 用 detach-client) が 1 回効かず「閉じるのに 2 回押す」回帰になる (scratch で実測・
#    _tmux.conf の bind t コメント参照)。ここは attach のみで -A を一切使わない。
set -eu

# nested attach ガード越え (TMUX) + 実 default socket の強制 (TMUX_TMPDIR)。
# TMUX_TMPDIR を落とさないと、継承 TMUX_TMPDIR を持つ文脈 (テストサーバ等) から開いたとき
# claude-fork を別 socket 側で探して「未作成」と誤案内する。scratch (tmux_scratch_popup.sh) /
# launcher (tmux_launcher_run.sh) と同方針。_tmux.conf の bind b「復活時の注意」も参照。
unset TMUX TMUX_TMPDIR

sess=claude-fork

if tmux has-session -t "$sess" 2>/dev/null; then
  # この session だけ status-left を FORK 表示に上書き (global format には触れない)
  tmux set -t "$sess" status-left '#[bg=colour22]#[fg=colour231]#[bold] 🌿 FORK (C-t b で閉じる) #[default]'
  exec tmux attach -t "$sess"
else
  printf '\n  フォーク未作成です。\n\n  Claude 会話で /fork-scratch を実行してから C-t b で開いてください。\n\n  (Enter で閉じる) '
  # `|| true`: set -e 下で read が EOF (popup の stdin 切断等) で非ゼロ終了しても
  # script をエラー終了させず popup を正常に閉じるため (display-popup -E は exit で閉じる)。
  read -r _ || true
fi
