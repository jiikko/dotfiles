# 626 (ux): glogx の利用枠 (U / R) を、Claude と codex の両方がそろうのを待たず、取れた方から出す

起票日: 2026-10-02

## 概要

glogx の利用枠の表示 (右上の U の箱 / 全画面の R のダッシュボード) は、`usage.FetchAll` が Claude と codex を並列に取ったあと
両方の結果を 1 つの Snapshot に併合してから 1 通の `usageMsg` で届く。速い方 (codex app-server は 0.6〜1.2 s、claude は約 2 s。
`usage_overlay.go` の fetchCmd のコメントの実測) が先に返っても、遅い方 (最大 `fetchTimeout` = 10 s) を待ってから描かれる。
ユーザーの要望 (2026-10-02): 取得した方から表示していく。

## 対応方針

- usage パッケージ: 出所ごとの取得 (Claude / codex) と、出所ごとの結果を Snapshot へ入れる併合を 1 か所に置き、`FetchAll` もそれで組む
  (bin/ratelimit と hook の振る舞いは変えない)
- glogx: fetchCmd は 2 本の取得を同時に投げ (Cmd が `tea.BatchMsg` を返す)、`handle` は届いた出所の枠だけを入れ替える
  - last-good は出所ごと (今の `MergeLastGood` と同じ不変条件)。全滅 (両方失敗) の扱い・`staleErr` / `ClaudeErr` の注記は今と同じ意味を保つ
  - single-flight は「その周の 2 本が両方返るまで」。キャッシュの保存は周の終わりに、その周で取れた分だけで行う (Claude 必須の契約は今のまま)
  - 片方を待っている間は、待っている出所を「取得中」として出す

## 進捗

- 2026-10-02: 起票
