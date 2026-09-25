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

## `--bg` で効くかを確かめる方法

本物の PG と同じ repo の worktree (trusted) を cwd にして、`--bg` を **1 本だけ**起こし、終わったら必ず止める:

1. `claude agents --json --cwd <worktree>` で基準を見る (同じ cwd の session が無いこと)
2. `cd <worktree> && claude --bg -n <名前> --model haiku --setting-sources project,local --settings '{"language":"日本語"}' "What is the capital of France? Answer in one short sentence. Do not use any tools."`
   (`-w` は付けない。worktree の中で -w を付けると入れ子の worktree を作る)
3. `claude logs <id>` で返事の言語を見る
4. `claude stop <id>` → `claude agents --json` に id が無く、`--all` で `state: done` になったのを見る

## 進捗

- 2026-09-25 (C-013): 実装
  - `dispatcher/launcher.go`: `ExecLauncher{UserSettings}` を持たせ、起動・再開のたびにユーザーの settings.json から
    `language` **だけ**を抜いて `--settings '{"language":…}'` を位置引数 (prompt) の前に挟む。読めない・壊れている・
    無い・文字列でない・空なら付けない (起動は止めない)。パスは claude と同じく `CLAUDE_CONFIG_DIR` があればその下 (`UserSettingsPath`)
  - `dispatchercmd.go`: 本物の dispatcher が `UserSettingsPath(home)` を渡す。437 の PM も同じ `d.Launch.Start/Resume` を通るので PM にも効く
    (origin/master 60d94191 へ rebase し、`dispatcher/pm.go` が `d.Launch.Start` / `Resume` を呼ぶのを確認)
- 確かめたこと
  - テスト: `dispatcher/launcher_test.go` (言語だけを渡す・hook / 許可 / model を持ち込まない・渡さない 6 通り・CLAUDE_CONFIG_DIR) と
    `dispatchercmd_test.go` の配線 (本物の dispatcher の launcher が settings.json のパスを持つ)。偽の settings.json を TempDir に置き、claude は起動しない
  - `bin/mutate-verify` で変異 7 本すべて想定のテストが red (rc=0): language だけでなく全体を渡す / 空の language を通す / 空でも --settings を付ける /
    起動に渡さない / 再開に渡さない / CLAUDE_CONFIG_DIR を無視 / dispatcher の配線を外す
  - **`--bg` で効いた** (上の方法で 1 回、2.1.282、haiku、cwd = この repo の worktree `pc-c-013`): 英語の問いに「パリはフランスの首都です。」。
    Workspace not trusted は出なかった。`claude stop a09fba4c` 後、稼働一覧から消え `--all` で `state: done` を確認
  - 🚨 対照 (`--settings` 無しの `--bg`) は取っていない (1 本だけの約束のため)。英語で返る側の証拠は 7d の `-p` の測定だけ。model も haiku で、PG の既定の model では測っていない
  - 敵対的レビュー (読み取りのみのサブエージェント 1 本): 再現する不具合は無し。「`Start` / `Resume` が language を読む配線がテストで守られていない」
    (`Start` の中で `""` を渡しても緑) は正しかったので、引数の組み立てを `ExecLauncher` のメソッドにして閉じた (変異 2 本で red を確認)。
    記録だけ: language の前後の空白は trim せずそのまま渡す (claude 側の扱いは未確認。実害は見ていない)
  - `make test` rc=0 (origin/master f8ab3915 へ rebase 後の c3915c80。pro-con card run)
- 残り: 本物の dispatcher で PG を起こして報告が日本語になるかは、次の dogfooding で見る (440)。PM が指示に「報告は日本語で」と書いている分は、それを見てから外す
