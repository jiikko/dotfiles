# tmux shim の「値取りグローバルオプション集合」の完全性を回帰チェックで固定する

起票日: 2026-09-11
出典: [issue 355](355-retro-tmux-production-kill-guard-2026-09-11.md) の残タスク（任意項目）。
最終ゲートの敵対的レビューが load-bearing 前提として挙げたもの
関連: `docs/tmux-production-kill-guard.md` の「load-bearing な前提」節（前提の一次情報）

## 何が前提になっているか

`bin/tmux`（本番 tmux サーバへの `kill-server` / `kill-session` を非対話シェルから拒否する shim）は、
コマンド列を解析するときに**値を 1 つ読み飛ばす**グローバルオプションを `-c -f -L -S -T` の
**5 個だけ**と決め打っている。この集合が tmux の実際の usage と一致していることが前提。

## 破れたときに何が起きるか

**将来の tmux が値取りグローバルオプションを 1 つ足すと、`;` を一切使わずに無音の under-block が開く。**

`tmux -X <値> kill-server` のような形で、shim が `<値>` を subcommand 位置と誤認する。
`<値>` は kill ではないので「kill ではない」と判定され、**後続の `kill-server` に到達しない**。
`;` による連鎖の検出（3〜5 周目で塞いだ経路）とは別の入口なので、既存のテストは 1 本も落ちない。

これは**例外にならず、拒否もされず、ただ本番サーバが死ぬ**形。しかも tmux を上げた日には
何も起きず、次に誰かがそのオプションを使ったときに初めて発火する。

## やること

`tmux` の usage 行から値取りグローバルオプションを機械的に抽出し、`bin/tmux` の集合と
突き合わせる検査を新設する。**`bin/tmux` 自体は変更しない**（shim の実装は 6 周の敵対的レビューを
通して確定しており、この issue は「前提が崩れたら気づく」層を足すだけ）。

## 受け入れ条件

- [x] `tmux` の usage から値取りグローバルオプションを抽出し、`bin/tmux` の読み飛ばし集合と
      比較する検査スクリプトを足す（`make test` の自動発見に乗る場所へ）
- [x] **抽出が空でも緑にならないこと**を確認する。usage の書式が変わって 0 個抽出になったら
      「一致」ではなく**失敗**にする（[`verify-execution-not-just-exit-code.md`](../../_claude/rules/verify-execution-not-just-exit-code.md)
      の「対象 0 件 / 抽出 0 件は失敗にする」）
- [x] **canary を本走査と同じ関数に通す**。既知の入力（`-L` は値取り / `-2` は値を取らない）で
      既知の答えが出ることを本走査の前に固定する
- [x] **変異検証**: `bin/tmux` の集合から 1 個抜く変異と、集合に架空の 1 個を足す変異の
      **両方**で red を見る（片側だけだと「集合を読んでいない検査」でも緑になる）
- [x] 現在の tmux のバージョンを検査の出力に出す（`docs/tmux-production-kill-guard.md` は
      3.7b で実測したと記録している。どの版で一致したのかが後から分かるように）
- [x] `docs/tmux-production-kill-guard.md` の「load-bearing な前提」節に、
      この検査が前提を守るようになったことを 1 行足す

## やらないこと

- `bin/tmux` / `_claude/hooks/deny-bare-tmux-kill.sh` の変更（実装は確定済み）
- tmux の usage の書式に依存しない汎用パーサの新設（過剰。書式が変われば検査が落ちて気づける形で足りる）

## 進捗 (2026-09-11)

- `tests/tmux/test_tmux_shim_value_opts.sh` を新設（commit: test(tmux): shim の値取りグローバルオプション集合を tmux の usage と突き合わせる）。
  - 抽出は `usage_value_opts()` / `shim_value_opts()` の 2 関数に寄せ、**canary と本走査が同じ関数を通る**。
  - 抽出 0 件はどちらの側も `bad`（失敗）にする。
  - `LC_ALL=C sort` で照合順を固定（既定ロケールでは `cfLST`、C では `LSTcf` になり比較が揺れるため）。
- `docs/tmux-production-kill-guard.md` の「load-bearing な前提」と「検証」節に、この検査が前提を守る旨を追記。

## 結果 (実測)

- tmux 3.7b (`/opt/homebrew/bin/tmux`) の usage から抽出した集合 = `LSTcf`、`bin/tmux` の読み飛ばし集合 = `LSTcf`。**一致**。
- 検査 3 件（canary 2 + 本走査 1）、fail=0。
- 変異検証（`bin/tmux` のみを変異。`bash -n` で構文が通ることを確認してから判定）:
  - 変異A `-f|-c|-T)` → `-f|-c)`（集合から 1 個抜く）: **red** (fail=1)
  - 変異B `-f|-c|-T)` → `-f|-c|-T|-Q)`（架空の 1 個を足す）: **red** (fail=1)
  - どちらも canary 2 件は緑のまま = red を出したのは本走査の突き合わせ。

## 残タスク

- なし（`bin/tmux` は変更していない。やらないこと節の 2 件も守った）。
