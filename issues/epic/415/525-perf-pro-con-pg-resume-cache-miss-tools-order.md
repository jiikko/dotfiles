# 525 (perf): PG の再開がキャッシュに当たらない (tools の並びが起動ごとに揺れる。PG の書き込みの約 36%)

起票日: 2026-09-26

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

外から動かす Claude の確認 (カード C-075) に、ユーザーが「起票して積む」と答えた。根拠は 449 の「C-002 の再開がキャッシュに当たらなかった理由」と「判断材料」
(`issues/epic/415/pending/449-research-pro-con-pg-startup-cost.md`)。

- PG の再開 87 回のうち 22 回が、再開したプロセスの最初の要求でキャッシュに当たらず、会話全体を書き直した (計 3.20M トークン。1 回平均 145k)。
  2026-09-25 の PG の書き込みの計 8.98M の約 36%、料金換算で約 $25.6・5 時間枠の約 7.3%
- 外れた側はどれも、起動時の tools に `SendFeedback` が在った session (26,601 の側)。再開のプロセスでは在るかどうかが揺れ、並びが変わると要求の先頭からキャッシュと食い違う
  (`thinking_drop` の `prefix_mismatch` が毎回付く)。揺れる理由は未確認。起動時に `SendFeedback` が無かった 5 session の再開 13 回は全部当たった

## 対応方針

- 449 の案: **PG を起こすとき (起動と再開の両方) に tools の並びを揃える**。例: `dispatcher/launcher.go` の `startArgs` / `resumeArgs` に `--disallowedTools SendFeedback` を足し、
  どのプロセスでも `SendFeedback` を tools に入れない。実装の選択 (付ける引数・PM と取り込みの係にも付けるか) は PG が決めてよい
- 🚨 **入れる前に、本物で 1 回測る** (未検証の点: 付けたときに tools から本当に消えるか / system prompt の `EndConversation` の段も揃うか / 28,875 などの別の並びが残らないか)。
  測るには本物の claude を起こすので利用枠を使う。起こす回数を最小にし (例: 起動 1 回 + 再開 1 回)、起動時の `prompt_snapshot` と再開の最初の要求の読み・`thinking_drop` の有無で当たりを確かめる
- テストは偽の claude で「起動と再開の引数に同じ tools の指定が入る」ことを固定する (本物の振る舞いは上の実測で確かめる)
- 効いたかは、入れた後の本物の PG の再開で、外れの回数と `prefix_mismatch` を 449 と同じ数え方で数え直して書き戻す (perf-claims-need-measurement)

## 関連ファイル

- `src/pro-con/dispatcher/launcher.go` の `startArgs` / `resumeArgs` / `withSettings`

## 関連

- 449 (PG の起動のコスト。数え方と根拠) / 464 (claude の実体の解決) / 431 (PG の session 用の設定)
