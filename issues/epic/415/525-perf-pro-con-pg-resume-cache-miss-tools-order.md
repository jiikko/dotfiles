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

## 進捗 (2026-09-27、カード C-101)

- [x] 入れる前に本物で測った (起動 1 回 + 再開 2 回。うち 1 回は flag 付きの再開が起こしたコピー)。結果は下の「実測」
- [x] PG を起こすとき (起動と再開の両方) に `SendFeedback` の有無を揃えた: `pro-con: PG・PM の --settings に feedbackDrafts: off を足し、起動と再開で SendFeedback の有無を揃える (525)`。
  `--disallowedTools` ではなく設定 `feedbackDrafts: "off"` にした (理由は下)。PM も同じ `sessionSettings` を使うので揃う。haiku (要約役・btw) は 1 回きりの `-p` で再開が無いので付けない
- [x] テスト (偽の claude なし。引数を組む関数を直接): 起動と再開の `--settings` に同じ `"feedbackDrafts":"off"` が入る (`launcher_test.go` の 2 本)。旧コードに戻すと 2 本とも赤になるのを確かめた
- [ ] 効いたかを、入れた後の本物の PG の再開で数え直す (取り込みの後)。🚨 **これだけでは外れは減らない見込み** (下の「実測」3)。続きは `546-research-pro-con-endconversation-gate-resume-cache.md`

### 実測 (claude 2.1.283)

1. **`SendFeedback` の出入りは GrowthBook の gate と設定で決まる** (claude の実体のコードを読んだ): `isEnabled() = feedbackDrafts !== "off" && Cdr()`、
   `Cdr()` は `tengu_juniper_relay` の値 (環境変数 `CLAUDE_CODE_SEND_FEEDBACK=false` でも外れる)。`feedbackDrafts` は policy / `--settings` (flagSettings) / user の設定から読む。
   → `--settings` に `"off"` を入れれば gate と関係なく外れる。`--disallowedTools` を選ばなかったのは、下の 2 のとおり `EndConversation` には効かず、
   `SendFeedback` に効くかも gate が false の起動でしか試せなかったため (実体のコードで効き方が読める設定の方を採った)。
   🚨 gate が true の起動で `"off"` によって消えることは**未観測** (今は `~/.claude.json` の gate のキャッシュが false で、起動時に在る側を起こせない)
2. **`--disallowedTools SendFeedback,EndConversation` で起動 → 再開**: 起動時の `prompt_snapshot` に `SendFeedback` も `EndConversation` の段も無かったが、
   再開の最初の要求の直前に `deferred_tools_delta` が `EndConversation` を**足した** (`--disallowedTools` は `EndConversation` に効かない)。
   起動時に無かったのは gate が false だっただけで、flag の効果とは言えない
3. **その再開はキャッシュを外した**: 起動の要求は読み 24,803 / 書き 21,718、再開の最初の要求は読み **26,318** / 書き **20,349** (会話全体をほぼ書き直し。`thinking_drop` は無し)。
   tools は揃っている (`SendFeedback` は両方とも無い) ので、食い違いは 26,318 より後 = system prompt の `EndConversation` の段と推定 (再開時の system prompt は transcript に残らない)。
   `EndConversation` の段は `tengu_umber_kestrel` の gate と ToolSearch の有効判定だけで決まり、**設定・環境変数で外す経路が無い** (実体のコード)。
   449 の数え方では `SendFeedback` とこの段はほぼ一緒に動く (プロセス 343 本で「両方在り 254 / 両方無し 85 / `SendFeedback` だけ在り 4」)ので、
   `SendFeedback` だけ揃えても、揺れの大半は段の側で残る
4. **2.1.283 でも外れは続いている** (C-047〜C-100、カードの 2 本め以降の transcript の、前のプロセスの最後の記録より後の最初の要求を数えた):
   起動時に `SendFeedback` 在りの session の再開 74 回のうち 23 回、無しの session の 42 回のうち 5 回が書き込み 10k 超。大きな外れ (書き直し 25k〜244k) は、
   読みがちょうど 24,803 (`SendFeedback` 無しの側の共有部分。2.1.282 の 24,645 に当たる) で揃う = 449 と同じ仕組み
5. 付随して分かったこと: flag 付きの `claude --bg --resume` は、止めた session でも「保存した options を持つので、flag 付きならコピーを起こす」と
   stderr に出してコピー (別の session id) を起こす。flag なしなら元の session を保存した options (`-n` / `--disallowedTools` / `--setting-sources` / `--settings`) で起こす。
   pro-con は前から flag 付きで再開していて、カードごとに transcript が複数あるのはこのため (2.1.282 から同じ。stdout の先頭は `backgrounded · <id>` のままなので読み取りは壊れない)
