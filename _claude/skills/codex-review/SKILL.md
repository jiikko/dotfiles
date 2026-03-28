---
name: codex-review
version: 1.2.0
description: codex review コマンドでコード変更のレビューを依頼し、指摘事項を報告する。
---

# Codex Review

`codex review` コマンドを使って、コード変更に対する Codex のレビューを取得する。

## 引数

`$ARGUMENTS` は自由テキスト。commit ID、コミット範囲、重点的に見てほしい観点を含められる。

```
/codex-review                                    → 未コミットの変更をレビュー
/codex-review abc1234                            → 特定コミットをレビュー
/codex-review abc1234..def5678                   → コミット範囲をレビュー（--base abc1234）
/codex-review abc1234 削除ロジックの安全性を重点的に見て
/codex-review 同期周りのモデリングが適切か確認して
```

## 手順

### 1. 引数のパース

`$ARGUMENTS` から以下を抽出する:

- **コミット範囲**: `{sha1}..{sha2}` 形式があれば `--base {sha1}` として使用（sha2 は無視、HEAD までの差分）
- **commit ID**: 7〜40文字の hex 文字列（`[0-9a-f]{7,40}`）が単独であれば `--commit {sha}` として使用
- **カスタム指示**: commit ID / 範囲 以外のテキスト
- **両方なし**: `--uncommitted` で未コミット変更をレビュー

### 2. レビューの実行

**重要: `codex review` CLI は `--commit`/`--base`/`--uncommitted` フラグと `[PROMPT]` 引数を併用できない。**

実行パターン:

```bash
# パターン A: フラグあり（プロンプト指定不可 — Codex のデフォルトレビューを使う）
command codex review --commit {sha}
command codex review --base {base_sha}
command codex review --uncommitted

# パターン B: プロンプトのみ（フラグなし、未コミット変更が対象になる）
command codex review "カスタム指示テキスト"
```

- `command` プレフィックス必須（zsh の関数オーバーライドを回避）
- タイムアウトは 300秒（5分）に設定する

### 3. 結果の報告

Codex の出力をユーザーに報告する。指摘がある場合は重大度順に整理する。

## プロジェクト固有のコンテキスト

Codex は自動的にリポジトリのコンテキスト（CLAUDE.md, README 等）を読むため、プロンプトへのプロジェクトルール手動追記は不要。

## ルール

- `command codex` を使うこと（`codex` 直接呼び出しは zsh 関数オーバーライドでエラーになる場合がある）
- レビュー結果はそのままユーザーに見せる（要約しすぎない）
- P1/P2 の指摘があれば、修正するか確認する
- `codex review` はリードオンリーなので、コードを変更しない
- タイムアウトは 300秒（5分）に設定する
