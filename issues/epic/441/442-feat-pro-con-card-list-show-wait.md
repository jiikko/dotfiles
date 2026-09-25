# 442 (feat): pro-con card list / show / wait — カードを画面なしで読む

起票日: 2026-09-25

親: [441](441-design-pro-con-viewer.md)

## 概要

PM (Claude が CLI で行う) と外の Claude が、画面を開かずにカードを読む口。今は `card add` が受付の箱の依頼 ID だけを返し、
振られたカード ID も分からない (2026-09-25 の dogfooding で cards.json を python で読んだ)。

## 対応方針 (候補)

- `pro-con card list [--state <列>]`: カードの一覧 (ID・列・題名・担当の PG・最古の待ち)
- `pro-con card show <カード>`: 依頼の原文・履歴・質問・テストの係の結果・PG の出力の末尾 (画面の詳細と同じ中身)
- `pro-con card wait <カード> [--until <列>]`: 状態が変わるまで待つ (dispatcher の知らせ = socket の購読で。無ければポーリング)
- `pro-con card add` は適用を待ってカード ID を返す (待てなければ依頼 ID を返して、そう言う)
- どれも記録を読むだけ (441 の守ること 1)。出力は人と Claude の両方が読める形 (`--json` も)
