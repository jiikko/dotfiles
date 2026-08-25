# `一時ファイルの配置` 規約が `mktemp -d` の扱いを書いていない

起票日: 2026-08-25

## 何が問題か

`_claude/CLAUDE.md`「一時ファイルの配置」は 1 行しかない:

> **`/tmp` の使用は禁止. `./tmp` を使うこと。絶対に。**

この文は**誰が・いつ作る一時ファイルの話なのか**を書いていない。実際には少なくとも 2 種類ある:

1. **Claude がセッション中に作る作業用ファイル** — 検証レポート・スクラッチ・中間成果物。
   これは `./tmp` が正しい（[`move-report-conclusions-to-issues.md`](../_claude/rules/move-report-conclusions-to-issues.md) が
   この前提で書かれている）
2. **スクリプト / テストが実行時に作る隔離用ディレクトリ** — `mktemp -d` で作り、終了時に消すもの

規約は 1 のことを言っているように読めるが、2 に適用されるとも読める。

## 実測（2026-08-25, macOS 24.6.0）

- `mktemp -d` の出力は `/var/folders/hw/…/T/tmp.xxxxxx`（`$TMPDIR`）であり、**`/tmp` ではない**。
  つまり `mktemp -d` は文面上の「`/tmp` の使用」には該当しない
- repo 内の `*.sh` に `mktemp -d` が **174 箇所**。`tests/` の環境隔離ハーネス
  （`tests/tmux/` の socket 隔離、`tests/claude/`、`tests/zshrc/` 等）の標準手段になっている
- `issues/099-human-verify-glogx-cli-health-toast.md` の再現手順
  （`CODEX_HOME=$(mktemp -d) glogx`）も同じ形

したがって現状は「規約違反が 174 件ある」のではなく、**規約が対象範囲を明示していない**状態。

## なぜ直す価値があるか

規約が「絶対に」という強い語で書かれているため、**読んだ側が過剰に適用する側へ倒れる**。
実際、issue 100 の確認時に「099 の手順は規約とぶつかるのでは」と判断が一度止まった。
逆方向の事故も起こりうる: テストが隔離ディレクトリを `./tmp` 配下に作ると、
**test 失敗で中断したときに repo の作業ツリーにゴミが残る**（`mktemp -d` なら OS が回収する）。

## 対応案

`_claude/CLAUDE.md`「一時ファイルの配置」に境界を 2 行足す:

- Claude がセッション中に作る作業用ファイル（レポート・スクラッチ・中間成果）は `./tmp`
- スクリプト / テストが実行時に作って自分で消す隔離ディレクトリは `mktemp -d`（`$TMPDIR`）でよい。
  これを `./tmp` 配下に作らない（中断時に作業ツリーへ残る）

## 却下の選択肢

「自明なので書かない」も成立する。ただしその場合は本 issue に却下理由を残し、
同じ迷いが再生産されたら再評価する。

## 関連

- `_claude/CLAUDE.md`「一時ファイルの配置」（正本）
- [`move-report-conclusions-to-issues.md`](../_claude/rules/move-report-conclusions-to-issues.md) — `./tmp` が gitignore である前提を使っているルール
- `issues/100-retro-glogx-cli-health-2026-08-24.md` — この気づきが出た経緯
