# 576 (perf): 役 (PM・取り込みの係) が生きている間、dispatcher が 3 秒ごとに `claude agents --json` を起こし続ける

起票日: 2026-09-28

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

[559](done/559-perf-pro-con-dispatcher-polls-claude-agents-every-tick.md) で、暇な間 (`dispatcher/idle.go` の `quiet` が真) に
session の一覧を取る間隔を 3 秒から 30 秒 (`defaultQuietListEvery`) へ延ばした。ただし `quiet` は、**生きている役
(`pm.Session != "" && !pm.Stopped`) が 1 つでも居ると偽**を返す。PM を常駐させる使い方では、カードが全部完了していても
Tick (`dispatchercmd.go` の `dispatcherInterval` = 3 秒) のたびに `claude agents --json` (`agents/agents.go` の `execAgents`)
が起き、559 の対策が効かない。1 分 20 回・1 日約 2.9 万回・CPU 約 1 時間/日 (いずれも 559 の実測値。1 回 0.14 秒 / CPU 0.13 秒、macOS 27)。

559 は、生きている役を暇の妨げから外す案を敵対的レビューの 2 周目で戻している (理由は `quiet` のコメント)。一覧で見張っているのは次の 2 つ:

1. **役の入力待ち (人への知らせ)**: `dispatcher/role.go` の `tellRole` が一覧の session の様子を見て知らせる。間引くと最長 30 秒遅れる
2. **落ちた時刻 (`DeadSince`)**: 終了の処理 (`stopRole`) は、Claude Code の自動の再開 (約 25 秒) を待つかどうかをこの時刻で決める。
   間引いている間に役が落ちると `DeadSince` が付かず、`stopRole` が待たずに抜け、そのあと再開した PM が残りうる

## 対応方針 (案。どれも未検証)

一覧を取らずに上の 2 つを保つ仕組みを先に用意し、その後で「生きている役」を `quiet` の妨げから外す。

- 入力待ち: 一覧以外に知らせる経路が無いか調べる (Claude Code の hook (Notification / Stop) で状態をファイルへ書く、など)。
  `claude agents --json` の様子との対応は実物で測ってから決める (`measure-external-cli-streams-separately.md`)
- `DeadSince`: 役の pid の生死を一覧なしで見る案。🚨 `kill(pid, 0)` はゾンビでも pid の再利用でも成功するので、それだけでは判定しない
  (`mutation-verify-new-tests.md` の「生死を kill(pid, 0) で判定しない」)。起動時刻との突き合わせなども含めて検討する
- 代わりの案: 役が生きている間は 3 秒と 30 秒の間の間隔 (例 10 秒) にする。入力待ちの遅れと、`DeadSince` の取りこぼし
  (約 25 秒の再開待ちの窓より短い間隔が要る) を受け入れられるかで決める
- 🚨 一覧の遅れで壊れるものを先に列挙してから延ばす (559 と同じ。`survey-receiver-guards-before-passing-new-values.md`)。
  画面が dispatcher の一覧を信じる鮮度 (`store.Seen` の Keep / `live/live.go` の `seenFresh`) も合わせて見直す

## 観測 (2026-09-28、Claude Code 2.1.283、macOS 27)

haiku の bg session を 1 本 (`--permission-mode default`・`touch` を頼んで権限の確認で止める) 起こし、1 秒ごとに `claude agents --json` の行と
`~/.claude/{sessions,jobs,daemon}` の下で mtime / サイズが変わったファイルを並べた。

| 出来事 | `claude agents` | 変わったファイル |
|---|---|---|
| 起動 | busy / working | `jobs/<id>/state.json`・`sessions/<pid>.json`・`daemon/roster.json` |
| 権限の確認で止まる | waiting / blocked / `permission prompt` | 同じ秒に `jobs/<id>/state.json` と `sessions/<pid>.json` |
| SIGKILL | すぐ pid 無し (state は blocked のまま) | **約 12 秒のあいだ何も変わらない** |
| 自動の再開 (約 12 秒後) | 新しい pid で busy | `roster.json`・`jobs/<id>/state.json`・新しい `sessions/<pid>.json` |
| turn の終わり (別の session で測った) | busy → idle / done | 同じ秒に `jobs/<id>/state.json`・`timeline.jsonl`・`sessions/<pid>.json` |

→ 様子の変化 (入力待ちに入る・busy から idle) は `state.json` の変化で拾えるが、**落ちたのはファイルでは拾えない** (pid の生死で見る)。

## 設計

`claude agents` を様子の唯一の出典のまま残し、**「一覧を取り直す合図」だけ**を安いもの (stat と pid) で作る (一覧の中身を自前で読み直す近似は作らない)。

- `quiet` は、生きている役を次の条件を満たすときだけ暇の妨げから外す: 起動・再開の結果待ち (`Launching`) でない・前に一覧と照らしたとき
  生きていた (pid あり)・その session の id が記録の session と同じ・`JobsDir` がある。落ちた・再開待ち (`DeadSince`) の役は今までどおり毎 Tick 取る
- 一覧を取る直前に、見張る役ごとに `JobsDir/<id>/state.json` の (mtime, サイズ, 在るか) を控える (取る前に控える: 取っている間の変化を落とさない)
- 暇で間引く Tick でも、次のどれかなら取る: 30 秒経った / 控えた `state.json` の値が変わった / 役の pid が生きていない (`kill(pid, 0)` が失敗。
  EPERM も「別のユーザーのプロセスが pid を使っている」なので落ちた側に倒す)
- 一覧を取った Tick で、前に控えた `state.json` がその後も変わっていない (取る前と取った後の両方) のに役の status が変わっていたら、
  1 度だけ出来事 (suspect) にする (Claude Code の版で書く場所が変わると、合図が鳴らないまま最長 30 秒遅れる形に黙って落ちるため)。
  暇な間に限らず、どの取得でも見る (見張る前の忙しい間に気づける)
- 一覧を取った Tick で照らせなかった役 (取れない・途中で抜けた・記録を読めない) と、生きていない役 (pid 無し) は見張らない
  (`Tick` が見張りを外す。次の Tick で取り直して控え直す)

受け入れる遅れ:
- 合図が鳴らない変化 (上の suspect の形) は最長 30 秒
- pid の再利用 (落ちた PM の pid を別のプロセスが 3 秒以内に取る) では、落ちたのに気づくのが最長 30 秒遅れる。macOS の pid は順に振られるので、
  3 秒の窓で同じ番号が回ってくることは実質起きない (実測はしていない)

## 対象外

- 動いているカード (完了していないカード・起動の結果待ち) がある間の 3 秒ごとの取得。PG の起動の確かめと落ちた PG の検出に要る

## 受け入れ条件

- [ ] PM が生きていて、カードが全部完了している間の `claude agents --json` の回数が 1 分 20 回より大きく減る (本物の dispatcher で実測する)
- [ ] 役の入力待ちの知らせの遅れを、延ばす前と比べて実測して書く
- [x] 間引いている間に役が落ちても、`stopRole` が自動の再開を待ってから抜ける (変異で red を確かめたテスト): 落ちた次の Tick で一覧を取り DeadSince を付ける (`TestWatchedPMDeathListsAndStopsWatching`、変異 M2 で red)。`stopRole` は DeadSince で待つかを決める既存の経路のまま (変えていない)

## 関連

- [559](done/559-perf-pro-con-dispatcher-polls-claude-agents-every-tick.md): 暇な間の間引き。本 issue はその「再開の trigger」
- [502](done/502-perf-pro-con-agents-list-spawned-per-screen-and-dispatcher.md): 画面の側の呼び出し
- [500](../../done/500-bug-macos-kernel-zone-leak-from-tmux-clients.md): カーネルのメモリの漏れ。559 は `claude agents` の多さを容疑の 1 つに挙げていた

## 進捗

- 2026-09-28 起票。まだ着手していない
- 2026-09-28 反証レビュー (read-only のサブエージェント 1 本): 反証できず。`quiet` が生きている役で偽になることは
  `dispatcher/idle_test.go` のテーブル (「PM が生きている」の行) が既に固定している。`DeadSince` の取りこぼしは今は起きない
  (役が生きている間は毎 Tick 一覧を取るので)。間引いたときに起きる問題として書いている
- 2026-09-28 実装 (dotfiles-f9): `dispatcher/idle.go` (watchable / rolesMoved / pidAlive / markRoles / settleMarks)、
  `role.go` (roleRun に pid・job・mark・marked)、`dispatcher.go` (Alive / hintSuspected、Tick での見張りの解除、一覧の前に控える)
  - 設計レビュー (opus 1 本): P1 なし。採用 P2-1 (照らせなかった Tick で合図を使い切る → Tick で見張りを外す) / P3-2 (コメント)。
    却下 P3-3 (再開の直後に古い id を見張る): 再開の間は `pm.Launching` が立つので暇にならない。
    記録 P3-1 (入力待ちから抜けるときの書き込み): 権限の確認に答える操作は非対話で作れず未実測。busy → idle は書かれた (観測の表)
  - 敵対的レビュー 1 周目 (opus 1 本、壊す観点): 入力待ち・DeadSince・Shutdown・役の表示は壊せなかった。
    - 採用 P3-d: 止めた PM の行が pid 無しで一覧に残ると、pid 0 を見張って毎 Tick 取っていた → 生きている役だけ控えを持つ
    - 却下 P2-1 (忙しい間の取得でも suspect を出すと、turn の終わりで誤報が出る): turn の終わり (busy → idle) でも `state.json` が同じ秒に書かれた (観測の表)。
      合図が鳴らない形はどの取得で見ても同じ意味なので、暇な間に限らない (設計の文面を実装に合わせた)。
      再提起するなら、`state.json` が書かれないまま status が変わる操作を実測して添えること
    - 記録 P3-a (AskUserQuestion の入力待ちで書かれるか未実測) / P3-c (`kill(pid, 0)` はゾンビにも成功する。daemon が回収しないと最長 30 秒遅れる。起きるかは未確認)
  - 変異 (`bin/mutate-verify`。どれも想定のテストだけが red): M1 `state.json` の合図を外す / M2 pid の生死を外す / M3 `quiet` を 559 の形に戻す /
    M4 控えるのを一覧の取得の後へ / M5 照らせなかった役の見張りを外さない / M6 suspect の「合図が鳴っていない」を外す / M7 suspect の「1 度だけ」を外す /
    M9 生きていない役にも控えを持たせる。M8 (照らした session が控えたときと違っても控えを持つ) は緑のまま = 等価:
    入れ替わった session の `state.json` を古い控えと比べるので、次の Tick で必ず取り直す
  - 敵対的レビュー 2 周目 (opus 1 本、1 周目の修正と素通りの観点): 修正が新しく壊したものは無かった。素通りを 5 件採用して直した:
    `state.json` が無い役を見張らない条件 (`exists`) を守るテスト / 取っている間に status も変わる形で suspect を出さない assert /
    見張りを外すときに控えも捨てる (取得の失敗の後に古い控えと比べて suspect を出さない) / 同じ長さの書き換え (mtime だけ進む) のテスト /
    `pidAlive` の本物の経路 (`kill(pid, 0)`) のテスト。あわせて `watchable` の、`marked` が含意する条件を外した
  - 2 周目の後に見つけた fixture の穴: PM の session の `StartedAt` が 0 で、記録 (registry) に行が載らない経路 (`hasRow` 偽) しか通っていなかった。
    起動の後に始まった形に直し、`settle` で行が載ったことを前提として固定した (止めた PM のテストは載らないのが正しいので外す)
  - 変異 (本番の形の fixture で当て直した。M8 以外は想定のテストだけが red): 上の M1〜M9 に加えて M10 `exists` を外す / M11 suspect の取得の後の読み直しを外す /
    M12 控えを捨てるのと `s.held` の両方を外す (片方ずつなら、もう片方が防ぐ。2 ファイルにまたがるので使い捨て worktree で手で当てた) /
    M13 mtime を比べない / M14 `pid > 0` を外す
  - 3 周目は回さない: 2 周目の修正は判定ロジックを新設せず、どれも変異で直接確かめた (`adversarial-review-own-safeguards.md` §7 の例外)
  - `make test` rc=0 (1 回目は `tests/zshrc/test_dotfiles_check_result_ownership.sh` が並列の負荷で落ちた。単独では 3 回とも通る。この変更は zsh を触らない)

## 残タスク

- [ ] 受け入れ条件 1・2 の実測 (本物の dispatcher で、PM が生きていてカードが全部完了の間の `claude agents` の回数と、入力待ちの知らせの遅れ)。
  測り方: dispatcher は一覧を取れるたびに `~/.local/state/pro-con/live/seen.json` の `at` を書き直す (`dispatcher/doing.go` の `publishSeen`)。
  PM を生かしたまま 5 分、`jq -r .at seen.json` を 1 秒ごとに読んで異なる値を数える (変更前は約 100、変更後は 10 前後のはず)。
  入力待ちの遅れは、PM を権限の確認で止め、`events.jsonl` の「PM が入力待ち」の行の時刻と `~/.claude/jobs/<id>/state.json` の mtime の差で見る。
  dispatcher は PM を本物で起こすので、自分 (Claude) からは測らない (ユーザーの環境の枠を使う)
- [ ] P3-a: AskUserQuestion の入力待ちで `state.json` が書かれるかを測る
- スコープ外: 人が `claude stop` した PM (DeadSince が付き Stopped は偽) は今までどおり毎 Tick 取る (設計レビューの P3-4。自動の再開は `state.json` を書くので、
  restartWait を過ぎたら `state.json` だけで見張る案がある)
