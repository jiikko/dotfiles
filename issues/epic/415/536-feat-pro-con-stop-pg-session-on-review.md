# 536 (feat): レビューの列に入ったら、PG の session を止める

起票日: 2026-09-27

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

外から動かす Claude の確認 (カード C-089) に、ユーザーが「止める」と答えた。

- 今は、PG が `card review` で終えてレビューの列に入っても、PG の session は idle のまま残る。1 本 415〜480MB (2026-09-27 に 4 本で約 1.8GB)
- 差し戻しの再開 (`dispatcher/launcher.go` の `Resume`) は、古いプロセスを `claude stop` してから `--resume` で起こし直すので、残しておいても再開には効かない
- 残して効くのは、レビュー中に人が `a` で attach してすぐ話せることだけ

## 期待する動作

- カードがレビューの列に入ったら、閉じたときと同じく PG の session を止める (447 の止める印と `stopMarked` / `ensureStopped` の同じ部品を使う。止める仕組みを 2 つ作らない)
- 差し戻し (`card rework`) と、取り込みの係・人の回答での再開は、今と同じく `--resume` で続きから起こす
- レビュー中に `a` で attach したいときは、再開してから attach する (その形を画面で案内するか、attach のときに自動で再開するかは PG が決めてよい。見た目が変わるなら見本で人に選んでもらう)
- 実装の選択は PG が決めてよい

## 確かめること

- 止めた後の再開: `Resume` は `stopID` があれば先に `claude stop` するが、`stopID` を立てるのは `dispatcher.go` の `prepare()` で、今の一覧に PID 付きで載っている session のときだけ (止まった session には stop を呼ばず、そのまま `--resume` する)。この安全弁を壊さないこと。偽の claude で「止まった session の rework が続きから再開する」をテストで固定する
- 止め損ねたときの扱いは 447 と同じ (上限の時間で諦め、理由を履歴と出来事に残す)。レビューの列のカードを取り込みの係が閉じるときの止め直しと二重にならないか
- 止めたことを履歴と出来事に残す

## 関連ファイル

- `src/pro-con/dispatcher/close.go` (`stopMarked`) / `src/pro-con/dispatcher/shutdown.go` (`ensureStopped`) / `src/pro-con/dispatcher/launcher.go` (`Resume`) / `src/pro-con/store/store.go` (`case "review"`)

## 関連

- 447 (閉じたら PG を止める) / 446 (差し戻し) / 487 (取り込みの係) / 527 (attach)

## 順番の見積もり (PM, 2026-09-27。C-089 を C-082 の後に積んだ)

- 触る場所: レビューの列に入る遷移で止める印を付ける所、`Resume`、レビュー中の `a` (attach)
- 変える判断: レビュー中の PG の session を持つか / 止まった session へどう attach するか
- 順番の理由: 527 (C-082) が attach の経路 (popup の窓・戻るキー) を作っている最中。止まった session へ attach するときの形は、その経路の上に乗せる

## 進捗 (C-089, 2026-09-27)

作り方 (止める仕組みは 447 の部品のまま。新しい停止の経路は作っていない):

- `store` の `review` の適用で `StopAfterClose` を付ける (PG の session があるとき)。`dispatcher/close.go` の `stopMarked` が止める
  - 止めに入らない形 (`reviewBusy`): 未達の追加オーダーがある (deliverOrders が同じ session の再開で届ける) / 記録の session が生きていて turn を終えていない (`card review` の後の報告を切らない。Status が読めない形も待つ)。待つ間は諦めるまでの時間を数えない
  - 止め終えたら `Stopped` を付ける (終了のときと同じ印)。差し戻しの再開 (`prepare`) は、一覧に無い session に stop を撃たず、自動の再開 (restartWait) も待たずに `--resume` する。`stopID` の安全弁は触っていない
  - 止め損ねたら 447 と同じく `closeStopWait` で諦めて履歴と出来事に書く (Stopped は付けない)
- 差し戻し (`rework`) と deliverOrders の再開で印を外す (残すと再開した PG を止める)
- deliverOrders は、レビュー待ちで PG の session が生きていない (止めた) ときも再開してオーダーを届ける (届かないままだと close が未達のオーダーで弾かれ続ける)
- 閉じるとき (close) の止め直しは残した (確かめとして安い)。レビュー待ちで止めてあれば履歴は「閉じた (PG の session はレビュー待ちの間に止めてある)」と書き、「既に止まっていた」を重ねない
- 画面の `a`: レビュー待ちで止めた PG には attach を頼まず、「話すなら + で追加オーダー (同じ session を続きから再開して届ける)」と案内する。attach のときに自動で再開する形は取らなかった (再開には PG へ渡す文が要り、PG が turn を 1 回走らせてしまう)
- help (`help/usage.md` の attach 節) に 1 行。mock の模擬もレビュー待ちで Stopped を付ける

確かめたこと:

- `dispatcher/review_stop_test.go` (偽の claude): idle なら止める / turn の途中は待つ / 止めた session の差し戻しは stopID 無しで続きから再開 / 止め終える前の差し戻しで印を外す / 止めた後の追加オーダーを届ける / 未達のオーダーがあれば止めずに届ける / 閉じたときに重ねて書かない / 止め損ねたら諦める。`ui` に a の案内のテスト
- 変異 9 本 (印を付けない・Stopped を付けない・rework で外さない・turn 待ちを外す・未達の待ちを外す・止めた後のオーダー配達を外す・配達で印を外さない・close の文・画面の分岐) をすべてテストが落とした
- 本物の claude では未確認 (止めた session の `--resume` は 447・終了の再開と同じ経路)

未確認リスク (敵対的レビューから。直していない):

- 止める要求を出してから一覧で止まったと確かめるまで (1 Tick ほど) は `Stopped` が付いていないので、画面の `a` は attach を頼む。その間に開いた attach は止める処理で切れうる (画面は PG の Status を持たないので、この窓だけ断る分岐は作れない。attach の間に人が打てば PG は busy になり、止めるのを待つ)
- 落ちて一覧から消えた直後の PG も、確かめで止まっていれば `Stopped` を付けて自動の再開を待たずに `--resume` する。447・終了の判定のままで、この変更で悪くなってはいない
