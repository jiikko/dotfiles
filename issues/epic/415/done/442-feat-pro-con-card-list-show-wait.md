# 442 (feat): pro-con card list / show / wait — カードを画面なしで読む

起票日: 2026-09-25

親: [441](../441-design-pro-con-viewer.md)

## 概要

PM (Claude が CLI で行う) と外の Claude が、画面を開かずにカードを読む口。今は `card add` が受付の箱の依頼 ID だけを返し、
振られたカード ID も分からない (2026-09-25 の dogfooding で cards.json を python で読んだ)。

## 対応方針 (候補)

- `pro-con card list [--state <列>]`: カードの一覧 (ID・列・題名・担当の PG・最古の待ち)
- `pro-con card show <カード>`: 依頼の原文・履歴・質問・テストの係の結果・PG の出力の末尾 (画面の詳細と同じ中身)
- `pro-con card wait <カード> [--until <列>]`: 状態が変わるまで待つ (dispatcher の知らせ = socket の購読で。無ければポーリング)
- `pro-con card add` は適用を待ってカード ID を返す (待てなければ依頼 ID を返して、そう言う)
- どれも記録を読むだけ (441 の守ること 1)。出力は人と Claude の両方が読める形 (`--json` も)

## 進捗

- 2026-09-25 (dotfiles-7d): 実装した (commit「feat(pro-con): card list / show / wait と、add が適用を待ってカード ID を返す (issue 442)」)

### 受け入れ条件

- [x] `pro-con card list [--state <列>] [--all] [--json]`: ID・列・題名・担当・経過・待ち。片付けたものは `--all` のときだけ。列は画面の見出し (依頼 …) でも英語 (requested …) でも書ける
- [x] `pro-con card show <カード> [--json]`: 依頼の原文・PM に渡した指示・待ち・質問・テストの係に頼んだコマンド・実行中・追加オーダー・履歴・PG の出力の末尾 (10 行)。
      出力の末尾は起動の記録 (`sessions.json`) で短い id とカード ID を照らして session id を引き、transcript から読む (記録に無い session のものは出さない。`claude` は呼ばない)
- [x] `pro-con card wait <カード> [--until <列>] [--timeout <長さ>] [--json]`: dispatcher の知らせ (wake の購読) で即時に読み直し、届かなくても 1 秒ごとに読み直す。上限つき (既定 10m、時間切れは rc=1)。
      完了したカードは待たずに rc=1 (`--until` の列には来ない / 列はもう変わらない)
- [x] `pro-con card add` は適用を待ってカード ID を返す (`--wait <長さ>`、既定 10s)。add が作ったカードに元の依頼 ID (`card.Card.FromRequest`) を持たせて引く。
      除けられたら rc=1 で理由。待てなければ依頼 ID を出して **rc=3** (rc=0 にすると、依頼 ID を次の `card plan` に渡しても誰も気づかない)。`--wait 0` は待たずに依頼 ID (rc=0)
- [x] 読み取りだけ: `TestViewCommandsDoNotWrite` (cardview_test.go) が、list / show / wait の前後で状態の置き場と transcript の置き場のファイルの中身・権限・mtime・数が変わらないこと、
      socket に届いたのが購読の `sub` だけ (wake / notify が無い) であることを見る。445 の「状態の置き場が変わらない」のテストの形 (偽の dispatcher で socket に届いた行を記録する) はここを下敷きにできる
- [x] pm-guide.md (PM への指示書) と README を更新。指示書のコマンドは TestPMGuideCommandsParse が読む口も含めて通す

### 検証

- テスト 11 本 (cardview_test.go)。変異 (bin/mutate-verify) 16 本がそれぞれ狙ったテストで red: FromRequest を書かない / 別の依頼のカード・除外を返す /
  除外を見ない / 購読しない / ポーリングしない / show で wake を打つ / wait で notify を打つ / list で記録を書き直す / 片付けたものを出す /
  完了で諦めない (--until あり・なし) / 起動の記録のカード ID を照らさない / 先の行で打ち切る / 時間切れを rc=0 にする
- 敵対的レビュー (Opus) 2 周。1 周目 P2 3 件・P3 6 件のうち 8 件を直した (上の rc=3・完了で諦める・`--wait` をフラグとして読む・テスト 3 本の素通り・
  起動の記録のカード ID)。2 周目は P1 なし、P2 1 件 (ポーリングのテストの競合) と P3 を直した。2 周目の直しはどれも直接の変異で確かめ、3 周目は回していない
  (「`--until` なしで完了なら諦める」は判定の新設だが、1 周目で攻めた `--until` の場合と同じ形)
- 本物の置き場 (dogfooding 中) で `card list` / `show` を 1 回ずつ打って読めることを確かめた

### 記録のみ (直していない)

- 完了以外で「もう来ない」列を待つ (例 レビュー の列から `--until 作業中`) は時間切れまで待つ。store の遷移に戻り道があるかを列ごとに決めていないので、判定を足していない
- rc=3 の後に PM が add をやり直すと、最初の依頼も後で適用されてカードが 2 枚になる (案内文で `card list` を勧めているだけ。仕組みでは止めていない)
- dispatcher がカードに元の依頼 ID を書くのはこの変更から。古いバイナリの dispatcher が動いている間は、適用されても add は rc=3 (案内文で再起動を勧める)
- 長いパスの置き場では、wait の購読が wake の逃がし先 (`/tmp/pro-con-<uid>`) の権限を 0700 に直すことがある (wake の既存の設計。状態の置き場には書かない)
