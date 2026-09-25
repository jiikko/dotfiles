# 439 (risk): pro-con の終了・複数の画面で、記録だけにした指摘と未確認のこと

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

427 の「終了のとき、pro-con が起動した claude が確実に止まる」と「複数の画面を同じ置き場で開いてよい」は、この 2 つの節の分で
敵対的レビュー (Opus) を 8 周回して P1 / P2 を直した (427 のほかの節の周回とは別に数えている)。直さずに記録だけにした指摘と、確かめていない前提をここに集める (再現したら直す。trigger を各項に書く)。

## 記録のみ (起きる条件が限られる)

- ctrl+r (ライブアップグレード) の exec の間は画面の印 (presence の flock) が外れる。その一瞬に別の画面が閉じると、最後の画面と判定されて止める。
  trigger: ctrl+r の後に PG が止まっていた報告。直し方の案: 印の fd の CLOEXEC を外して exec をまたいで持ち越す
- 画面の印を数えられない状態が続く (sudo で作った root 所有の `screens/*.lock` が残る等) と、画面が起こした dispatcher が 1 分ごとに止めて抜け、
  画面が起こし直して再開する (枠を使う)。trigger: dispatcher.log に「開いている画面が 1m0s 無い」が繰り返し出る
- 画面の印を置けなかった画面も、開いたときに 1 回は dispatcher を起こす (起動の経路 main.go の startDispatcherIfIdle。keep の防ぎは効かない)
- 前の `--stop` が止め直しを続けている間に次の `--stop` が走ると、stop.log へ並行して書き、「今回の子の分だけ出す」が崩れる
- `--stop` の子は SIGTERM / SIGHUP / SIGINT を無視して止め終えるが、SIGKILL では途中で止まる
- SIGTERM を受けた画面が起こした dispatcher は 1 回だけ止めて抜ける。止めきれないと rc=1 で抜け、PG が残る (画面を開けば keeper が次の dispatcher を起こして扱う)
- 同じ repo を 2 つの状態の置き場 (本物と e2e の同時利用など) で動かすと、PG の worktree の名前 (`pc-c-001`) がぶつかる
- テストの係で、コマンドが自分で作ったプロセスグループの子孫は止められない (427 の段階 4)
- 再開の取り込み (dispatcher.go の adopt の「再開」) は、同じ worktree で印の後に始まった外の bg session も取り込む (既存の形)。
  終了のときはその session も止め直しと名指しの対象になる。trigger: pro-con の worktree で手で `claude --bg` を立てる運用が出てきたら
- 画面が起こした dispatcher が SIGTERM で止めきれずに抜ける rc=1 は、誰も読まない (起こした画面は Release している)。残りは dispatcher.log にだけ出る。
  止めた結果を読む前に、別の `--stop` が結果のファイルを消すと、止め終えていても rc=1 になる

## 未確認

- attach で開いた (人が直接話している) session も、pro-con の終了で止める。照合は session id だけで、pid は見ない
  (同じ session id で pid が違うのは Claude Code の自動の再開 = pro-con の PG。外の shell の `--resume` は別の session id = 427 の 3f の実測)。
  止めた session を attach で同じ id のまま起こし直せるかは確かめていない
- claude (native binary) が起動時にシグナルの扱いを戻すかは確かめていない (`--stop` は Notify で受けるので、子は既定の扱いで起動する。
  Go の signal.Ignore なら子に引き継がれることは実測した)
- `claude agents --json` / `--all` の出力の形は 2.1.282 で測った。止まったかは pid と state で決める (pid 無し かつ working でない = 01dbb3b0。
  dogfooding で、終えた session が stop 後も state: done のままだと分かって直した)。版が上がって pid が出なくなる / working の綴りが変わると、
  止まったかの確かめがどちらかへ倒れる (未確認)
