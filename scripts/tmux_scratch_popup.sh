#!/bin/sh
# scratch popup のトグル (bind t / C-t から `run-shell '... #{q:client_name} #{q:session_name}'`
# で呼ばれる)。開閉判定ごとここに集約する (if-shell -F 込みの 200 文字超の bind
# 文字列を t / C-t に複製しない。二重エスケープで壊れやすい)。
# 引数: $1 = client_name / $2 = session_name (いずれも run-shell の format 展開で渡る。
#   🚨 #{q:} でシェル特殊文字をエスケープすること。素の "#{...}" 埋めは値に " が入ると
#   sh 構文エラーになる) / $3 = 押したキー (t か C-t。bind の字面。観測ログに書くだけ)
#   --inner = popup の中で動く側 (下の display-popup が自分を起こす)
#
# 開閉は観測ログ (~/.cache/tt-restore-trigger.log) に scratch-popup-start / -end / -close-key として残す
# (scripts/tmux_scratch_popup_log.sh)。popup が見えないまま開いていてキーが scratch に入っても、いつ・どのキーで開き、どう閉じたかをログから追える。
# 書き手は背景で起こし、開く・閉じるのを待たせない
#
# - session_name が scratch = popup 内で押された
#   → detach で popup を閉じ、全クライアントを再描画。refresh は旧 bind の run-shell -b と
#   同じくバックグラウンドで走らせ、tmux サーバをブロックしない。
# - それ以外 → scratch popup を開く
#
# ここに集約した演出:
# - 2 色枠: 罫線 fg=colour33 (電子ブルー) × 地 bg=colour201 (ショッキングピンク) の固定。
#   (開くたびに色が変わる「枠色ガチャ」を一度実装したが、毎回同じ青×ピンクという視覚
#   アイデンティティを優先してユーザー判断で固定に戻した 2026-07-02)。中身は濃紺 (-s bg=colour17)。
# - 動的タイトル: 開いた時刻 (#{t/f/%H#:%M:client_activity} = C-t t の keypress 時刻) を
#   スナップショット表示 (生時計にはならない。per-draw のタイトル再展開は 3.5a に無い)。
#   🚨 bare の %H:%M は -T では strftime 展開されない (実測)。t/f 修飾子経由が必須。
# - status 2 行化: scratch セッションだけ status 2 にして演出 2 行目 (status-format[1],
#   _tmux.conf 側で定義) を出す。通常セッションは 1 行のまま。
#
# 🚨 以下の不変条件は _tmux.conf の bind t コメント (孤児サーバ予防の経緯) 由来。壊さないこと:
# - `unset TMUX TMUX_TMPDIR`: nested attach ガード越え + 呼び出し元が継承 TMUX_TMPDIR を
#   持つ環境 (テストサーバ等) でも scratch を必ず実 default socket 側に作り孤児を生まない
# - 既存 scratch に `new-session -d -A` を打ってはいけない (popup 内の最初の C-t t が
#   1 回効かず「閉じるのに 2 回押す」回帰。実測)。attach 失敗時のみ作成する
# - status 2 は「作成時に 1 回だけ」設定する (session option はセッションが生きている限り
#   永続するので毎回叩く必要がない。点滅/スピナーの駆動は global の status-interval=1)
#
# 開き側の高速化 (2026-07-04): has-session → set status 2 → attach と popup 内で
# tmux client を毎回 3 回 fork する形にしない。まず attach を直接試し (通常パス = fork 1 回)、
# 失敗した時だけ new-session \; set status 2 → attach で作成する (作成パスのみ 3 回)。
# 2>/dev/null は attach 失敗時 ("no sessions" 等) のエラー表示の抑止 (直後に作成するので不要)。
# 通常パスの attach は exec しない (attach の成否で || 分岐する shell が必要なため sh が
# 1 個 popup 内に残るが、popup 自体が sh で動いており実害なし。この sh が end の行を書く)

log="${TT_SCRATCH_POPUP_LOG:-$(dirname "$0")/tmux_scratch_popup_log.sh}"  # env はテストの差し込み口
# -e で中の shell へ渡すのは、client の名前 (tty のパス) と bind の字面のキーだけにする。セッション名は渡さない
# (tmux は「; で終わる引数」をコマンドの区切りと読むので、`foo;` のような名前で display-popup が切れて開かなくなる)。
# 外側のセッションは書き手が list-clients から引く

if [ "${1:-}" = "--inner" ]; then
  # 外側のサーバの TMUX は書き手のゲート (本番のサーバか) に要るので、外す前に控える
  outer_tmux="${TMUX:-}"
  unset TMUX TMUX_TMPDIR
  # 書き手は背景で起こし、HUP と TERM を無視させる。待って書くと popup が閉じるのがその分 (約 40ms) 遅れ、
  # その間に打ったキーが閉じかけの pty に入って消える。無視させずに背景で起こすと、popup の片付けが書き手ごと止め、
  # 行が残らない (隔離サーバで実測: 外側のクライアントが消えたとき、shell は HUP の trap まで動くが、背景の書き手は書く前に消えた)。
  # 無視は fork の前にこの sh で入れて子に引き継がせる。子の中で `trap ''` すると、そこへ届く前に、この sh
  # (pty のセッションリーダー) の終了で前面のグループへ送られる HUP で子が死ぬ (pty の下で 100 回に 1 回 end が欠けた)
  sp_log() { trap '' HUP TERM; ( TMUX="$outer_tmux"; export TMUX; exec "$log" "$@" ) </dev/null >/dev/null 2>&1 & }
  # 外側のクライアントが消える (Terminal の窓を閉じる) と、tmux が popup の job (この sh) に TERM を送り、pty が閉じて HUP も来る。
  # TERM の trap を外すと、sh は HUP の trap より先に TERM で死に、end が残らない (隔離サーバで実測)
  sp_traps() {
    trap 'sp_log end "$TT_SP_CLIENT" signal=HUP shell=$$; exit 129' HUP
    trap 'sp_log end "$TT_SP_CLIENT" signal=TERM shell=$$; exit 143' TERM
  }
  sp_log start "$TT_SP_CLIENT" key="$TT_SP_KEY" shell=$$
  sp_traps
  tmux attach -t scratch 2>/dev/null || { tmux new-session -d -s scratch \; set -t scratch status 2; tmux attach -t scratch; }
  rc=$?
  sp_log end "$TT_SP_CLIENT" rc=$rc shell=$$
  exit "$rc"
fi

client="$1"
session="${2:-}"
key="${3:-?}"

if [ "$session" = "scratch" ]; then
  "$log" close-key "$client" key="$key" </dev/null >/dev/null 2>&1 &
  # shellcheck disable=SC2086 # ${client:+...} は client 空のとき引数ごと消す意図の word splitting
  tmux detach-client ${client:+-t "$client"}
  # 以前あった close 後の refresh 強制 (tmux 3.5a の popup 再描画同期バグ #4920 対策) は、
  # #4920 が 3.7 で修正され不要になったため撤去 (2026-07-15、稼働 3.7b)。
  exit 0
fi

# shellcheck disable=SC2016 # $TT_SP_SELF は popup の中の shell が展開する (パスを字面に埋めると ' を含むパスで壊れる)
exec tmux display-popup -E -w 80% -h 75% -b heavy \
  ${client:+-c "$client"} \
  -S "fg=colour33,bg=colour201,bold" \
  -s 'bg=colour17' \
  -T "#[fg=colour16] ⚡ SCRATCH #{t/f/%H#:%M:client_activity}〜 — nested tmux (C-t t で閉じる) ⚡ " \
  -e "TT_SP_CLIENT=$client" -e "TT_SP_KEY=$key" -e "TT_SP_SELF=$0" \
  'exec sh "$TT_SP_SELF" --inner'
