#!/usr/bin/env bash
# tmux: 呼び出し元ペインの画面+履歴から「意味のある断片」(URL / パス / git ハッシュ /
# 単語) を抽出し、fzf で選んで貼り付け・コピー・開く (extrakto 型)。
# _tmux.conf の `bind y` から display-popup -E 経由で呼ばれる前提。
#
# ⚠️ popup の shell-command 内では #{...} フォーマットは展開されない (bind x のコメント参照)。
# そのため対象ペインは popup 内シェルの `tmux display -p` で解決する
# (popup 内からの暗黙ターゲットは popup 直下のアクティブペインを指す。実測済)。
#
# fzf 内キー:
#   Enter  → 元ペインのコマンドラインへ貼り付け
#   Ctrl-Y → クリップボードへコピー (pbcopy)
#   Ctrl-O → URL は open / それ以外は $EDITOR で開く
set -euo pipefail
unset CDPATH

pane=$(tmux display -p '#{pane_id}')

# 画面 + 履歴 1000 行を取得し、下 (=新しい) 行を優先するため逆順にする。
# -J: 折り返し行を連結 (長い URL / パスが行折り返しで千切れるのを防ぐ)
# 逆順は awk で行う (`tail -r` は BSD 拡張で GNU coreutils に無く、GNU tail が PATH 先頭に
# 来る環境では set -o pipefail 込みで popup が無言即死する)
captured=$(tmux capture-pane -p -J -S -1000 -t "$pane" \
  | awk '{a[NR]=$0} END{for(i=NR;i>=1;i--) print a[i]}')

# 断片抽出。カテゴリごとに色付きタグを付け、出現順 (=新しい順) を保って dedup する。
# - url:  http(s)://... (末尾の閉じ括弧・句読点は落とす)
# - path: / を含む語、または file:line 形式 (先頭の装飾記号は剥がす)
# - hash: 7-40 桁の16進 (コミットハッシュ想定。数字のみは除外)
# - word: 5 文字以上の英数語 (フォールバック。上位カテゴリと重複しても別タグなら残す)
list=$(printf '%s\n' "$captured" | awk '
  {
    line = $0
    while (match(line, /https?:\/\/[^[:space:]"'"'"'><)\]]+/)) {
      s = substr(line, RSTART, RLENGTH)
      sub(/[.,;:]+$/, "", s)
      print "\033[36murl \033[0m\t" s
      line = substr(line, RSTART + RLENGTH)
    }
  }
  {
    n = split($0, w, /[[:space:]]+/)
    for (i = 1; i <= n; i++) {
      t = w[i]
      gsub(/^[("'"'"'`\[<]+|[)"'"'"'`\]>.,;:]+$/, "", t)
      if (t == "") continue
      if (t ~ /^https?:\/\//) continue                    # url で拾済
      # 桁数は {7,40} 等の interval 指定でなく length() で判定する (mawk が interval
      # 未対応でリテラル解釈し、Linux CI で hash/word が 1 件も抽出されなくなる)
      if (t ~ /\// || t ~ /^[A-Za-z0-9_.-]+\.[a-z]+:[0-9]+$/)
        print "\033[33mpath\033[0m\t" t
      else if (t ~ /^[0-9a-f]+$/ && t ~ /[a-f]/ && length(t) >= 7 && length(t) <= 40)
        print "\033[35mhash\033[0m\t" t
      else if (t ~ /^[A-Za-z0-9_.-]+$/ && length(t) >= 5)
        print "\033[90mword\033[0m\t" t
    }
  }' | awk -F'\t' '!seen[$2]++')
[ -n "$list" ] || exit 0

out=$(printf '%s\n' "$list" \
  | fzf --ansi --reverse --border --delimiter='\t' --nth=2 \
        --prompt='extract> ' \
        --header='Enter: 貼り付け / C-y: コピー / C-o: 開く' \
        --expect=ctrl-y,ctrl-o) || exit 0

key=$(head -1 <<<"$out")
text=$(sed -n '2p' <<<"$out" | cut -f2)
[ -n "$text" ] || exit 0

case "$key" in
  ctrl-y) printf '%s' "$text" | pbcopy ;;
  ctrl-o)
    case "$text" in
      http://*|https://*) open "$text" ;;
      # -l -- : 抽出語が '-' 始まり (--flag / -p/path 等。word/path 正規表現は先頭 '-' を
      # 許すため頻出) でも send-keys のフラグに誤解釈させず literal 貼り付けする。無しだと
      # tmux が invalid flag で失敗し、popup -E は即閉じるため silent no-op になる。
      *) tmux send-keys -t "$pane" -l -- "${EDITOR:-nvim} ${text%%:*}" ;;
    esac ;;
  *) tmux send-keys -t "$pane" -l -- "$text" ;;
esac
