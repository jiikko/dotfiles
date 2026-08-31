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
