# nvim / tmux / zsh / glogx 系の変更を push したら、Bench をウォッチしてデグレ確認までがタスク

## ルール

- **以下のパスに触れる変更を push したら、その commit の CI (Bench workflow を含む全 run) の
  完了を watch し、デグレしていないことを確認してからタスク完了を報告する**。push して
  「CI はそのうち通るはず」で終わらない
  - 対象パス: `_nviminit.lua` / `nvim/` / `_tmux.conf` / `scripts/tmux_*` / `scripts/lib/tmux_*` /
    `zshlib/` / `_zshrc` / `bin/tmux-toast` / `src/glogx/` / `vendor/nvim-plugins/` /
    `vendor/tmux-plugins/` / 各 bench スクリプトと budgets
- 測定結果の正本は 2 箇所 (どちらも tests/run_bench.sh が出力):
  - **人間向け**: 各 Bench run の Step Summary (markdown。run ページの Summary タブ)
  - **機械向け**: ジョブログの `metric=<name> ms=<value>` 行。
    `gh run view <run-id> --log | grep 'metric='` で取得する (Step Summary は API 非公開のため、
    ログ出力が CLI で数値比較できる唯一の経路。run_bench.sh 側にその旨のコメントあり)
- **予算 green でも数値は一瞥する**: 直前の master の Bench run と同 metric を比べ、
  1.5 倍超の悪化があれば予算内でも報告に含める (予算は桁級回帰の安全網で、
  微デグレの追跡はこの経時比較が担う。.github/workflows/bench.yml ヘッダ参照)

## デグレしていたときの対処

1. **真のデグレ (意図しないコスト増)** → 「効果がなかった修正は revert」の原則
   (CLAUDE.md「不具合対応の原則」) に従い、原因 commit を特定して revert または修正。
   直るまでタスク完了を報告しない
2. **意図したコスト増 (機能追加の対価)** → budgets ファイル (`tests/*/bench_budgets.ci`) を
   CI 実測ベースで追従させる。予算の根拠コメント (実測値・margin) も同時に更新する
   (追従漏れで master が赤くなった実例が bench.yml ヘッダにある)
3. **flake の疑い (共有 runner の混雑)** → Bench だけ再実行 (workflow_dispatch / rerun)。
   min-of-3 + 混雑補正 (startup の rel) を突き破る単発ノイズは稀なので、再実行でも赤なら
   flake と断定せず 1/2 に戻る

## watch の実務

- run の特定と失敗ログは `bin/ci-log` ([use-ci-log-for-ci-inspection.md](use-ci-log-for-ci-inspection.md))。
  完了待ちは Monitor 等で `gh run list --commit <sha>` の全 run completed をポーリングする
- ホットパス (hook 経路・描画ループ・起動列) に触る規模の変更は push 前にローカルでも
  `tests/nvim/bench_nvim.sh` (BENCH_BASELINE=1) / `tests/tmux/bench_tmux.sh` を回し、
  実測の変化を commit メッセージに残す

## なぜ

パフォーマンス回帰は機能テストでは捕まらず、放置すると「いつからか遅い」だけが残って
原因 commit の特定コストが跳ね上がる。Bench workflow は push ごとに予算ゲートを回している
ので、**push 直後に watch して原因 commit が 1 つに絞れている瞬間に検知する**のが最安。
この repo はレイテンシ最適化 (REPLY 契約 / fold 凍結 / fork 削減) に投資してきており、
その資産を守る回帰ゲートの運用側がこのルール。

## 関連

- [use-ci-log-for-ci-inspection.md](use-ci-log-for-ci-inspection.md) — CI ログ確認の一本化 (本ルールの下請け)
- `tests/run_bench.sh` — 3 回実行 → min 集約 → Step Summary + ログ出力 → 予算チェックの実体
- `docs/feedback-nvim-tmux-2026-07-29.md` — 実測値の基準 (2026-07-29 時点のローカル値)
