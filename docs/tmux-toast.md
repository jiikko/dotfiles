# tmux-toast — フォーカスを奪わない toast 通知

`bin/tmux-toast` は tmux の右下に数秒だけ通知を浮かべるコマンド。**フォーカスを奪わない**
(表示中も手元の pane にそのまま打鍵できる) のが display-popup との違い。2026-07-19 実装。

## 使い方 (pane 内のシェルから叩くだけ)

```bash
tmux-toast "ビルド完了 ✔"           # 2 秒表示 (デフォルト)
tmux-toast -d 5 "デプロイ完了"       # 5 秒表示
tmux-toast -f 16 -b 208 "警告っぽい色"  # 前景/背景を 256 色番号で指定
```

- tmux の中で実行することが前提 (`$TMUX` 必須。外で叩くとエラー)
- 長時間コマンドの完了通知に: `make build; tmux-toast "build done ($?)"`
- hook / スクリプトからは `run-shell -b` 経由で呼ぶ (`-b` で tmux をブロックしない)

## 組み込み済みの通知

- **pane 分割時**: `_tmux.conf` の `after-split-window` hook が「🪟 pane を分割しました」を出す

## 仕組みと経緯

display-popup はモーダル (表示中のキー入力を必ず奪う) で、非ブロック化は本家で却下済み
(tmux/tmux PR #4379 → floating panes に置き換えて close)。そこで:

1. **本命: floating pane** — tmux 3.7 系に入っている `new-pane -X/-Y` (任意座標に浮く pane) を
   `-d` (フォーカス非奪取) 付きで使う。メッセージの表示セル幅 (東アジア文字=2セル) から
   右下座標を計算し、pane 内で `sleep duration` → 終了と同時に自動で消える。点滅しない
2. **fallback: tty 直描画** — floating 非対応の古い tmux では、クライアント端末の tty へ
   直接エスケープシーケンスで描画する。tmux の再描画に消されるため 0.05 秒周期で描き直し、
   終了時に `refresh-client` で tmux に上書きさせて消す。tmux の再描画との競合で
   点滅がわずかに残る (原理的に消せない)

## ⚠️ 罠: hook から呼ぶと無限増殖する (再入ガードが抑止している)

toast 用の `new-pane` 自体が `after-split-window` hook を**再発火する** (tmux 3.7b 実測)。
そのため分割 hook から素朴に呼ぶと「toast が toast を生む」無限増殖になる
(実測で pane 60 個超)。抑止は tmux-toast 側の再入ガードが担う:

- 直近 2 秒以内に toast 済み (`@tmux_toast_last_epoch`) なら黙って skip
- 副作用として 2 秒以内の連続通知は 1 つに間引かれる (通知 UI としては許容)
- **hook 側 (`_tmux.conf`) に増殖抑止の条件分岐を足さないこと** (二重管理になる。
  この契約は conf 側にもコメントしてある)

## テスト

`tests/tmux/test_tmux_toast.sh` (PATH stub 方式、実 tmux に触れない) が
再入ガード / floating の引数 (−d と座標) / fallback の描画と消去 / 異常系を固定している。
