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

## 関連

- 460 (監査の記録) / 457 / 447 / 01dbb3b0 (done・pid 無しの実測)
