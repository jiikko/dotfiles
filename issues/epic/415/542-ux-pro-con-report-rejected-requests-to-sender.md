# 542 (ux): 受付の箱で断られた依頼を、置いた人に知らせる (今は pro-con log を見ないと分からない)

起票日: 2026-09-27

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

2026-09-27、外から `pro-con card close C-087 --ending rejected` を打つと、依頼の ID が出て rc=0 で終わった。ところが dispatcher は
「close: レビューの列に無い (今は 質問待ち)」で除けていて (`pro-con log` の kind `reject`)、カードは質問待ちのまま残った。打った側は、log を見るまで気づけなかった。

- `card add` は `--wait` (既定 10 秒) で適用を待ってカードの ID を返す。ほかの操作 (close・rework・answer・delete・order・plan・move 等) は箱に置いたら返る
- 画面から置いた依頼も、除けられたときに画面に出るかは確かめていない

## 期待する動作

- `pro-con card` のどの操作も、既定で適用を待ち、除けられたら理由を出して rc≠0 で終わる (`add` と同じ `--wait`。待てないときは今と同じく依頼の ID と rc=3)
- 画面から置いた依頼が除けられたら、画面に理由を出す (通知)
- PM・PG・取り込みの係の指示書が「rc を見て、除けられたら理由を読む」前提で書かれているかも見直す

## 受け入れ条件

- [x] 列の合わない close を打つと、理由つきで rc≠0 になる
  (`TestCardOperationsReportRejection`: 質問待ちのカードへ `card close --ending rejected` → rc=1・stderr に「dispatcher が依頼 … を除けた: close: レビューの列に無い (今は 質問待ち)」。
  適用は dispatcher と同じ `store.Apply` をテストから呼ぶ形で、本物の dispatcher のプロセスを立てた確認はしていない)
- [x] 画面から置いた依頼が除けられると、画面に理由が出る
  (issue 481 で入っていた。画面の依頼はすべて `live.Backend.submit` を通って依頼 ID を覚え、記録の `rejected` と照らして置いた画面にだけ理由を渡し、
  `ui.Model.showRejected` がトーストに出す。`live` の `TestRejectedReasonGoesOnlyToSubmittingScreen` / `ui` の `TestRejectedRequestShownOnce` が通ることを 2026-09-27 に確認。
  実画面での目視はしていない)

## 関連

- `src/pro-con/cardcmd.go` (`addAndWait` / `parseCardWait`) / `src/pro-con/store/` (Apply の結果) / 438 (追加オーダー)

## 進捗

- 2026-09-27 (C-100): `pro-con card` の箱に置く操作は、どれも既定で適用を待つようにした (`submitAndWait`。記録の `Applied` / `Rejected` に依頼 ID が出るまで待つ)。
  除けられたら理由を stderr に出して rc=1、待てなければ依頼 ID を出して rc=3、`--wait 0` なら待たない (今までの動き)。適用された add 以外の操作は依頼 ID を出す。
  attach と run も同じ (`run <カード> [--wait <長さ>] -- <コマンド>`)
  - 古い dispatcher (カードに元の依頼 ID を書かない) で add が適用されたときは、時間切れまで待たずに rc=3 で返るようになった (`Applied` で適用は分かるため)
  - PG の指示・PM の指示書・取り込みの係の指示書に rc の読み方を足した。正本は `dispatcher.CardRCRule` の 1 つで、指示書 2 本は目印の行 `{{rc の読み方}}` を置き換える
    (`TestGuidesCarryCardRCRule`)。PM の指示書の「完了のカードには order を出せない (log に残る)」も「rc=1 で理由が出る」に直した
- codex の敵対的レビュー (P2 2 件): rc=1 には「受付の箱に置けない」も含む / rc=3 は読めない間に適用済みのこともある、の 2 点で rc の読み方の文言が誤っていた。直した
- 残り: 本物の dispatcher を立てた E2E と、実画面での目視はしていない。`pro-con config set` も箱に置いたら返る (card ではないので、この issue では触っていない)
