# 466 (bug): `agents.Session.Stopped()` が知らない state を「止まった」と読む

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

もろい作りの監査 (460) の観点 2 の P1 (潜在)。止まったかの判定が「pid 無し かつ working でない」なので、再開の途中を表す state の名前が変わる版では、
再開の途中の PG を止まったと読んで止めない。

## 詳細

- 該当: `src/pro-con/agents/agents.go` の `Session.Stopped` (`PID == 0 && State != "working"`)。使うのは終了の `ensureStopped`・447 の `stopClosed`・457 の直し
- 発火条件: 自動の再開の途中 (pid 無し) を表す state が `working` 以外の名前になる、または state の欄が無くなる版
- 壊れ方: 閉じるときは「既に止まっていた」と書いて止めず、PG は 11〜18 秒後に戻って閉じたカードで動き続ける。終了のときの確かめの段も同じ。どちらも黙って起きる
- 根拠: コードと `TestSessionStopped` の表を読んだだけ (監査の係)。2026-09-25 の `--all` の 27 本の state は done 9 / stopped 9 / blocked 1 / working 1 / state の無い対話の session 7

## 対応方針 (候補)

- 止まった state を許可リストで決める (stopped / done)。それ以外で pid 無しは「判定不能」として止めに行き、出来事に警告を出す
- 🚨 457 (カード C-010) が同じ判定を触っているので、457 を取り込んでから着手する

## 進捗

- [x] 実装 (2026-09-25, pro-con カード C-019。457 は master に取り込み済みの上で着手)。「対応方針」の 1 つ目を採った
  - `agents.Session.Stopped` = pid 無し かつ state が stopped / done (許可リスト)。pid 無しで stopped / done / working のどれでもないものは
    `UnknownState` (判定不能) で、`Stopped` は偽 = 止める相手 (`stoppable`) に入る
  - 止めに行くときに `dispatcher/shutdown.go` の `unknownStateNote` が「pid が無く state が "…" (知らない値) なので、止まったと判定できない。止めに行く」を
    出来事 (stop) に出す。呼ぶのは終了のカードの段と、確かめる段 `ensureStopped` (閉じたとき 447 と記録に無い PG 457 もここを通る)
- [x] 確かめたこと (偽の launcher / 一覧。本物の claude・state dir には触らない。`dispatcher/unknown_state_test.go` / `agents/agents_test.go`)
  - 発火条件 (pid 無しで state が "resuming" / 空) で、直す前は red: 閉じると「閉じた後に確かめたら PG の session は既に止まっていた」で stops=[]、
    記録に無い PG の終了は stops=[] かつ err=nil、警告なし。直した後は green (`go test ./...` 全 package ok)
  - 変異 4 本 (`bin/mutate-verify`) がすべて想定のテストで red: 判定を「working でない」に戻す / `ensureStopped` の警告を落とす /
    カードの段の警告を落とす / `UnknownState` を常に偽
- [x] 敵対的レビュー (read-only のサブエージェント。写しのプローブで再現したもの 3 件を直し、それぞれ変異で red を確かめた)
  - PM を止めるとき (`stopPM`) に警告が出ない → PM にも出す (`TestShutdownStopsPMInUnknownState`)
  - 止めても知らない state のまま `--all` に残る版で、止め直しの周ごとに警告が重なる (実測 17 回) → 1 本の session につき dispatcher の生涯で 1 回
    (`Dispatcher.unknownSeen`。`TestUnknownStateWarningOncePerSession`)
  - 判定できる session (生きている / pid 無しの working) にも警告を出す変異が生き残る → 出さないことを検査 (`TestNoUnknownStateWarningForKnownStates`)
- 受けた帰結 (直していない): 止めた session も知らない state (例: state の欄が無い) で残る版では、止まったと確かめる手段が無いので、
  終了は名指しの失敗のまま ok にならず (serve の stopUntilDone は止め直しを続ける)、閉じたときは closeStopWait の後に「止められない」で諦める。
  黙って「止まった」と読むより声が出る側を選んだ。その版が来たら、止めた後の state を実測して許可リストに足す
- 未確認リスク (再現していない。レビューの推測)
  - 再起動などでプロセスだけ消え、pid 無しで blocked / failed のまま `--all` に残る session が今の版で在るなら、知らない state に入り、
    `claude stop` がエラーを返す形だと終了が失敗し続ける (425 の実測にこの形は無い)
  - 上の版では、unregistered の緩い照合 (`loose`) が worktree に残る止まった session を「示せない生きている session」として名指ししうる

## 関連

- 460 (監査の記録) / 457 / 447 / 01dbb3b0 (done・pid 無しの実測)
