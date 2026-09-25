# 461 (feat): PG / PM の起動に言語の設定を渡す (報告が英語になるのを止める)

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

2026-09-25 の dogfooding で、PG の報告 (出力の末尾) が途中から英語になった (440 の 4 回目)。7d の測定 (440) で、
`claude -p … --setting-sources project,local` (PG と同じ) では英語の依頼に英語で返し、**`--settings '{"language":"日本語"}'` を足したときだけ日本語で返した** (3/3)。
ユーザーの settings.json の `language` は `--setting-sources` に user を入れても `-p` では効かなかった。今は PM が指示に「報告は日本語で」と書いて回している。

## 期待する動作

- dispatcher が PG (と 437 の PM) を起動・再開するとき、言語の設定を `--settings` で渡す。値はユーザーの settings.json の `language` を読んで渡す
  (dotfiles に日本語を固定で書かない。読めなければ渡さない)
- 🚨 `--settings` の中身は言語だけにする (hook や許可を足さない。`--setting-sources project,local` で user の hook を外している意味を崩さない)
- `--bg` で効くかは未測定 (隔離した cwd では「Workspace not trusted」で起動を拒むため 7d は測れていない)。worktree の中で `--bg` を 1 回起こして確かめる方法を本文に書く
  (本物の PG と同じ repo の worktree なら trusted のはず。確かめられなければ未確認と書く)

## 関連

- 440 (4 回目の記録と 7d の測定) / 431 (PG の session の設定。役割ごとの `--settings` はそちらの叩き台と同じ口) / 437 (PM の起動)
