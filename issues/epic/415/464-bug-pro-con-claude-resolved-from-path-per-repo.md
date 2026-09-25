# 464 (bug): claude を PATH の素の名前で引くので、nodenv の shim が repo ごとに別の版の claude を選ぶ

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

もろい作りの監査 (460) の観点 2。PG の起動・再開・一覧・停止が、repo によって違う版の claude で行われうる。

## 詳細

- 該当: `src/pro-con/dispatcher/launcher.go` の `runClaude` (`exec.CommandContext(ctx, "claude", …)`、起動は `cmd.Dir = repoPath`、再開は worktree) / agents・usage・runner も同じく素の名前
- 発火条件 (実測): dispatcher の env の PATH に nodenv の shim が入っていて `NODENV_VERSION` が無い (人が画面から起こした dispatcher はこの形)。
  shim は cwd の `.node-version` で node を選ぶので、`~/src/gx-navi` (node 19.3.0) では claude 2.1.17 (`agents --json` は `unknown option '--json'`)、
  `~/src/ubiregi-server` (22.11.0) では 2.1.273 が選ばれた (監査の係が `NODENV_VERSION` を外した shim の PATH で実行)
- 壊れ方: 古い版では起動・一覧が失敗して 462 のループに入る。少し古い版では、起動・再開はその版、一覧と stop は別の版で行われ、stop の意味 (done / stopped。01dbb3b0 の実測) が合うかは未確認
- 今の dogfooding の dispatcher は PM の Claude から起こしたので node 24.2.0 に固定されていて、この形が見えていない

## 対応方針 (候補)

- dispatcher の起動時に `claude` の絶対パスを 1 回解決して固定し、版をログと出来事 (444) に出す (`path-shim-must-resolve-real-binary.md` の形。解決は PATH を見た 1 回だけ)
- あわせて、dispatcher を起こした側の env が PG まで流れる件 (PG に `CLAUDE_PID` / `CLAUDE_EFFORT` など PM の値が入っていた。`card run` には CLAUDECODE・CLAUDE_CODE_SESSION_ID・
  MESSAGING_SOCKET まで渡る。挙動への影響は未確認。460 の P3) も、PG に渡す env を決めて絞る

## 関連

- 460 (監査の記録) / 462 (失敗が上限なく繰り返す) / 461 (PG に渡す設定)
