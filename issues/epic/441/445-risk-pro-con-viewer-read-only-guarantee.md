# 445 (risk): viewer が読み取りだけであることの担保と、見せる範囲

起票日: 2026-09-25

親: [441](441-design-pro-con-viewer.md)

## 概要

441 の viewer (442〜444) が、人間の pro-con の状態を変えないこと、見せてよいものだけを見せることを、設計とテストで担保する。

## 確かめること

- viewer のコマンド (`card list / show / wait`・`screen`・`log`) が、受付の箱・記録・socket の「起こす / 知らせる」を 1 つも書かない
  (テストで、実行の前後で状態の置き場の中身が変わらないことを見る。socket に `wake` / `notify` を送らないことも)
- 覗いている Claude が PG に入力できない (attach・SendMessage の経路を viewer に作らない)
- 見える範囲: 依頼の原文・PG の出力の末尾・入力欄に打ちかけの文。同じユーザーの別の Claude が読むことを前提にしてよいか、
  打ちかけの文は出さない方がよいかを決める (ユーザーに聞く)
- 画面の中継 (443) のファイルと出来事の記録 (444) の置き場の権限 (0600 / 0700) と、大きさの上限

## 進捗

- 2026-09-25: 442 の読む口 (`card list / show / wait`) には、`src/pro-con/cardview_test.go` の `TestViewCommandsDoNotWrite` を入れた
  (実行の前後で状態の置き場と transcript の置き場のファイルの中身・権限・mtime・数を比べ、偽の dispatcher で socket に届いた行が購読の `sub` だけかを見る)。
  443 / 444 の口にも同じ形を当てられる。見せる範囲 (打ちかけの文) の判断は継続
