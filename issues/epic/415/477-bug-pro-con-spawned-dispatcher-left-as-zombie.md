# 477 (bug): 画面が起こした dispatcher は、抜けても画面が閉じるまでゾンビで残る (起こし直しが続くと溜まり続ける)

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

リソースリークの監査 (479) で見つけた。画面は dispatcher を `Start` して `Process.Release()` するだけで、`Wait` しない。
dispatcher が抜けても、親の画面が生きている間は刈り取られず、ゾンビ (`Z`) としてプロセス表に残る。
画面の keeper は、dispatcher が居なくなるたびに起こし直す。なので、dispatcher が落ち続ける形では 1 回起こすごとに 1 つ溜まる。

## 詳細

- 該当: `src/pro-con/main.go` の `spawnDispatcher` (`cmd.Start()` → `cmd.Process.Release()`)。呼び元は `startDispatcherIfIdle`:
  画面を開いたとき (`wireLive`) と、keeper (`live.Backend.keep`。`live.Interval` = 3 秒ごと、dispatcher の Tick が `keepAfter` = 10 秒より古いとき)。
  非テストの Go に SIGCHLD の扱い・`Wait4` は無い (grep で 0 件)
- 発火条件: 画面が開いている間に、その画面が起こした dispatcher が抜ける。1 回ごとに 1 つ残る。溜まり続ける形は、既に分かっている「dispatcher が抜けて keeper が起こし直す」繰り返し:
  - 記録の JSON が壊れて Tick が失敗し続ける (460 の観点 1 の P2)。起こし直すたびに最初の Tick で抜けるので、およそ 12 秒ごとに 1 つ (約 300 / 時。間隔はコードを読んで出した値で、測っていない)
  - 画面の印を数えられず、1 分ごとに止めて抜ける (439 の 2 つ目)。約 60 / 時
  - 手の `pro-con dispatcher --stop` を keeper が取り消す (459)。1 回ごとに 1 つ
- ctrl+r (ライブアップグレード) は `syscall.Exec` で pid を保つので、それまでのゾンビは新しい版の子のまま残る。新しい版もそれを Wait しない
- 漏れたとき: ゾンビは fd もメモリも持たないが、プロセス表の枠を 1 つずつ使う。この Mac の上限は `kern.maxprocperuid` = 10666 (2026-09-25 に sysctl で確認)。
  300 / 時なら 35 時間ほどで、そのユーザーの fork が全部失敗する (シェル・tmux・claude も起動できない)。ゾンビは画面を閉じれば launchd が刈り取る
- 根拠 (実験): module を一時ディレクトリへ写し、`package main` のテストから本物の `spawnDispatcher` を 5 回呼んだ。
  子はテストの二進 (TestMain で即 `os.Exit(1)`) にした。2 秒後の `ps -o pid,ppid,stat` で、テストのプロセスの子に `ZN` が 5 つあった
  (`zombies=5`。テストを抜けると消えた)。落ち続ける繰り返しの中でゾンビが溜まることは、組み合わせで確かめていない
- 意図の反証: コメントは「画面とは別のプロセスグループで起動する (画面を閉じても・ctrl+c が届いても道連れにしない)」。これは Setpgid の説明で、
  刈り取らない理由は書かれていない。`Release` は Unix では pid の扱いを捨てるだけで、刈り取りはしない (プローブのとおり)。
  `stopInChild` は goroutine で `cmd.Wait()` しているので、Wait しないのは spawn の側だけ

## 対応方針 (候補)

- 刈り取る: `go func() { _ = cmd.Wait() }()`。ただし ctrl+r の exec をまたぐと、その goroutine は消える。exec の前に起こした dispatcher は、また刈り取られなくなる
- exec をまたいでも残さない形: 中継のプロセスを挟んで二重に fork する (中継はすぐ抜けて画面が Wait する。dispatcher は launchd の子になる)
- 🚨 SIGCHLD を無視 (SA_NOCLDWAIT) にしない: 画面の他の子 (pbcopy・attach・upgrade の shim) の `Wait` が ECHILD で失敗する
- 回帰の検査: `spawnDispatcher` の後に子が抜けても、親の子に `Z` が残らないことをテストで固定する (上のプローブの形)

## 進捗

- [x] 実装 (2026-09-25): 候補 2 の二重 fork。`spawnDispatcher` は内部用のサブコマンド `pro-con spawn-detached <args>` (中継) を起こして
  `Run` で待つだけにした。中継は自分のバイナリを別のプロセスグループで `Start` し、待たずに抜ける。dispatcher は launchd の子になるので、
  抜けたら launchd が刈り取る。画面は dispatcher の pid を持たないので、ctrl+r の exec をまたいでも残らない (goroutine で Wait する形はここで漏れる)
  - 中継の起動の失敗 (exe が無い等) は中継が dispatcher.log へ書き、画面へは「中継が失敗した (様子は dispatcher.log)」で返す
- [x] 回帰の検査: `TestSpawnedDispatcherIsNotLeftAsZombie` (main_test.go)。本物の `spawnDispatcher` を呼び、子はテストの二進 (TestMain が
  中継は本物を走らせ、`dispatcher` は pid を書いて即抜ける偽物にする。本物の dispatcher・状態の置き場には触らない)。抜けた偽の dispatcher の pid が
  `ps` で居なくなるのを待つ。画面 (テスト) の子の `Z` で見えたら落ちる
  - 変異 (`bin/mutate-verify`。旧実装の `Start` → `Release` へ戻す) で red: `抜けた dispatcher (pid 41932) が画面の子のゾンビで残った: ppid=41911 stat=ZN`
  - 検査が守らない形: goroutine で `cmd.Wait()` する形は、この検査では green になる (exec をまたぐ漏れは検査していない)
- [x] `make test` (repo root) rc=0 (2026-09-25、テストの係。6m11s。ログに FAIL 0 件)
- 直す前の版の画面から ctrl+r で上げた場合、それまでに溜まったゾンビは新しい版でも刈り取られない (画面を閉じれば消える)
- 2026-09-25 PM のレビュー: 差し戻した (二重 fork は終了の保証・keeper の起こし直し・presence に絡む自作の安全機構の変更なので、取り込む前に Opus の敵対的レビューを通す)
