# concat 関数クロスレビュー指摘事項

調査日: 2026-03-14
調査モード: Forge Minimum+（code-reviewer, architecture-reviewer, security-auditor）
調査対象: `zshlib/_concat.zsh`, `zshlib/_concat_helpers.zsh`

---

## 🔴 High Priority

### 1. `${(o)}` 辞書順ソートでファイル連結順が狂う
- **ファイル**: `_concat.zsh` L131, L214, L555
- **内容**: `${(o)}` は辞書順ソートのため、ゼロパディングなしのファイル名（`clip_1, clip_10, clip_2`）で数値順にならない。動画の連結順序が入れ替わる実害がある
- **発生条件**: ファイル数10以上 かつ ゼロパディングなし命名。実用上の発生確率は低い
- **推奨**: `${(n)}` （数値ソート）に変更する
- **指摘元**: architecture-reviewer, code-reviewer（独立して同一バグを指摘）

### 2. macOS `date +%s%N` がリテラル `%N` を出力する
- **ファイル**: `_concat.zsh` 一時ファイル名生成箇所
- **内容**: macOS の date コマンドはナノ秒 `%N` に非対応で、リテラル文字列 `%N` が出力される。一時ファイル名の一意性が低下し、並行実行時に競合する可能性がある
- **推奨**: `$$` (PID) や `$RANDOM` の組み合わせ、または `mktemp` で代替する

### 3. `__concat_escape_path` の戻り値が呼び出し元で未チェック
- **ファイル**: `_concat.zsh` → `_concat_helpers.zsh`
- **内容**: エスケープ失敗時もそのまま concat list に書き込まれるため、ffmpeg が想定外のファイルを連結する可能性がある
- **推奨**: 戻り値をチェックし、失敗時はエラー終了する

---

## 🟡 Medium Priority

### 4. awk 除算のゼロ除算リスク
- **ファイル**: `_concat_helpers.zsh` ビットレート計算
- **内容**: `$total_duration` が 0 の場合に awk でゼロ除算が発生する。上流で duration=0 は弾かれるため実害は低いが、防御的チェックが望ましい
- **指摘元**: code-reviewer（architecture-reviewer が High→Medium に降格）

### 5. `__concat_find_common_suffix` が同一文字列で空を返す可能性
- **ファイル**: `_concat_helpers.zsh`
- **内容**: 全ファイルが完全に同名のエッジケースで、suffix が空になる可能性がある

### 6. バックスラッシュを含むパスで concat list が壊れる
- **ファイル**: `_concat.zsh` リスト生成箇所
- **内容**: ffmpeg concat list のエスケープ処理がバックスラッシュを正しく扱えない

### 7. dryrun モードで不要な一時ファイルが作成される
- **ファイル**: `_concat.zsh`
- **内容**: `--dry-run` でも一時的な concat list ファイルが生成される。副作用なしが期待されるモードとして不適切

### 8. 数値パターンに一致しないファイルがグルーピングで無言スキップされる
- **ファイル**: `_concat.zsh` グルーピング処理
- **内容**: グループ化の際、数値パターンに一致しないファイルが警告なくスキップされ、ユーザーが気づかない

### 9. `echo "$info"` が `print -r --` でない箇所がある
- **ファイル**: `_concat_helpers.zsh` 診断出力
- **内容**: `echo` は特殊文字（バックスラッシュ、`-n` 等）を解釈するため、ファイル名に特殊文字が含まれると出力が壊れる。`print -r --` に統一すべき

---

## 🟢 Low Priority

### 10. always ブロック内の cleanup 処理にコメントがない
- **ファイル**: `_concat.zsh`
- **内容**: zsh の仕様上は正しく動作するが、意図を示すコメントがあると保守性が向上する
- **指摘元**: code-reviewer（architecture-reviewer が High→Low に降格）
