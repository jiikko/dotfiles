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

- [x] Claude の Bash (non-interactive snapshot) から `codex --version` が関数経由で成功する
- [x] 対話 zsh での brew 自動 install 挙動 (helper の本来の目的) が退行しない

## 対応 (2026-09-01)

候補 1 を self-heal 付きで採用。`codex()` (`_zshrc`) を次の 3 段に変更:

1. helper 未定義なら `$HOME/dotfiles/zshlib/_ensure_cli_with_brew.zsh` を source して自己修復
2. それでも未定義 (ファイルが無い環境) なら ensure を諦める — ensure は対話シェルでの
   brew 自動 install の利便であり、無くても codex の実行は成立する
3. `command codex "$@"` を実行

原因の実測: Claude Code の shell snapshot は `_` 始まりの関数をほぼ含めない
(定義 78 関数中 `_` は 1 つ)。codex だけが snapshot に載り、実行時に helper が
`command not found` になっていた。

- 回帰テスト: `tests/zshrc/codex-wrapper/test_codex_snapshot_survives.sh`
  (snapshot 条件 = 関数本体のみ・helper 未定義の zsh を再現。対話回帰 2 本も同居)。
  変異 (旧実装へ戻す) で red を確認
- 受け入れ条件 1 の注記: 現行セッションの snapshot は旧定義のまま残るため、実環境での
  確認は次セッション起動後 (snapshot は起動時に再生成される)。テストが同条件を固定している
- 横展開: 同型 2 系統 (`_reload_then_call` 系 6 関数 / t・tt の `_TMUX_SESSION_LIB` 消失) を
  issue 152 に起票。codex と違い「Claude Bash から呼ぶ需要があるか」の判断が要るため分離した

## 受け入れ条件 1 の確認 (2026-09-02)

新 snapshot (`snapshot-zsh-1788306374630`, 09/02 08:46) を `zsh -f` で source し、helper 未定義
(`${+functions[_ensure_cli_with_brew]}` = 0) のまま関数経由で `codex --version` → `codex-cli 0.152.1` / rc 0。
旧 snapshot (`...1788188722354`) では `command not found: _ensure_cli_with_brew` / rc 1。

🚨 Bash ツールのシェルは**プロセス起動時の snapshot を持ち続ける**ため、起動済みセッションでは
旧定義のままになる (会話を clear しても引き継がれる)。既存セッションでは `command codex` で回避する。
