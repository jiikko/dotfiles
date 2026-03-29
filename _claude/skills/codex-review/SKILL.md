---
name: codex-review
version: 2.0.0
description: codex exec review コマンドでコード変更のレビューを依頼し、指摘事項を報告する。
---

# Codex Review

`codex exec review` を使って、コード変更に対する Codex のレビューを取得する。

## 引数

`$ARGUMENTS` は自由テキスト。commit ID、差分の基点、重点的に見てほしい観点を含められる。

```
/codex-review                                    → 既定ブランチとの差分をレビュー
/codex-review --uncommitted                      → 未コミット変更をレビュー
/codex-review abc1234                            → 特定コミットをレビュー
/codex-review abc1234..HEAD                      → 現在の HEAD に対する差分をレビュー（--base abc1234）
/codex-review abc1234 削除ロジックの安全性を重点的に見て
/codex-review --strict                           → 厳しめレビュー
```

## 手順

### 1. 引数のパース

`$ARGUMENTS` から以下を抽出する:

- **`--uncommitted`**: 明示的にフラグがあれば未コミット変更をレビュー
- **`--strict`**: 厳しめレビューモード
- **コミット範囲**: `{sha1}..HEAD` または `{sha1}..{current_head_sha}` 形式のみ `--base {sha1}` として使用
- **任意のコミット範囲**: `{sha1}..{sha2}` で `{sha2} != HEAD` の場合、`codex exec review` ではそのまま表現できない。誤った差分をレビューしないため、そのまま `--base` に変換せず、終点を `HEAD` に合わせて実行するかをユーザーに確認する
- **commit ID**: 7〜40文字の hex 文字列（`[0-9a-f]{7,40}`）が単独であれば `--commit {sha}` として使用
- **カスタム指示**: 上記以外のテキスト（レビュー指示に追記する）
- **デフォルト**: 何も指定がなければ、リモート既定ブランチとの差分をレビュー

### 2. 既定ブランチと出力先の準備

```bash
default_base="$(git symbolic-ref --quiet --short refs/remotes/origin/HEAD 2>/dev/null | sed 's#^origin/##')"
if [ -z "$default_base" ]; then
  if git rev-parse --verify --quiet main >/dev/null; then
    default_base=main
  elif git rev-parse --verify --quiet master >/dev/null; then
    default_base=master
  else
    echo "default base branch could not be determined" >&2
    exit 1
  fi
fi

mkdir -p ./tmp
review_out="./tmp/codex-review.$(date +%Y%m%d-%H%M%S).$$.md"
```

- `origin/HEAD` を優先して既定ブランチを解決する
- 解決できない場合のみ `main`、次に `master` を試す
- 出力ファイルは毎回ユニークなパスにする

### 3. レビューの実行

`codex exec review` を使用する。`--full-auto --ephemeral -o "$review_out"` は常に付与する。

#### レビュー指示テンプレート

**通常モード（デフォルト）:**

```
コードレビューして。バグ、リグレッション、仕様逸脱、テスト不足を優先。問題があるものだけを重要度順に列挙し、各項目に file:line、理由、最小修正案を書く。要約や称賛は不要。
```

**厳しめモード（`--strict`）:**

```
厳しめにコードレビューして。バグ、データ破壊、クラッシュ、並行処理の不整合、仕様逸脱、テスト不足を優先。良い点の記述は不要。問題があるものだけを重要度順に列挙し、各項目に file:line、再現条件、理由、最小修正案を書く。
```

カスタム指示がある場合は、テンプレートの末尾に追記する。

#### 実行パターン

```bash
# パターン A: 既定ブランチとの差分（デフォルト）
command codex exec review --base "$default_base" --full-auto --ephemeral -o "$review_out" \
  'コードレビューして。バグ、リグレッション、仕様逸脱、テスト不足を優先。問題があるものだけを重要度順に列挙し、各項目に file:line、理由、最小修正案を書く。要約や称賛は不要。'

# パターン B: 未コミット変更
command codex exec review --uncommitted --full-auto --ephemeral -o "$review_out" \
  'コードレビューして。...'

# パターン C: 特定コミット
command codex exec review --commit {sha} --full-auto --ephemeral -o "$review_out" \
  'コードレビューして。...'

# パターン D: 現在の HEAD に対する範囲
command codex exec review --base {base_sha} --full-auto --ephemeral -o "$review_out" \
  'コードレビューして。...'
```

- `command` プレフィックス必須（zsh の関数オーバーライドを回避）
- コマンド実行ツールのタイムアウトは 300000ms（5分）に設定する

### 4. 結果の読み取りと報告

1. `"$review_out"` を Read ツールで読む
2. 指摘を以下の3カテゴリに分けて報告する:
   - **すぐ直すべきもの**（バグ、クラッシュ、データ破壊など）
   - **後回しでよいもの**（コードスタイル、軽微な改善など）
   - **指摘なし** の場合はその旨を報告
3. 高重大度の指摘があれば、修正するか確認する

## プロジェクト固有のコンテキスト

Codex はリポジトリ内のコンテキストをある程度拾えるが、重要なプロジェクトルールや今回のレビュー観点は必要に応じてプロンプトに追記する。

## ルール

- `command codex exec review` を使うこと（`codex` 直接呼び出しは zsh 関数オーバーライドでエラーになる場合がある）
- 常に `--full-auto --ephemeral -o "$review_out"` を付与する
- stdout 直読みではなく、`-o` で出力されたファイルを読むこと（stdout には警告や進捗が混ざるため）
- レビュー結果はそのままユーザーに見せる（要約しすぎない）
- `codex exec review` はリードオンリーなので、コードを変更しない
- `origin/HEAD` を優先して既定ブランチを解決し、任意の `{sha1}..{sha2}` を安易に `--base {sha1}` へ変換しない
- コマンド実行ツールのタイムアウトは 300000ms（5分）に設定する
