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
- [ ] 残り
  - 起動・再開の途中 (Launching) で、claude が返した id がまだカードに無い形は、名指しするだけで止めない
    (起動は名前、再開は worktree しか手がかりが無く、pro-con が起動したと示せない)。再開の途中にカードの worktree で人間が bg の session を
    立てていると、その間は終了が名指しで失敗する (再開の新しい session の名前が引き継がれるかは未測定。引き継がれるなら名前で絞れる)
  - 記録に無いまま落ちた PG (pid 0・cwd が repo root になる = 427 の 3f) は cwd で示せないので、戻るまでは名指しだけになる (レビューの P2。再現は組んでいない)
  - 記録の行はあるが、短い id が別の session id を指す形 (register が「別の session を指している」と知らせる) は、今も「一覧に無い = 止まっている」と読む
    (レビューの P2。`TestShutdownTouchesOnlyOwnSessions` がこの形を ok と決めているので変えていない。名指しに回すかは PM の判断)
  - 同じ repo を 2 つの状態の置き場で使うと worktree のパスがぶつかる (カード ID が同じ)。この変更の前からの形で未確認
  - 止まったかの判定 `Session.Stopped()` は 466 が扱う (ここではその 1 か所を使うだけにした)
  - make test は `pro-con card run` で頼んだ (結果は下に追記する)

## 関連

- 460 (監査の記録) / 427 の終了の保証 / 447 (閉じたら止める)
