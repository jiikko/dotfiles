# 465 (bug): `-w pc-<カード>` は同じ名前の worktree を黙って再利用し、カード ID は置き場を作り直すと C-001 に戻る

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

もろい作りの監査 (460) の観点 2。状態の置き場を作り直すと、新しいカードが前の世代のカードのブランチとコミットの上で黙って作業する。

## 詳細

- 実測 (2.1.282・一時 repo。監査の係): `old-branch` の worktree `pc-c-001` を先に作ってから `claude -p -w pc-c-001` を実行すると、rc=0 で新しい worktree は作られず、既存の worktree が使われた (locked になった)
- 該当: `src/pro-con/dispatcher/launcher.go` の Start (`-w pc-<カード>`) / `store.Load` (cards.json が無いと NextID 1 から数え直す)。
  PG の worktree は消さない設計 (447) で、dotfiles には pc-c-002〜010 が残っている
- 発火条件: 状態の置き場 (`~/.local/state/pro-con/live`) を消す・作り直す・別の置き場で動かした後に、同じ repo で C-00N を起動する
- 壊れ方: 新しい C-00N の PG が前の世代のブランチ・未取り込みのコミットの上で作業を始める。PM の cherry-pick に前の世代のコミットが混ざりうる
- 427 の「同時に 2 つの置き場」の記録とは別の形 (こちらは順番に作り直した場合)

## 対応方針 (候補)

- 起動の前に `<repo>/.claude/worktrees/pc-<カード>` が在るかを見て、在れば起動せずに理由を書く (人の番へ)。または worktree の名前に置き場の印を混ぜる
- 使い終わった PG の worktree とブランチの片付け (人が判断する。今日の分は pc-c-002〜010) の口を決める

## 関連

- 460 (監査の記録) / 427 (同時に 2 つの置き場) / 451 (削除のときも worktree は残す)

## 進捗

- 2026-09-25 (C-025): 1 つ目の方針を入れた。`dispatcher.prepare` が、まだ一度も起動を始めていない (`LaunchedAt` が空) カードの
  `<repo>/.claude/worktrees/pc-<カード>` が在れば起動せず、462 の人の番の部品 (`askAfterCrashes`) で理由 (パス) を書く。
  片付けてから回答すると起動する。起動し直し (印の後) は自分の前の起動が作った worktree なので止めない。
  検査は `TestStartRefusesLeftoverWorktree` / `TestRestartAfterUnknownLaunchKeepsOwnWorktree` (偽の launcher と一時 repo。変異 2 本で red を確認)
  - 427 の「同時に 2 つの置き場」でも、後から起動する側の C-00N は同じ理由で止まる (起動の直前と claude -w の間の競合は残る)
- 残り: 使い終わった PG の worktree とブランチの片付けの口 (人が判断する。pc-c-002〜010)
