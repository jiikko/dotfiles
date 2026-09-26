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
