# 457 (bug): 記録に載らなかった PG は、終了でも閉じても止めず、「既に止まっていた」と書いて ok を返す

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

もろい作りの監査 (460) の P1。427 の不変条件「pro-con が起動した生きている session は終了で 0 本 / 止められなければ名指し」が、
PG の session が pro-con の記録 (sessions.json) に取り込まれなかったときに破れる。壊れ方に音が無い。

## 詳細

- 該当: `src/pro-con/dispatcher/shutdown.go` の `stopTarget` (`owned` が偽の枝: launchGrace を過ぎると `("", false)` = 止めるものが無い) /
  `ensureStopped` (記録 sessions.json と sessions-retired.json に在るものしか確かめない) / 447 の `stopClosed` (同じ `ensureStopped` を使う)
- 発火条件: 作業中のカードの PG が一覧には出ているのに、`register` に取り込まれないまま launchGrace (1 分) を過ぎる。取り込まれない形:
  claude の版が変わって一覧の `kind` が "background" でなくなる (427 に kind の警告として記録済み) / session の startedAt が LaunchedAt より前になる (時計が戻る)
- 壊れ方: 終了のとき履歴に「PG の session は既に止まっていた」、`Shutdown` は nil、stop-result は ok。生きている PG は止まらず、画面からも見えず見張られない。
  カードは分解済みへ戻るが、次の dispatcher で「再開できない: 前の session が pro-con の記録に無い」のまま進まない。閉じても (447) 止めない
- 根拠: 監査の係が一時ディレクトリの写しで、既存の偽物 (fakeLauncher / newDispatcher) を使うプローブで再現した (kind="bg" / 開始時刻 2 秒前の両方で stops=[] かつ err=nil)。
  PM がコードで `stopTarget` の枝を読んで確かめた (2026-09-25)

## 対応方針 (候補)

- カードが `Session` (起動で返った短い id) を持つなら、記録に無くても一覧でその id が生きていれば止める対象にする
- 止めないと決めた場合も「既に止まっていた」とは書かず、「記録に無い session が生きている (止めていない)」として名指しし、stop-result を ok にしない
- 取り込めない理由 (kind の違い・時刻) はそのたびに出来事 (444) に出す

## 進捗

- [x] 実装 (2026-09-25, pro-con カード C-010)。「対応方針」の 1〜3 つ目を全部採った
  - 判定の部品は `dispatcher/shutdown.go` の `unregistered` 1 つ。終了 (`stopTarget` → `strayPlan`) と、確かめる段 `ensureStopped`
    (→ `checkTargets`。447 の `stopClosed` もこれを通る) の両方がこれを呼ぶ
  - **止めてよい (proven)** のは、記録に行が無いカードについて、一覧の session の短い id がカードの `Session` (pro-con の起動・再開が返した id) で、
    cwd がそのカードの worktree (`<repo>/.claude/worktrees/pc-<card>`。`worktreePath` + `samePath`) そのものの session だけ。
    外の shell の claude は別の短い id を持ち PG の worktree の外で動くので当たらない。kind と開始時刻は見ない (取り込まれなかった理由そのもの)
  - **示せない生きている session** (短い id だけ一致して cwd が違う / 起動・再開の途中でカードの worktree に居る) は止めず、
    「記録に無い session が生きている。pro-con が起動したと示せないので止めていない」と名指しして `Shutdown` をエラーにする
    (stop-result は ok にならない)。閉じたときは closeStopWait の後に「止められない: …」と履歴に書く。「既に止まっていた」とは書かない
  - 記録にある session の照合 (`stopTarget` の記録の枝・`ensureStopped`・`stillAlive`) からも `Kind == "background"` を外した
    (session id で照らしているので kind は要らない。残すと claude の版で kind が変わったとき、記録にある PG も止まったと数える = 同じ壊れ方)
  - 取り込めなかった理由は、従来どおり `register` が kind / 開始時刻の `suspect` を出来事に出す (Tick ごと・終了の周ごと)。
    記録に無い PG を止めたときは「pro-con の記録に無かったが、カードの session の id と worktree が一致したので止めた」を出す
- [x] 確かめたこと (偽物の launcher / 一覧。本物の claude・state dir には触らない。`dispatcher/unregistered_test.go`)
  - 発火条件 2 つ (kind="bg" / startedAt が LaunchedAt の 2 秒前) それぞれで、終了・閉じたときに止める。直す前は 6 本が red (stops=[]・
    「既に止まっていた」)、直した後は green
  - 示せない session (cwd = repo root) は止めずに名指し / 記録に無い session が一覧で stopped なら「既に止まっていた」で ok
  - 変異 5 本 (`bin/mutate-verify`) がすべて想定のテストで red: proven の枝を殺す (終了 / 確かめる段) / kind の条件を戻す / 名指しを落とす / cwd の条件を外す
- [x] Opus の敵対的レビュー (プローブで再現したもの 4 件を直し、それぞれ変異で red を確かめた)
  - P0 再開の途中 (記録に前の行がある) で、再開が立てた新しい PG が kind の違いで取り込まれないと「既に止まっていた」で ok →
    Launching 中は記録の行があっても探し、worktree に居る対話でない session を名指しする (`TestShutdownNamesUnadoptedResume`)
  - P1 起動の途中のカードの worktree に人間の bg session が居ると、名指しし続けて終了が永久に ok にならない →
    起動は名前 (`-n pc-<card>`) の一致を要る (`TestShutdownIgnoresOtherSessionInWorktreeWhileLaunching`)
  - P1 kind の条件を外したので、記録と同じ session id の対話の session (短い id 無し) に `claude stop ""` を撃つ →
    止める相手は `stoppable` (短い id があり止まっていない) に揃えた (`TestEnsureStoppedSkipsSessionWithoutShortID`)
  - P1 カードの記録が読めないと、確かめる段が 1 本も止めずに抜ける (後退) → 記録の行は止め、記録に無い分は確かめられないと名指しする
    (`TestShutdownStopsOwnedWhenCardsUnreadable`)
  - P2 終了のカードの段が再開で入れ替わった前の行を見ずに「記録に無い」と書く → strayPlan に retired も渡す (再現は組んでいない)
- [x] origin/master に rebase して、並行の C-008 (451 削除) の `deleteTargets` が持っていた「記録に無い session」の独自の拾い方
  (短い id + kind + 開始時刻。cwd は見ない) を消し、`ensureStopped` の中の同じ `unregistered` に任せた (判定を 1 つにする)。
  451 のテストはすべて green。proven の「register と同じ根拠」の枝を殺す変異で `TestDeleteStopsUnregisteredSession` が red
- [ ] 残り
  - 起動・再開の途中 (Launching) で、claude が返した id がまだカードに無い形は、名指しするだけで止めない
    (起動は名前、再開は worktree しか手がかりが無く、pro-con が起動したと示せない)。再開の途中にカードの worktree で人間が bg の session を
    立てていると、その間は終了が名指しで失敗する (再開の新しい session の名前が引き継がれるかは未測定。引き継がれるなら名前で絞れる)
  - 記録に無いまま落ちた PG (pid 0・cwd が repo root になる = 427 の 3f) は、register が取り込む根拠 (kind が background・LaunchedAt 以降に開始) を
    満たせば止める (451 の `TestDeleteStopsUnregisteredSession` の形)。kind も時刻も崩れていて cwd も repo root なら、戻るまでは名指しだけになる
  - 記録の行はあるが、短い id が別の session id を指す形 (register が「別の session を指している」と知らせる) は、今も「一覧に無い = 止まっている」と読む
    (レビューの P2。`TestShutdownTouchesOnlyOwnSessions` がこの形を ok と決めているので変えていない。名指しに回すかは PM の判断)
  - 同じ repo を 2 つの状態の置き場で使うと worktree のパスがぶつかる (カード ID が同じ)。この変更の前からの形で未確認
  - 止まったかの判定 `Session.Stopped()` は 466 が扱う (ここではその 1 か所を使うだけにした)
- [x] make test (`pro-con card run`、ee0c531a): rc=2。Go のテストは pro-con (-race) を含め全部 ok。落ちたのは
  `tests/tmux/test_log_kill_command.sh` の 1 本だけ (「kill-server で直前保存が quiet 起動される」: 期待した `save quiet` の呼び出しが無い)。
  この変更は pro-con の Go だけで tmux には触っていない (test と対象のスクリプトの最後の変更は 2026-09-05)。原因は調べていない。
  テストの係の環境に左右されている疑いがあるが未確認

## 関連

- 460 (監査の記録) / 427 の終了の保証 / 447 (閉じたら止める)
- 2026-09-25 PM のレビュー: 差し戻しなし。止める対象を広げる所 (`unregistered`) は、pro-con が起動で受け取った短い id が一致し、かつ worktree の場所か
  「bg・最後の起動より後に始まった」も一致したものだけを止め、名前だけの手がかりは止めずに名指しする形で、外の session に触らないのを読んで確かめた。
  make test の赤 (`tests/tmux/test_log_kill_command.sh`) は、単独では master でも C-010 のブランチでも 3/3 ずつ緑 (偽の tmux を使う検査。ロードアベレージ 25 の時間帯に落ちた = 471)。
  取り込んだ tree で pro-con の `make lint` 0 件・`go test -race` 15 パッケージ ok → master へ (376472e5 まで)。カード C-010 を閉じた

## 残りの進め方 (2026-09-27、PM。pro-con カード C-009)

- **PM の判断: 「記録の行はあるが、短い id が別の session id を指す形」は名指しに回す** (「一覧に無い = 止まっている」と読んで ok にしない)。
  427 の不変条件「止められなければ名指し」に合わせる。`TestShutdownTouchesOnlyOwnSessions` がこの形を ok と決めている部分は、
  この判断に合わせて直す (外の session に触らない、は変えない)
- 「再開の新しい session の名前が引き継がれるか」は測れるなら測り、引き継がれるなら再開の途中の名指しを名前で絞る (人間の bg session で終了が失敗し続ける形を減らす)
- それ以外の残り (kind も時刻も崩れて cwd も repo root の落ちた PG / 2 つの状態の置き場で worktree がぶつかる) は、直せる根拠が無ければ「未確認のリスク」として残してよい
- 並行するカードとの衝突: C-003 (555。`shutdown.go` の止め直しの記録) は完了済み。C-006 (552。wtclean) / C-007 (557。dispatcher-state と一覧) とは重ならない

## 進捗 (2026-09-27、pro-con カード C-009)

- [x] **短い id が別の session を指す形を名指しに回した** (PM の判断どおり)。`unregistered` の「記録の行がある」枝で、カードの短い id が
  記録に無い別の session id を指して生きていれば proven=false で返す。終了 (`stopTarget` の記録の枝の末尾 → `strayPlan`) と
  確かめる段 (`ensureStopped` → `checkTargets`。閉じる 447・削除 451 も通る) の両方がこれで名指しし、止めない (外の session に触らない、は変えない)
  - 自動の再開を待つ間 (DeadSince から restartWait 以内) は従来どおり待ちを先にする (記録の session が戻れば止める)
  - `gone` (Tick の消えた PG) は従来どおり true を返す (再開は `prepare` が「短い id が今は別の session を指している」で拒むので、その session には触らない)。コメントだけ直した
  - `TestShutdownTouchesOnlyOwnSessions` の後半をこの判断に合わせて直した (stop は 0 本のまま、Shutdown はエラーで `C-001 (id-pc-c-001` を名指し、履歴に「既に止まっていた」を書かない)。
    閉じたときの形は `TestCloseNamesShortIDPointingElsewhere`
- [x] **再開の名前は引き継がれる (測定済み)**: 488 (2026-09-26、C-045) で `--bg --resume` が `-n` を受けて名前がそろうことを A-B で実測し、
  pro-con の再開は PG に `pc-<card>`・役に worktree の名前を渡している (`launcher.go` の `resumeArgs`)。新たには測っていない
  - これで再開の途中の名指し (`unregistered` の loose) を、起動と同じく worktree + 名前 (`pc-<card>`) に絞った (前は worktree に居る対話でない session すべて)
  - 🚨 同じ仕組みの穴が取り込みにもあった: 結果の分からない再開を取り込む `adopt` (カード: `dispatcher.go` / 役: `role.go`) が、
    記録の作業ディレクトリに居て印の後に始まった bg の session を**名前を見ずに**取り込んでいた = PG / 役の worktree で人間が立てた bg の session を
    カードの session にし、終了で止める。どちらも名前の一致を要るようにした (`TestFailedResumeAdoptsNewSessionByCwd` の前半 / `TestShutdownNamesUnadoptedResume` / `TestPMResumeAdoptNeedsName`)
- [x] 確かめたこと: `go test ./dispatcher/` ok・`go vet` / `gofmt` 0 件。変異 5 本 (`bin/mutate-verify`) がすべて想定のテストで red:
  短い id が別の session を指す名指しを殺す (`TestCloseNamesShortIDPointingElsewhere`) / 終了のカードの段で strayPlan を見ない (`TestShutdownTouchesOnlyOwnSessions`) /
  カードの adopt の名前を外す (`TestFailedResumeAdoptsNewSessionByCwd`) / 役の adopt の名前を外す (`TestPMResumeAdoptNeedsName`) / loose を前の形に戻す (`TestShutdownNamesUnadoptedResume`)
- [x] codex (gpt-6-luna, effort high) の敵対的レビュー: 具体的な発火条件のある指摘なし。未確認リスクとして「再開の `-n` が本物の一覧の name に出るか」は
  偽の一覧でしか見ていない (488 の実測に依る) を挙げた
- 未確認のリスクとして残すもの (直す根拠が無い)
  - 短い id が別の session を指す形は、その session が生きている間は終了・閉じるが名指しで失敗し続け、カードも再開できない (fail closed。人が確かめて claude stop するまで)
  - 488 より前の版の dispatcher が始めた再開 (名前を渡していない) の結果を、この版が取り込む形は名前で当たらない (dispatcher の入れ替え 505 の途中だけ。名指しに回る)
  - kind も時刻も崩れて cwd も repo root の落ちた PG は、戻るまで名指しだけになる / 2 つの状態の置き場で worktree がぶつかる形は未確認のまま
