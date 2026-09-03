# 203 test: lint 転用の残り 3 候補 (いずれも現在の違反 0 件 = 予防的)

起票日: 2026-09-03
出典: `/audit` の lint-from-done (direct、glogx 以外が対象。2026-09-03)
重要度: P3 (**現に壊れているものは無い**。将来の再発を止める投資)

## 前提: 実害のあった 2 件は同じ監査で実装済み (`7ae8d63d`)

- 丸ごと skip なのに `exit 0` → `scripts/check_skip_exit_code.sh` (違反 2 件を修正)
- Action の版 drift → `scripts/check_workflow_action_pins.sh` (`doctor.yml` の `setup-go@v6` を修正)

以下は **grep して現在の違反が 0 件**だったもの。「あると良さそう」ではなく done の issue で
実際に起きた形だが、**今は再発していない**ので優先度を下げて記録する。

## 候補 A: 公開 shell 関数が `_` helper に依存し、Claude の Bash snapshot で壊れる

出典: issue **149** (`codex()` が `_ensure_cli_with_brew` で command not found) / **152**
(横展開 grep で `_reload_then_call` 系 6 関数 + `t`/`tt` の `_TMUX_SESSION_LIB`)。**2 件**。
retro **153** が「横展開 grep が本命級を 2 系統掘り当てた」と記録している。

**検出したい形**: `_zshrc` と eager source される `zshlib/*.zsh` で定義された非 `_` の公開関数が、
本体で `_` 始まりの関数・変数を参照しているのに self-heal ガードを持たない形。

```zsh
concat() { _reload_then_call concat ... }                                    # ← 落とす
concat() { (( ${+functions[_reload_then_call]} )) || source ...; _reload... } # ← 通す
```

**現在の違反: 0 件**。`_zshrc` の公開関数 8 本 (`av1ify` / `av1c` / `concat` / `repair` /
`repair_mp4` / `validate-mp4` / `codex` + prompt 系) は全部ガード済み。

**偽陽性の risk: 小**。lazy source される `zshlib/_av1ify.zsh` 等の中の公開関数は snapshot に
載らないので対象外にする必要がある → 「`_zshrc` + `_zshrc` が無条件 source する lib」に限定する。

**実装**: 既存の `tests/zshrc/test_snapshot_wrappers_survive.sh` に**静的検査の節を足す**のが安い
(`tests/zshrc/test_print_p_injection.sh` の「§4 静的検査」が同じ設計の先例)。現在の実行時テストは
concat と tt の 2 本を固定しているだけで、**新しい公開ラッパーが増えたときは何も見ない**。

## 候補 B: 新しい Go プロジェクトが CI レーン無しで入る

出典: issue **080** (`GO_PROJECT_DIRS` の手動列挙 → 無音で lint/test から外れる。Makefile 側は
`wildcard src/*/go.mod` で解決済み) / **087**。**2 件**。

**検出したい形**: `Makefile` のコメントが手順書として残しているものをそのまま検査にする —
`src/*/go.mod` が在るのに `src/<name>/Makefile` に `lint:`/`test:` が無い、または
`.github/workflows/src_<name>.yml` が無い / その `paths:` が `src/<name>/` を含まない。

**現在の違反: 0 件**。6 プロジェクトすべてに 2 target + `src_*.yml` + 正しい paths filter が在る。

**実装**: `scripts/check_go_project_lanes.sh`、または `check_ci_group_deps.sh` に 4 本目として同居
(あちらが既に「Makefile を出典に CI の網羅を見る」役)。

## 候補 C: assertion を 1 つも持たない Go テスト

出典: issue **082** (`result_log_test.go:TestResultLogCloseIdempotent` が「panic しないこと」だけ) /
**170**。**2 件**。

**現在の違反: 0 件** (glogx 以外の `src/**/*_test.go` を機械走査して 0)。

**偽陽性の risk: 中**。table-driven のローカルヘルパー (`check(t, ...)`) に assertion を逃がす
書き方が入ると誤検出する。「`t` を引数に取る呼び出しがあれば通す」で緩和できるが、
`t.Helper()` を使わない形は取りこぼす。**検出が弱い方向に倒れるので害は小さいが、値も小さい**。

## 受け入れ条件

- [ ] 候補 A / B を実装する (C は「A / B を入れてから判断」で十分)
- [ ] 各検査は**変異で red を見る**まで確認する (ガードを外す / paths filter を壊す)
- [ ] 0 件を緑にしない (対象が見つからなければ失敗)
- [ ] `scripts/CLAUDE.md` の check_*.sh 一覧表に足す

## 転用しないと判断したもの (次の監査が同じ提案を再生成しないため)

- **`run-shell -b` の無音契約を手動リストから自動発見へ** (111/129): 基準「detach されて長く
  生きるか」は `tests/tmux/test_runshell_silence.sh` 冒頭自身が「機械的には導出できない」と
  書いており、代理指標 (`sleep` を含む) にすると `tmux_agent_panel.sh` (pane 内で描画する
  常駐ループ。出力が目的) が確実に FP になる
- **数値検証ゲートの `[0-9]` 範囲式** (150/151): issue 150 が横展開を実測のうえ却下済み。
  repo 内 25 箇所は全て「非数値 → 安全な既定値」のフォールバックで、die ゲートは 1 箇所のみ。
  用途の区別が静的に付かず FP が支配的。規範は `shell-numeric-gate-explicit-digits.md` が持つ
- **`grep -qE '\t'` の BSD/GNU 方言** (108): 108 自身が静的検査を検討して却下済み。さらに
  issue 133 で Linux を落として CI も macOS 単一になり、発火経路が消えた
- **`/opt/homebrew` のハードコード** (176/140): 残る非 Go の 21 箇所は deny hook の許可パス判定・
  kill ログ・テスト fixture で、**リテラルであること自体が仕様**
