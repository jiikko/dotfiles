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

## 関連

- 460 (監査の記録) / 427 の終了の保証 / 447 (閉じたら止める)
