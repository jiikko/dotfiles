# 441 (design): 外の Claude から、動いている pro-con を読み取りだけで覗く (viewer)

起票日: 2026-09-25

> epic 441 の親 issue。設計の正本 (目的・守ること・範囲) はここに置き、実装は同じディレクトリの子 issue で進める。

## 概要

人間が pro-con を使っている最中に、別の Claude の session (デバッグ役) から、その pro-con を**読み取りだけで**覗けるようにする
(2026-09-25 にユーザーが依頼: 「外部の claude から僕の pro-con を参照してデバッグとかログが見れるように、viewer モードとかアタッチする機能」)。

## なぜ要るか (2026-09-25 の dogfooding で実際に困ったこと)

- カードの状態・PG の出力の末尾・質問を見るのに `cards.json` を python で読んだ (PM の CLI にカードを読む口が無い)
- dispatcher の判断は手で起動したときの nohup の出力 (自由文) にしか残らず、「なぜ止め直しを続けているか」は log を tail して推理した
- 画面の不具合 (開いた直後の「dispatcher 未起動」・「画面 N」が遅れて出る) は、隔離した tmux の画面を撮って初めて見つかった。
  人間の画面に何が出ているかを外から読む口が無い

## 守ること

1. **読み取りだけ**: viewer の経路から、依頼・回答・停止・attach のどれも打てない (入力の口を作らない)。
   外の Claude が覗いても、人間の pro-con の状態を 1 バイトも変えない (テストで固定する)
2. **自分だけ**: 同じユーザーだけが読める (状態の置き場は 0700、socket は 0600。package wake と同じ)
3. **人間の画面を遅くしない**: 覗かれているかどうかで画面の描画が待たない (中継は最新の 1 枚を置くだけ)
4. 見えるものは状態の置き場にあるものと同じ範囲 (依頼の原文・PG の出力の末尾)。それ以上の秘密を増やさない

## 範囲 (子 issue)

- [442](442-feat-pro-con-card-list-show-wait.md) — `pro-con card list / show / wait` (カードを画面なしで読む。PM の CLI にも要る)
- [443](443-feat-pro-con-screen-relay.md) — 画面の中継 `pro-con screen` (人間の画面に今出ているものを外から読む)
- [444](444-feat-pro-con-event-log.md) — 出来事の記録 `pro-con log` (dispatcher の判断を構造化して残し、外から読む)
- [445](445-risk-pro-con-viewer-read-only-guarantee.md) — 読み取りだけであることの担保と、見せる範囲

## 関連

- epic 415 (pro-con 本体)。dogfooding の記録は 440
