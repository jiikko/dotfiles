#!/bin/sh
# scratch popup を prefix+t で閉じた直後 (bind t の detach-client 側) に呼ばれ、残っている
# tmux クライアントを即再描画して「閉じた後にボーダー(枠線)が ~1秒残る/もっさりする」
# アーティファクトを潰す。
#
# 背景 (tmux 3.5a の popup overlay 再描画同期バグ。修正 issue #4920 は未リリースの 3.7 のみ):
#   背景再描画圧 (scratch の status-interval=1 + 前面の TUI) の下で popup を閉じると、
#   teardown 再描画が次の再描画サイクル (status-interval≒1秒) まで遅延し枠が焼き付く。
#   放置でも ~1秒後の次サイクルで自動的に消える = 「再描画さえ走れば消える」ので、
#   閉じた直後に明示 refresh-client して即潰す。
#   tmux を 3.7+ (issue #4920 fix 入り。stable 3.6b には未収録・master/HEAD のみ) に上げたら、
#   本スクリプトと bind t の `; run-shell -b '...'` は不要になるので削除してよい。
#
# sleep 0.05: popup teardown 完了後に refresh を当てるため (teardown は ms オーダー)。待たずに
#   撃つと teardown 前に refresh して空振りしうる。1秒の自然再描画より十分速く体感ゼロ。効かない
#   場合はこの値を 0.1 等に上げて実機検証する (それでも効かなければ本 fix を revert)。
# 全クライアントを refresh するのは、残骸が乗った client を確実に含めるため (他 client の再描画は
#   同一内容の描き直しで不可視・無害)。detach 後なので scratch の client は既に居ない。
sleep 0.05
tmux list-clients -F '#{client_name}' 2>/dev/null | while IFS= read -r c; do
  tmux refresh-client -t "$c" 2>/dev/null
done
