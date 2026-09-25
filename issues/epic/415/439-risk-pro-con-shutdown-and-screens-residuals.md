# 439 (risk): pro-con の終了・複数の画面で、記録だけにした指摘と未確認のこと

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

427 の「終了のとき、pro-con が起動した claude が確実に止まる」と「複数の画面を同じ置き場で開いてよい」は、敵対的レビューを 7 周回して
P1 / P2 を直した。直さずに記録だけにした指摘と、確かめていない前提をここに集める (再現したら直す。trigger を各項に書く)。

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

## 未確認

- attach で開いた (人が直接話している) session も、pro-con の終了で止める。照合は session id だけで、pid は見ない
  (同じ session id で pid が違うのは Claude Code の自動の再開 = pro-con の PG。外の shell の `--resume` は別の session id = 427 の 3f の実測)。
  止めた session を attach で同じ id のまま起こし直せるかは確かめていない
- `claude agents --json` / `--all` の出力の形は 2.1.282 で測った。版が上がって state の値が変わると、止まったかの確かめが「止まっていない」に倒れ続ける (止め直しを続ける側)
