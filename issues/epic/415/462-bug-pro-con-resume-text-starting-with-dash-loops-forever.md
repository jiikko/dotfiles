# 462 (bug): 回答が「-」で始まると claude がオプションと読み、再開の失敗を上限なく繰り返して枠を占める

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

もろい作りの監査 (460) の観点 2。再開・起動がすぐ失敗する形に回数の上限が無く、カードは分解済みのまま枠を 1 つ占めて回り続ける。

## 詳細

- 該当: `src/pro-con/dispatcher/launcher.go` の `ExecLauncher.Resume` (`--bg --resume <id> --setting-sources project,local <text>` の最後に本文を位置引数で渡す) /
  `store` の answer (`c.Resume = r.Answer` を生のまま) / `dispatch` (失敗 → 印を残す → launchGrace の後に起動し直す。回数の上限が無い)
- 発火条件 (実測): 回答の本文が「-」で始まる (箇条書きの回答)。`claude … "- …"` は `error: unknown option` で rc=1 (監査の係が隔離した cwd の `claude -p` で確認。2.1.282)
- 壊れ方: 再開の前に `claude stop` するので PG は止まったまま。カードは分解済みに居るので回答し直せない (`Answerable` が偽)。印が残ったまま launchGrace ごとに
  同じ失敗を繰り返し、枠を 1 つ占め続ける。落ちた回数の上限 (3c-2a) はこの形には効かない
- 同じループに入る形 (読んだだけ): trust していない repo (415 の実測 rc=1) / 消した worktree への `--resume` (chdir の失敗) / 未ログイン / 464 の古い claude
- 偽物の `Resume` はどんな文でも受けるので、テストから見えない (460 の偽物とのずれ)

## 対応方針 (候補)

- 本文を渡す前に `--` で位置引数を区切るか (claude が `--` を受けるかを先に実測)、固定の前置き (差し戻しの `ReworkPrefix` と同じ形) を付ける
- rc≠0 がすぐ返る起動・再開の失敗を分類し、同じ失敗が N 回続いたら分解済みで回し続けず、人の番 (質問待ち) へ回して理由を書く。偽物に「すぐ失敗する」を足してテストで固定する

## 関連

- 460 (監査の記録) / 464 (古い claude でも同じループに入る) / 452 (人の番)
