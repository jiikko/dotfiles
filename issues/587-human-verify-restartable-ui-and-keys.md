# 587 (human): restartable の見た目と R / Q の操作を実端末で確かめる

起票日: 2026-10-01
期限: 2026-10-08

## なぜ人が要るか

最下行と確認ダイアログの見た目、キー操作、端末の復元は、実端末 (tmux のペインを含む) でしか確かめられない。自動テストは model と View の文字列と
非 TTY の経路までで、bubbletea の inline 描画が実端末でどう見えるかは測っていない (issue 586)。

## 手順

1. 入れる: `go install github.com/jiikko/dotfiles/src/restartable@v0.0.0-20261001052920-3cf90b653dd4` (または dotfiles の checkout で `cd src/restartable && go build -o /tmp/restartable .`)
2. 試す: `restartable --build 'echo build; sleep 2' --stop-cmd 'exit 0' -- sh -c 'i=0; while :; do i=$((i+1)); echo "tick $i"; sleep 1; done'`
   (stop-cmd が成功しても子は自分で終わらないので「終了待ち」の状態を試せる)
3. 見る・押す:
   1. 最下行が `── [R] 再起動  [Q] 終了 ──── running (pid N)` の形で、ログ (tick) がその上に流れ、端末のスクロールバックに残る
   2. ビルド中 (最初の 2 秒) に R → 「ビルド中」と出て何も起きない
   3. R → 確認ダイアログが最下行の上に出る → n で閉じる / もう一度 R → y で「終了待ち (Esc で取り消し / Ctrl-C で強制終了)」になる → Esc で running に戻る
   4. Q → 確認 → y → 終了待ち → Esc で戻る
   5. 端末の幅を変える → 最下行の罫線が幅に合わせて伸縮する
   6. Ctrl-C → 子が止まって rc 130 で終わり、端末が元に戻る (カーソルが見える・入力がエコーされる)
   7. stop-cmd 無しで: `restartable -- sh -c 'trap "exit 0" TERM; while :; do echo t; sleep 1; done'` → R → y で再起動 (世代が上がる)、Q → y で終了
   8. 別の端末から同じディレクトリで `restartable status` / `restartable restart` → 状態が返り、再起動する

## 関連

- [586](586-feat-restartable-foreground-restart-runner.md) — restartable 本体

## 期待と違ったら

- 見た目・キー・端末の復元の問題は dotfiles issue 586 を open のまま、症状をそこに書く

## 結果 (2026-10-01、Claude が機械で確かめた)

ユーザーに「お前では確認できないの？」と聞かれ、手順を pty を操作するスクリプト (python の pty。使い捨て) と、隔離した tmux サーバ (`-L` の専用ソケット) で流した。
起票の時点で「機械で測れないか」を詰めていなかった (人に回すのが早すぎた)。

- pty (80x24) で 23 段すべて通った: ビルドのログ / ビルド中の R で「ビルド中」/ 最下行 `[R] 再起動  [Q] 終了 … running (pid N)` / ログが上に流れる /
  R → 確認 (再起動しますか) → n で閉じて同じ pid / R → y で「終了待ち」(stop-cmd 成功、子は生きたまま、status は stopping) → Esc で running (同じ pid) /
  Q → 確認 (終了しますか) → y → Esc で戻る / 幅を 120 と 60 にすると最下行の表示幅がちょうど 120 / 60 / Ctrl-C で rc 130・子が残らない・端末が canonical + echo に戻る・カーソルを表示し直す /
  stop-cmd なしで R → y で再起動 (世代 1→2、pid が変わる) / 別のプロセスから status (JSON) と restart (世代が上がる) / Q → y で rc 0 / 終了後の status は rc 2
- 隔離した tmux (ユーザーの `~/.tmux.conf` を読む / 素の設定、の 2 通り、80 桁): 最下行は 80 桁の 1 行に収まり (罫線 `─` は 1 桁で描かれ折り返さない)、R で確認の板が最下行の上に出る
- 機械で確かめられないのは「見た目が好みか」だけ。これは obaket の 1008 (実アプリで make dev-fg-loop を使う確認) で同じ画面を見るので、ここは done にする
