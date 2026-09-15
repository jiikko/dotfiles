#!/usr/bin/env bash
# shellcheck disable=SC2034  # TT_FLOAT_* は呼び出し側 (tmux-toast / tmux_agent_panel.sh) が
#   読む出力変数。このファイル自身では使わないので、ファイル単位で宣言する
#   (行内 `;` 区切りの代入には、直前行の disable が最初の 1 つにしか効かないため)
# tmux_float_geometry.sh — floating pane (tmux 3.7+ の new-pane -x/-y/-X/-Y) の
# ジオメトリを決める共通ロジック。bin/tmux-toast と scripts/tmux_agent_panel.sh が使う。
#
# 🚨 tmux は「幅 == window 幅」も「高さ == window 高さ」も受理しない
#    (rc=1 / "size or position too large"。幅は 2026-08-21、高さは 2026-09-15 に実測)。
#    1 引いて渡すのが唯一の防御で、失敗は呼び出し側で無音になりやすい
#    (toast は `|| exit 0`、panel は `|| return 1` で黙って縮退する) ため気づけない。
#
# 🚨 この計算を呼び出し側へコピーしないこと。2 箇所に分かれていた間、panel 側だけが
#    `-gt` + `w=$win_w` で「幅 == window 幅」を許しており、**幅 150 以下の window では
#    panel が一度も出なかった** (issue 377 の敵対レビューで発覚。toast 側は同じ罠を
#    2026-08-21 に踏んで直していたが、その知識が panel には伝わっていなかった)。
#
# tt_float_geom <win_w> <win_h> <want_w> <want_h> <anchor>
#   anchor: top-right (右上に貼る) | bottom-right (右下に貼る)
# 結果は TT_FLOAT_W / TT_FLOAT_H / TT_FLOAT_X / TT_FLOAT_Y へ入れる
#   (行ごとの $( ) = fork を避けるための REPLY 返し。panel の描画ループは 2 秒周期で回る)
# 戻り値: 入力に非数値・空があれば 1 (呼び出し側は通知/パネルを諦めて縮退する)
tt_float_geom() {
  local win_w="$1" win_h="$2" w="$3" h="$4" anchor="$5" x y v
  # 🚨 文字種の判定は範囲式 [0-9] でなく明示列挙で書く (ja_JP.UTF-8 では全角数字が
  #    [0-9] を通り、後段の算術が落ちる。rules/shell-numeric-gate-explicit-digits.md)
  for v in "$win_w" "$win_h" "$w" "$h"; do
    case "$v" in ''|*[!0123456789]*) return 1 ;; esac
  done
  [ "$w" -ge "$win_w" ] && w=$((win_w - 1))
  [ "$w" -lt 1 ] && w=1
  [ "$h" -ge "$win_h" ] && h=$((win_h - 1))
  [ "$h" -lt 1 ] && h=1
  x=$((win_w - w))
  case "$anchor" in
    bottom-right) y=$((win_h - h)) ;;
    *)            y=0 ;;
  esac
  [ "$x" -lt 0 ] && x=0
  [ "$y" -lt 0 ] && y=0
  TT_FLOAT_W="$w"; TT_FLOAT_H="$h"; TT_FLOAT_X="$x"; TT_FLOAT_Y="$y"
  return 0   # 🚨 直前の [ ] の rc を関数の rc にしないこと (呼び出し側は || で縮退する)
}
