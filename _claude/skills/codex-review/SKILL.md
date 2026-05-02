---
name: codex-review
version: 2.1.0
description: codex exec review を基本に、必要なら codex exec を使ってコード変更のレビューを依頼し、指摘事項を報告する。
---

# Codex Review

`codex exec review` を基本に、必要なら `codex exec` を fallback として使い、コード変更に対する Codex のレビューを取得する。

## 引数

`$ARGUMENTS` は自由テキスト。commit ID、差分の基点、重点的に見てほしい観点を含められる。

```
/codex-review                                    → 未コミット変更をデフォルトレビュー
/codex-review --uncommitted                      → 未コミット変更をデフォルトレビュー
/codex-review abc1234                            → 特定コミットをデフォルトレビュー
/codex-review abc1234..HEAD                      → 現在の HEAD に対する差分をデフォルトレビュー（--base abc1234）
/codex-review origin/master..HEAD                → upstream との差分をデフォルトレビュー
/codex-review 削除ロジックの安全性を重点的に見て     → 未コミット変更を重点レビュー
/codex-review abc1234 削除ロジックの安全性を重点的に見て → 特定コミットを重点レビュー
/codex-review --strict                           → 未コミット変更を厳しめにレビュー
```

## 手順

### 1. 引数のパース

`$ARGUMENTS` から以下を抽出する:

- **`--uncommitted`**: 明示的にフラグがあれば未コミット変更をレビュー
- **`--strict`**: 厳しめレビューモード
- **基点付き差分**: `{base}..HEAD` または `{base}..{current_head_sha}` 形式のみ `--base {base}` として使用する。`{base}` は commit SHA でもブランチ名でもよい
- **任意の差分範囲**: `{base}..{target}` で `{target} != HEAD` の場合、`codex exec review` ではそのまま表現できない。誤った差分をレビューしないため、そのまま `--base` に変換せず、終点を `HEAD` に合わせて実行するかをユーザーに確認する
- **commit ID**: 7〜40文字の hex 文字列（`[0-9a-f]{7,40}`）が単独であれば `--commit {sha}` として使用
- **カスタム指示**: 上記以外のテキスト（レビュー指示に追記する）
- **デフォルト**: 何も指定がなければ `codex exec review --uncommitted` で未コミット変更をレビュー
- **CLI 制約**: `codex exec review` は `--base` / `--commit` / `--uncommitted` と `[PROMPT]` を併用できない。selector を使う場合はプロンプトなしで実行する
- **コマンド選択**:
  - selector のみ: `codex exec review`
  - selector なしで `--strict` またはカスタム指示あり: `codex exec review` のプロンプト付きモード
  - selector と `--strict` / カスタム指示が両方ある: `codex exec -s read-only` を使う

### 2. 出力先の準備

```bash
mkdir -p ./tmp
stamp="$(date +%Y%m%d-%H%M%S).$$"
review_out="./tmp/codex-review.$stamp.md"
```

- 出力ファイルは毎回ユニークなパスにする
- `/tmp` ではなく必ず `./tmp` を使う

### 3. レビューの実行

`codex exec review` を基本に使用する。`--full-auto --ephemeral -o "$review_out"` は常に付与する。selector と追加指示を両立させる必要がある場合のみ、`codex exec -s read-only` を使う。

#### レビュー指示テンプレート

**通常モード（プロンプト付き）:**

```
コードレビューして。バグ、リグレッション、仕様逸脱、テスト不足を優先。問題があるものだけを重要度順に列挙し、各項目に file:line、理由、最小修正案を書く。要約や称賛は不要。
```

**厳しめモード（`--strict`）:**

```
厳しめにコードレビューして。バグ、データ破壊、クラッシュ、並行処理の不整合、仕様逸脱、テスト不足を優先。良い点の記述は不要。問題があるものだけを重要度順に列挙し、各項目に file:line、再現条件、理由、最小修正案を書く。
```

カスタム指示がある場合は、テンプレートの末尾に追記する。

#### 実行モード

- **デフォルトモード**: 引数なしなら `codex exec review --uncommitted` を使う
- **プロンプト付きモード**: selector なしで実行する。未コミット変更に対して `--strict` やカスタム指示を渡したい時に使う
- **selector モード**: `--uncommitted` / `--commit` / `--base` を付けて実行する。この場合はプロンプトを付けない
- **fallback モード**: selector と `--strict` / カスタム指示を両立させたい時は `codex exec -s read-only` を使い、対象差分をプロンプト内で明示する

#### 実行パターン

```bash
# パターン A: デフォルトの未コミットレビュー
command codex exec review --uncommitted --full-auto --ephemeral -o "$review_out"

# パターン B: プロンプト付きの未コミットレビュー
command codex exec review --full-auto --ephemeral -o "$review_out" \
  'コードレビューして。バグ、リグレッション、仕様逸脱、テスト不足を優先。問題があるものだけを重要度順に列挙し、各項目に file:line、理由、最小修正案を書く。要約や称賛は不要。'

# パターン C: 特定コミットのデフォルトレビュー
command codex exec review --commit {sha} --full-auto --ephemeral -o "$review_out"

# パターン D: 現在の HEAD に対する差分のデフォルトレビュー
command codex exec review --base {base} --full-auto --ephemeral -o "$review_out"

# パターン E: 特定コミットを重点レビューする fallback
command codex exec -s read-only --full-auto --ephemeral -o "$review_out" \
  'commit {sha} をコードレビューして。重点観点: {custom_instruction}。最初に `git show --stat --oneline {sha}` と必要な diff を確認してからレビューする。バグ、リグレッション、仕様逸脱、テスト不足を優先し、問題があるものだけを重要度順に列挙し、各項目に file:line、理由、最小修正案を書く。要約や称賛は不要。'

# パターン F: 基点付き差分を重点レビューする fallback
command codex exec -s read-only --full-auto --ephemeral -o "$review_out" \
  '{base}..HEAD の差分をコードレビューして。重点観点: {custom_instruction}。最初に `git diff --stat {base}..HEAD` と必要な diff を確認してからレビューする。バグ、リグレッション、仕様逸脱、テスト不足を優先し、問題があるものだけを重要度順に列挙し、各項目に file:line、理由、最小修正案を書く。要約や称賛は不要。'
```

- `command` プレフィックス必須（zsh の関数オーバーライドを回避）
- コマンド実行ツールのタイムアウトは 900000ms（15分）に設定する

### 4. 結果の読み取りと報告

1. まず `"$review_out"` を読む
2. `"$review_out"` が空なら失敗扱いにせず、Codex コマンドの stdout / stderr を確認する
3. `codex exec review` は `-o` に最終メッセージを書かず、stdout 側にだけレビュー本文を出す場合がある。その場合はコマンド出力からレビュー本文を拾って報告する
4. 指摘を以下の3カテゴリに分けて報告する:
   - **すぐ直すべきもの**（バグ、クラッシュ、データ破壊など）
   - **後回しでよいもの**（コードスタイル、軽微な改善など）
   - **指摘なし** の場合はその旨を報告
5. 高重大度の指摘があれば、修正するか確認する

### 5. 一時ファイルの削除

レビュー本文を読み終わってユーザーに報告した **直後に**、`./tmp` に書き出したばかりの `.md` ファイル（`$review_out`）を必ず削除する。`./tmp` をレビュー結果のアーカイブ置き場にしない。

```bash
rm -f "$review_out"
```

- 削除対象は今回の実行で作った 1 ファイルのみ。`./tmp/*.md` を一括削除しない（他の作業中ファイルを巻き込むため）
- レビュー結果は会話履歴に残しておけば十分で、ファイルを残す必要はない
- 例外的にユーザーが「ファイル残しといて」と明示した場合のみスキップ

## プロジェクト固有のコンテキスト

Codex はリポジトリ内のコンテキストをある程度拾えるが、重要なプロジェクトルールや今回のレビュー観点は必要に応じてプロンプトに追記する。

## ルール

- `command codex` を使うこと（`codex` 直接呼び出しは zsh 関数オーバーライドでエラーになる場合がある）
- 常に `--full-auto --ephemeral -o "$review_out"` を付与する
- `codex exec` fallback を使う時は `-s read-only` を付ける
- まず `-o` の出力ファイルを読み、空なら stdout / stderr を fallback として使う
- レビュー結果はそのままユーザーに見せる（要約しすぎない）
- `/tmp` は使わず、出力ファイルは必ず `./tmp` に置く
- レビュー本文を読み終わって報告したら、`./tmp` に書き出したばかりの `.md` ファイル（`$review_out`）を必ず `rm -f` で削除する。`./tmp` を恒久的なレビュー結果置き場にしない（ユーザーが明示的に残せと言った場合のみ例外）
- `codex exec review` と `codex exec -s read-only` はレビュー用途として使い、コードを変更しない
- デフォルトは `codex exec review --uncommitted` にする。master 直作業の運用では、必要に応じて `origin/master..HEAD` のように基点を明示する
- 任意の `{base}..{target}` を安易に `--base {base}` へ変換せず、終点が `HEAD` の場合だけ変換する
- `--base` / `--commit` / `--uncommitted` とプロンプトを併用しない
- コマンド実行ツールのタイムアウトは 900000ms（15分）に設定する
- **構造的修正優先**: 指摘の報告時、修正案は中長期的に改修を続けることを前提とした構造的な方針を優先する。場当たり的な条件分岐やワークアラウンドを修正案として提示しない
