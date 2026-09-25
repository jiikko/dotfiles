# 479 (research): pro-con のリソースリークの監査 (2026-09-25)

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

pro-con のカード C-014 の依頼。pro-con (`src/pro-con`) のリソースリークを「壊れている前提で」探した。コードは直していない。
対象は master e34e4b3c。見た観点は goroutine / 子プロセス / fd・socket・lock / timer・ticker / bubbletea の Cmd / 長く動く dispatcher のメモリとファイル。
既知のもの (460・439・445・457〜469) は除いた。

- 実験は、module (`src/pro-con` と replace 先) を `mktemp -d` へ写し、その中の go test で行った。本物の pro-con と claude の session には触っていない
  (本物に対しては ps・lsof・ファイルの大きさを読むだけ。claude は偽のスクリプトに差し替え、本物は起動していない)
- 走査した母集合 (非テストの Go。機械で数えた): 53 ファイル。goroutine を起こすもの 7 / `exec.Command` 11 / timer・`time.After` 7 /
  Open・Create・Listen・Dial 10 / `tea.Cmd` を返すもの 11

## 全数勘定: 生存 6 (issue 2 / 記録 4) / 却下 4

### issue にしたもの

- P2 → [477](477-bug-pro-con-spawned-dispatcher-left-as-zombie.md): 画面が起こした dispatcher は Wait されず、抜けるとゾンビで残る。
  keeper の起こし直しが続くと溜まり続ける (プローブで 5/5 がゾンビになった)
- P3 → [478](478-perf-pro-con-cards-json-grows-and-is-reread-every-tick.md): cards.json は終えたカードも持ち続け、空の Tick でも 8 回読み直す
  (実測: 750 枚で Load 1 回 17ms)

### 記録のみ (発火条件を示せない・影響が小さい)

- **要約の係の `claude -p` は WaitDelay も Setpgid も無い** (`dispatcher/runner.go` の `HaikuSummarize`)。stdout・stderr が bytes.Buffer (pipe) なので、
  claude の子孫が pipe を握ったままだと、時間切れで claude を kill しても `Wait` がその子孫が抜けるまで戻らない。
  その間 `execute` は `job.done` を送れず、`d.active` が残って、テストの係の列が止まる。終了のときの `cancelRun` は 10 秒で諦めて `d.active` を空にするので、
  次の Tick が 2 本目の実行を始めうる。
  - 実測 (偽の claude = stdout を握る `sleep 8` を残して自分は `sleep 60`、期限 1 秒): `HaikuSummarize` は 8.2 秒後に戻った。WaitDelay 1 秒のある `runClaude` は 2 秒で戻った
  - 未確認の所: 本物の claude が pipe を握る子孫を残すか。常駐の claude daemon (`claude daemon run`。ppid 1) は fd 0/1 が /dev/null、2 がファイルで、pipe を握っていなかった (lsof)。
    `usage.go` の `usageCmd` には「子孫が stdout を握ったままでも timeout で戻る (launcher と同じ)」とある。WaitDelay は非テストの Go で 6 か所に付いている
    (launcher / agents / usage / upgrade / pbcopy / テストの係の ExecRunner。grep で数えた)。claude を呼ぶ所で漏れているのは要約だけ
  - あわせて: claude を呼ぶ 4 つ (`runClaude` / `execAgents` / `usageCmd` / `HaikuSummarize`) は、どれも時間切れで claude 本体だけを kill する (Setpgid が無い)。
    子孫は残る (上のプローブの孫は自分で抜けるまで生きていた)
  - trigger: テストの係が「実行中」のまま動かない / dispatcher が抜けた後に `claude -p` の子が ppid 1 で残っている。直す方向: `cmd.WaitDelay` を足す
    (ほかの claude の呼び出しと揃える)。子孫まで止めるなら Setpgid + グループへの kill
- **dispatcher.log と stop.log は足すだけで、回さない** (`main.go` の `spawnDispatcher` / `stopInChild` の O_APPEND)。中身は events.jsonl と同じ出来事の行で、
  events.jsonl の側には `MaxBytes` の回しがある。増える速さは events.jsonl と同じで、dogfooding の 1 日で約 17KB。小さいので記録に留める。
  trigger: どちらかが 10MB を超えたら issue にする (「止めきれない (止め直す)」は 10 秒ごとに 1 行足すので、止め直しが続くと速くなる)
- **ctrl+r の後に画面が kill -9 で落ちると、resume-*.json が残る** (`main.go` の `removeResume` は正常な終了のときだけ)。tea が失敗したときに残すのは
  意図どおり (直して引き継ぐため。コメントあり)。落ちるたびに 200B ほどのファイルが 1 つ残る。trigger: 置き場の resume-*.json が 10 を超える
- **ExecRunner の実行の後始末 `Kill(-pgid, SIGKILL)` は、`Wait` で刈り取った後に撃つ** (`dispatcher/runner.go` の `ExecRunner.Run` の defer)。
  グループに誰も残っていなければ、その pgid の番号は空いている。撃つまでの一瞬に別のプロセスが同じ番号のグループを作れば、そちらを撃つ。
  窓は数μ秒で pid の使い回しも順番なので、起きた例は無い。記録のみ

### 却下 (理由つき)

- 画面の transcript のキャッシュ (`live.Backend` の `cache` / `paths`) は消されずに増える。ただ量が小さい: 本物の 47 session
  (sessions.json 15 + sessions-retired.json 32) を画面と同じ入口 (`transcript`) で読んだところ、ヒープの増分は 340KB だった (1 session 約 7KB)。何日開いていても数 MB
- ctrl+r の exec が失敗して tea を作り直すと、前の Program の `waitChanged` の goroutine が 1 つ残り、次の知らせを 1 回取って消える。
  1 回の失敗につき 1 つで溜まらず、画面は 1 秒の tick で読み直すので見た目も変わらない
- `killStale` が exec・setsid の子孫を止められないこと (439 に既知) / runs/ と要約の transcript (460 に既知)
- cards.json が消えないこと自体は 438 の決定。費用の面だけを 478 にした

## 攻めたが指摘が出なかった範囲

- goroutine: `wake` (accept は Close で抜ける。sub の接続は Close で全部切る。Broadcast は書き込みの期限つき) / `watchDir` の購読は `waitFor` の cancel で抜ける /
  `live.Start` の 2 本は `Wait` が両方を待つ / `relay.Writer.Close` は wg を待つ / `stopInChild` は goroutine で Wait する (ctx が切れても子は刈り取られる) /
  `--stop` のシグナルの goroutine は os.Exit か、プロセスと一緒に終わる
- fd: lock・presence・relay の印は、どの失敗の経路でも Close している。eventlog・transcript・logTail は defer で Close。
  3 時間動いていた本物の `--view` 画面 (pid 67428) の fd は 15 本 (CHR 3・PIPE 4・REG 3・KQUEUE 2・unix 1・DIR 1)。増える種類は無かった
- timer: `NewTicker` は 2 か所とも defer で Stop。ループの中の `time.After` は go 1.25 の module なので、止めなくても GC される
- bubbletea: tick・upgradeTick は 1 本の鎖。frame は `framing` で二重に回さず、動くものが無ければ止まる。裏の処理は `child` で数え、exec の前に 0 を待つ
- dispatcher のメモリ: `notified` と `stopFrom` は、待ちを抜けたとき・止め終えたときに消している
- 測れなかった所: 長く動いた本物の dispatcher の fd。監査の途中で pro-con の終了があり、dispatcher (pid 55138) が入れ替わった
