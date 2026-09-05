#!/usr/bin/env bash
# C-v (root テーブル・prefix なし) の実体。クリップボードを tmux 自身が読み、
# 端末エミュレータ -> tmux のキー入力経路を通さずにペインへ流し込む (issue 138)。
# conf 側の bind と背景は _tmux.conf の "大量ペースト" 節を見る。
#
# 🚨 貼り先は引数の pane_id で明示する。入れ子の tmux コマンドは run-shell の対象ペインを
# 継承せず「今アクティブなペイン」へ貼るので、複数クライアントが別ペインを見ていると誤爆する。
#
# 🚨 バッファ名に $$ を付けて使い捨てにする。固定名だと C-v のキーリピートや二度押しで
# 2 本同時に走ったとき、後発の paste-buffer が "no buffer" で落ちる (実測 2026-09-05)。
#
# 🚨 失敗は display-message で出し、非 0 で終わらない。run -b の失敗出力はペインを
# view-mode に落とし、q を押すまで入力できなくなる (実測 2026-09-05)。
#
# トーストにバイト数を出すのは、⌘V と数字を比べて「落ちたか」を人が判定できるようにするため
# (issue 248 の確認 1)。tmpfile を挟むのは pbpaste を 2 回呼ぶと 2 回目に別の内容を掴みうるため。
set -uo pipefail

pane="${1:?usage: tmux_paste_clipboard.sh <pane_id> [client_name]}"
client="${2-}"

# 通知は右下の floating pane (bin/tmux-toast) に出す。conf の他の通知と同じ見た目にするため。
# ステータス行の display-message はセッション名の領域を潰して読みづらい (2026-09-05 のフィードバック)。
#
# 🚨 TMUX_PANE を渡してから呼ぶ。tmux-toast の中の tmux コマンドは既定ターゲットを使うので、
# 渡さないと「最後に活動したクライアント」の画面へ出る (複数クライアントで誤爆する)。
# ⚠️ **TMUX_PANE が選ぶのはセッションまで**で、pane/window はそのセッションの current window の
# active pane に丸められる (実測 2026-09-05: TMUX_PANE=%1 でも #{pane_id} は %0 を返した)。
# キー押下の経路では押したペイン = current window の active pane なので一致するが、
# ペースト直後に window を切り替えると別 window に出る (未確認。機序上はそうなる)。
#
# 🚨 -F でノイズ抑止ガードを外す。tmux-toast の既定は「pane 分割の装飾通知」向けに
# 2 秒デバウンス + agent panel の 3 秒 quiet 窓を持つが、ペースト通知は**押した本人への
# 直接の応答**なので間引かれると「効いていない」に見える (実測 2026-09-05: window 切替直後の
# ペーストが panel の quiet 窓に食われて無音だった)。
#
# tmux-toast を呼べない環境 ($TMUX が無い / スクリプトが無い) ではステータス行へ落とす。
# ⚠️ tmux-toast は new-pane に失敗しても exit 0 で閉じる契約 (hook にエラーを積まないため) なので、
# 「floating pane を作れなかった」はここでは拾えない (その場合は tmux-toast 内部の
# クライアント tty 直描画の fallback が受ける)。
TOAST_BIN="$(cd "$(dirname "$0")/../bin" && pwd)/tmux-toast"
toast() {
  if [ -x "$TOAST_BIN" ] && TMUX_PANE="$pane" "$TOAST_BIN" -F ${2:+-b "$2"} "$1" 2>/dev/null; then
    return 0
  fi
  if [ -n "$client" ]; then
    tmux display-message -d 1200 -c "$client" "$1"
  else
    tmux display-message -d 1200 -t "$pane" "$1"
  fi
}

tmp="$(mktemp -t tmux-paste)" || { toast "🚨 ペースト失敗: mktemp" 52; exit 0; }
trap 'rm -f "$tmp"' EXIT
buf="ctrlv-$$"

pbpaste >"$tmp" 2>/dev/null
if [ ! -s "$tmp" ]; then
  toast "🚨 ペーストなし: クリップボードが空" 52
  exit 0
fi

if ! tmux load-buffer -b "$buf" "$tmp" 2>/dev/null; then
  toast "🚨 ペースト失敗: load-buffer" 52
  exit 0
fi
if ! tmux paste-buffer -d -p -b "$buf" -t "$pane" 2>/dev/null; then
  tmux delete-buffer -b "$buf" 2>/dev/null
  toast "🚨 ペースト失敗: 貼り先のペインが無い" 52
  exit 0
fi

toast "📋 tmux 経由でペースト: $(wc -c <"$tmp" | tr -d ' ') バイト"
