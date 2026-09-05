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

# トーストの配送先はクライアントで決まる (-t はフォーマット文脈にしか効かない。実測 2026-09-05:
# 外部から起こすと「最後に活動したクライアント」へ出た)。bind から #{client_name} を受け取れた
# ときだけ -c で固定し、無ければサーバ既定に任せる
toast() {
  if [ -n "$client" ]; then
    tmux display-message -d 1200 -c "$client" "$1"
  else
    tmux display-message -d 1200 -t "$pane" "$1"
  fi
}

tmp="$(mktemp -t tmux-paste)" || { toast "🚨 ペースト失敗: mktemp"; exit 0; }
trap 'rm -f "$tmp"' EXIT
buf="ctrlv-$$"

pbpaste >"$tmp" 2>/dev/null
if [ ! -s "$tmp" ]; then
  toast "🚨 ペーストなし: クリップボードが空"
  exit 0
fi

if ! tmux load-buffer -b "$buf" "$tmp" 2>/dev/null; then
  toast "🚨 ペースト失敗: load-buffer"
  exit 0
fi
if ! tmux paste-buffer -d -p -b "$buf" -t "$pane" 2>/dev/null; then
  tmux delete-buffer -b "$buf" 2>/dev/null
  toast "🚨 ペースト失敗: 貼り先のペインが無い"
  exit 0
fi

toast "📋 tmux 経由でペースト: $(wc -c <"$tmp" | tr -d ' ') バイト"
