---
name: crash-log-analyzer
version: 2.0.0
description: macOS アプリのクラッシュログ (.ips) を解析し、根本原因を特定するスキル。「クラッシュした」「crash」「.ips」「DiagnosticReports」「SIGSEGV / SIGABRT」で発火。**macOS 専用**（.ips の読み方が macOS の DiagnosticReports 前提。iOS の .ips は未対応）。解析は crash-analyzer subagent に委譲する。クラッシュログが存在しない一般のバグ調査・テスト失敗には使わない。
---

# macOS Crash Log Analyzer

対象のクラッシュログを選び、解析を `crash-analyzer` agent に渡して、結果を検閲して報告する。
**.ips の読み方・分類・報告の書式の正本は `_claude/agents/crash-analyzer.md`**。ここには写さない
(2 箇所に書くと片方だけ直って食い違う。実際に食い違っていた)。

## Step 1: 候補を出す

- プロジェクトに `bin/*crash-log` があれば (ThumbnailThumb の `bin/tt-crash-log` など)、それで最新のログを特定してよい
- 無ければ `crash-analyzer.md` の「0. 対象のログを決める」にある一覧のコマンドを、`APP` にアプリ名を入れて回す。
  1 行目のメタデータ (`timestamp` / `bug_type` / `app_name`) だけを読むので速い

## Step 2: ユーザーに確認する (必要なときだけ)

候補が 1 件で、`app_name` がカレントプロジェクトのアプリ名と一致し、24 時間以内なら確認を省いて Step 3 へ進む。
それ以外 (候補が複数 / 0 件 / アプリ名が違う / 古いものしかない) は AskUserQuestion で聞く:

1. **対象のログ**: 候補 (アプリ名・日時) から選んでもらう
2. **クラッシュしたときの状況**: 何をしていたか (任意)

## Step 3: agent に渡す

```
subagent_type: crash-analyzer
prompt: |
  次のクラッシュログを解析してください。手順と報告の書式はあなたの定義に従ってください。
  - ログ: {crash_log_path}
  - プロジェクトのソース: カレントディレクトリ
  - クラッシュしたときの状況: {user_context または「不明」}
```

## Step 4: 結果を検閲して報告する

agent (sonnet) の報告はそのまま採らない (`~/.claude/rules/subagent-model-tiering.md`):

- 「根本原因」とされた `file:line` を自分で開き、そのコードがスタックトレースの関数と合っているかを見る。
  合わなければ「仮説」に落とす
- 仮説のまま確度が低い / ソースの深い理解が要るなら、main モデルで自分が読み直すか debugger agent へ回す
- **issue を起こすかはユーザーに確認し、起こすならその repo の issue 規約で書く** (type は `bug`)。
  agent には起票させない
