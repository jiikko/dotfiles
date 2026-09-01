# 149 codex: shell 関数が profile helper 依存で Claude の Bash snapshot から起動できない

- 起票: 2026-09-01 (出典: obaket retro 652 項目 2、実測 2026-08-31)

## 症状

Claude Code の Bash shell から `codex` を呼ぶと `command not found: _ensure_cli_with_brew`。
shell snapshot に zsh profile の helper (`_ensure_cli_with_brew`) が含まれないため、
snapshot に載った `codex` 関数が実行時に壊れる。`/opt/homebrew/bin/codex` 直叩きで回避可能
(codex-review skill の `command codex` prefix はこの回避が効いている場面もある)。

## 対応候補

1. `codex` 関数を helper 非依存にする (存在チェックを関数内で自己完結 or 素の PATH 解決に委ねる)
2. または helper 群を snapshot に含まれる場所へ移す

## 受け入れ条件

- [ ] Claude の Bash (non-interactive snapshot) から `codex --version` が関数経由で成功する
- [ ] 対話 zsh での brew 自動 install 挙動 (helper の本来の目的) が退行しない
