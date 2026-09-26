# 535 (ux): 「分解済み」のレーンの名前を「着手待ち」にする

起票日: 2026-09-27

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

ユーザーの提案 (2026-09-27): 「分解済みっていうレーン名称だけど、着手可能・着手待ちとかにすれば?」。

- 「分解済み」は PM がしたこと (分けた) の名前で、カードの今の状態を言っていない
- この列に居るのは、PG が起こされるのを待つカード: PG の空き待ち / 利用枠の回復待ち (hold) / 順番の前のカードの完了待ち (`--after`。468)。
  後ろの 2 つは今は着手できないので「着手可能」は事実と食い違う。**「着手待ち」にする** (どれにも合う)
- 「分解済み」「着手待ち」はどちらも全角 4 文字なので、ヘッダ・レーンの幅は変わらない

## 変えるもの (2026-09-27 に `git grep -c 分解済み` で数えた。67 ファイル 229 か所)

- 名前の正本: `src/pro-con/card/card.go` の `State.Label` (Planned) と `Meaning` の文
- 人に見せる文: `dispatcher/dispatcher.go`・`dispatcher/shutdown.go`・`store/store.go` の出来事や履歴の文、`README.md`・`pm-guide.md`・`integrator-guide.md`・`?` のヘルプ (515)・`pro-con help` (510)
- テスト (期待値の文)

## 変えないもの

- 状態の内部の名前 (`card.Planned`・`--state planned`・JSON の値)。記録と CLI の引数の互換を保つ
- 見本の `.ans` (`samples/`。その時点の画面の写し) と issue の本文 (経緯の記録)
- 既に書かれた履歴 (`cards.json` の履歴の文)。古いカードの履歴に「分解済み」が残るのは受け入れる

## 受け入れ条件

- [ ] 画面のレーン・ヘッダ・`card list` / `card show` の列の名前が「着手待ち」
- [ ] `?` のヘルプ・README・pm-guide・`pro-con help` の説明が「着手待ち」で、この列に居る理由 (空き待ち・枠待ち・順番待ち) が書かれている
- [ ] `--state planned` は今までどおり使える
- [ ] production と文書に「分解済み」が残っていない (`git grep 分解済み -- src ':!src/pro-con/samples'` が 0 件。残すなら理由をその行に)

## 関連

- 515 (`?` のヘルプの流れ。C-073。同じ `Meaning` を触るので、その後に着手) / 468 (`--after`) / 510 (`pro-con help`)

## 進捗

(まだ無い)
