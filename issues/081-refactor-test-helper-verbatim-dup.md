# 081 refactor: テストヘルパの逐語重複 (nvim headless ラッパ 3 本は既に anchor が drift 済み)

起票日: 2026-08-21
種別: refactor
優先度: **P2** (nvim ラッパの drift は既に起きている実害。他は予防)

出典: 監査 [072](done/072-research-test-audit-2026-08-20.md) の `072-nvim-wrapper-dup` /
`072-nvim-ok-anchor-drift` / `072-tmux-spawn-helper-dup` / `072-default-sock-dup` /
`072-assert-file-exists-dup` / `072-three-own-ok-ng` / `072-toast-set-e-rc`。
**出典 issue には「反証で崩れた (却下)」の一覧がある** — 特に
「`assert_contains` の 8 コピーが fail-fast を壊す」は**反証で崩れている** (5 本すべて
`setopt err_exit` を立てており誤 callsite 0 件)。重複そのものは事実だが、
**「fail-fast が壊れている」を根拠に使わないこと**。

## 確認できた事実 (2026-08-21)

| 重複 | 実数 | 状態 |
|---|---|---|
| nvim headless ラッパ | `tests/nvim/test_smooth_scroll.sh:34` / `test_folds_timer.sh:32` が `grep -q "^OK"`、`test_image_hover.sh:32` は **`grep -q "OK"` (アンカー無し)** | **既に drift 済み**。`tests/nvim/lib/check_log.sh` のヘッダは「コピペの貼り忘れが実 false-pass を起こしたので一元化した」と明記しているのに、この 3 本には適用されていない (同型バグの横展開漏れ) |
| 偽サーバ / 補助プロセス生成 | `tests/tmux/test_periodic_save.sh` (`spawn_helper`) / `test_snapshot_health.sh` (`spawn_helper`) / `test_server_watchdog.sh` (`spawn_fake_server`) | load-bearing な ⚠️ コメント (素の `cmd &` は EXIT trap 継承で TMP_DIR がテスト途中で消える。2026-07-30 に実測特定) ごとコピー。**既に名前・リダイレクト・配列名が 3 者で違う** |
| `DEFAULT_SOCK=` | tests/tmux 5 ファイル | 逐語 |
| `assert_contains` の定義 | tests 配下 8 ファイル | 逐語相当 |
| `assert_file_exists` の定義 | tests 配下 5 ファイル | 逐語相当 |
| 自前 ok/ng | `test_agent_panel.sh` / `test_agent_jump.sh` / `test_tmux_toast.sh` の 3 本のみ共有 lib 非使用 | **設計差がある** (共有 lib は fail-fast、この 3 本は fail を積んで最後に報告) ので素の置換は不可 |

付随: `tests/tmux/test_tmux_toast.sh:168` 等の `out=$(...); rc=$?` は `set -euo pipefail`
(:11) の下なので**代入が失敗した時点で死んで診断が出ない**。同ディレクトリの
`test_extract_popup.sh:56` は共有 lib の `run()` でこの class を既に潰している。

## 下がる複雑性

- 「ヘッダで一元化を宣言している lib があるのに適用されていない」不整合が消える
  (anchor drift はこの形の実害。次の 4 本目でまた貼り忘れる)
- load-bearing なレース回避コメントの touch 箇所が 3→1

## 対応方針 (着手時に再確認)

1. nvim ラッパ 3 本を `tests/nvim/lib/check_log.sh` (または新しい共有ラッパ) へ寄せる。
   **anchor は `^OK` に揃える** (image_hover が緩い側なので、寄せると同時に厳しくなる →
   そのテストが今も通るか確認する)
2. 補助プロセス生成を `tests/tmux/lib/` へ寄せる (`spawn_helper` に統一し、リダイレクトは引数)
3. `DEFAULT_SOCK` / `assert_contains` / `assert_file_exists` を共有 lib へ。
   zsh 系 helper は `setopt err_exit` の前提を壊さないこと (反証で確認済みの契約)
4. 自前 ok/ng の 3 本は共有 lib に accumulate 版 (`expect_called` + `tt_assert_summary`) を
   足してから移行する。fail-fast へ素で寄せると「全 fail を見て直す」運用が壊れる
5. `out=$(...); rc=$?` を共有 `run()` へ寄せる

## 変異検証

集約後、各テストで「本来 red になる変異」を 1 つ当てて red を確認する。特に 1 は
**image_hover のログを `NOT OK` 相当にする変異**で red になること (アンカー無しだと緑)。

## trigger

4 本目の nvim テストを足すとき / tmux の偽サーバ系テストを次に触るとき。
1 (anchor drift) だけは単独で先に閉じてよい (実害が既に発生している側)。
