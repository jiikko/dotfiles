# 587 (human): restartable の見た目と R / Q の操作を実端末で確かめる

起票日: 2026-10-01
期限: 2026-10-08

## なぜ人が要るか

最下行と確認ダイアログの見た目、キー操作、端末の復元は、実端末 (tmux のペインを含む) でしか確かめられない。自動テストは model と View の文字列と
非 TTY の経路までで、bubbletea の inline 描画が実端末でどう見えるかは測っていない (issue 586)。

## 手順

1. 入れる: `go install github.com/jiikko/dotfiles/src/restartable@<版>` (または dotfiles の checkout で `cd src/restartable && go build -o /tmp/restartable .`)
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
