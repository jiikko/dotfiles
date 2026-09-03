# glogx: git log の自動追従の反映が Update を 20-140ms 止める

種別: perf / priority: low (実害の報告が出たら着手する)

## 現状

`gitlog_watch.go` の反映 (`reflectGitLogChange` → `tui.go` の `reloadLog`) は **Update の中で
同期的に** git を 5-6 本 fork する:

- `LoadCommits` (解析用 `git log`)
- `LoadLogDisplay` (表示用 `git log`。`--oneline` 以外)
- `planStatuses` → `UnpushedSHAs` (`rev-list`) + `ResolveRepo` (`remote get-url` ×2 / `rev-parse`)

## 実測 (合成 repo: 60 コミット × 各 4000 行 patch、2026-09-01 / 敵対レビューで計測)

| 表示 | Update のブロック時間 |
|---|---|
| 既定 (medium) | 22 ms |
| `--stat` | 45 ms |
| `-p` | 139 ms |

デバウンスは 1 秒なので、rebase を回している間は最大 1 秒ごとにこの停止が入る。

## なぜ今直さないか

- pull (`u`) の全面リロードは前からこの形で、体感の苦情は出ていない
- ただし pull は**利用者の操作**なので停止が予期できる。自動追従は**無操作で入る**点が違う
- `LoadCommits` / `LoadLogDisplay` は `runGit` (timeout なし) なので、git が stall すると
  Update が戻らない (その間 Ctrl-C も処理されない)。起動時の同期経路と同じ関数を使っているため、
  ここだけ timeout を付けると「起動はできるのに自動追従だけ失敗する repo」が生まれる

## 着手の trigger

- 実 repo (大きな monorepo) で「外部 commit のときに一瞬固まる」と感じたとき
- `-p` を常用する運用が出たとき

## 直し方の候補

- 読み直しを Cmd (goroutine) へ出し、結果 (`[]Commit` / verbatim / statuses) を Msg で受けてから
  モデルへ入れる。世代 (`logWatch.gen` と同じ規律) で古い結果を捨てる
- 錨の画面行は「読み直し前」に測る必要があるので、Cmd を出す時点で測って Msg に載せる

## 決着 (2026-09-02)

trigger を待たず着手した (ユーザー指示)。候補どおり読み直しを Cmd へ出した。

- `reloadLog` を `loadLogData` (git fork のみ・モデルを触らない) と `applyLogData` (反映) に分けた。pull の
  全面リロードは同期のまま `reloadLog` を使う (利用者の操作)
- `reflectGitLogChange` は `loadLogData` を Cmd に出し、`gitLogReloadMsg` で受けて `handleGitLogReload` が
  `applyLogData` する。錨 (先頭行 / カーソル) と keepView の判定は Msg が届いた時点 = 行集合を入れ替える直前に
  測る (Cmd を出した時点の錨は、届くまでのスクロールで古くなる)
- 捨てる条件: 見張りの世代 `gen` が進んだ / 読み直しの世代 `reloadSeq` が進んだ (in-flight 中に pull が入った。
  古い logData で pull の結果を巻き戻さない) / 見送り状態になった / 読み直し失敗。いずれも基準 `seen` を触らないので
  次の観測で再挑戦する
- in-flight 中は `reloading` の札で測定を見送る (結果は捨てられるだけの fork)

### 実測 (2026-09-02, 合成 repo 60 コミット × 各 4000 行 patch, 同一マシン)

| 表示 | 変更前 (issue 本文, Update 内の同期) | 変更後 Update 内 | 変更後 goroutine 側 | 反映 (apply) |
|---|---|---|---|---|
| 既定 | 22 ms | 0.2 µs | 23.5 ms | 0.1 ms |
| `-p` | 139 ms | 0.25 µs | 59.5 ms | 0.45 ms |

git の fork はそのまま (総コストは減らない) で、Update を止めなくなっただけ。`-p` の goroutine 側が
issue 本文の 139ms より短いのは計測条件の差 (patch 本文の内容が違う) で、改善ではない。

- テスト: `TestGitLogReflectRunsGitInsideCmd` (Cmd を作った後に PATH を空にして、Cmd 実行が初めて失敗する =
  Update 内で読んでいない) / `TestGitLogReloadDiscardsWhenSelfReloadHappenedMeanwhile` /
  `TestGitLogReloadDefersWhenOverlayOpensDuringReload` / `TestGitLogProbeSkipsMeasureWhileReloading`。
  既存の反映テストは `runGitLogReload` で Cmd を実行して Msg を渡す形に直した。変異検証: seq 判定削除 → red /
  apply 時の見送り削除 → red / reloading 見送り削除 → red / Update 内で同期に読む → red
  (🚨 最初に書いた配線テストはこの 4 つ目を検知できず、PATH を空にする形に作り替えた)
- 残る制約: `LoadCommits` は timeout なし。git が stall すると Update は止まらないが `reloading` が立ったままに
  なり自動追従だけ止まる (コードコメントに明記)
- 敵対的レビューは通していない (codex は自発起動しない運用)
