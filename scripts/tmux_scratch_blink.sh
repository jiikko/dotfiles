#!/bin/sh
# scratch セッションの status bar を「ソフト点滅」させるためのパリティ生成器。
#
# 呼び出し元: _tmux.conf の status-left。scratch セッション表示時のみ評価される
#   (#{?#{==:session_name,scratch},...} の条件で振り分け、偽側 #() は tmux が実行しない)。
#   現在秒の偶奇で "1"/"0" を返し、status-left 側の #{?#(...),色A,色B} が色を切り替える。
#   scratch は session 単位で status-interval=1 にしてあるので毎秒再描画され、色が交互に
#   反転して点滅する。SGR の blink 属性(端末依存)に頼らず実際に色を変えるので端末非依存。
#
# なぜスクリプトにするか: format に直接 #(expr $(date +%s) % 2) と書くと、tmux の #() パーサが
#   $(date +%s) の ")" を #() の閉じ括弧と誤認してコマンドが途中で切れ、常に空(=偽)になる。
#   スクリプトに閉じ込めれば format 側には ")" が出ないので壊れない。
expr "$(date +%s)" % 2
