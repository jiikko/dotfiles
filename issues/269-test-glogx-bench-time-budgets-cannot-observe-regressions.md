# test: glogx の bench 時間予算が緩すぎて退行を観測できない（確保予算だけが効いている）

起票日: 2026-09-05
カテゴリ: test
優先度: 中（ゲートの実効性の問題。今の値でも false red は出ないが、真の回帰も出ない）

## 何が起きているか

`tests/glogx/bench_budgets.ci` の **時間 metric は全 12 件が CI 実測の 16〜125 倍**の上限を
持っており、桁が変わらない限り赤くならない。一方 **確保 (alloc) metric は全 11 件が 1.0〜1.1 倍**
で締まっている。つまり「確保のゲートは効いているが、時間のゲートは実質存在しない」。

実例として、issue 268 の退行（`issues_view_2000` が 1 commit で 2.8 倍）は
**Bench workflow が緑のまま通過した**。

## 全数勘定（CI run 33964922996 / job 101303286129, headSha `eb8d670e`, 2026-09-05T12:04Z）

較正器 `glogx_calib` は 0.089（基準 0.15 を下回る = 静穏 run、rel のスケールは 1 倍）。

### 時間 metric（余裕 = 予算 / CI 実測）

| metric | CI 実測 (ms) | 予算 | 余裕 |
|---|---:|---:|---:|
| `view_diff` | 0.040 | 5 rel | **125.0x** |
| `view_steady` | 0.031 | 3 rel | 96.8x |
| `view_panel` | 0.033 | 3 rel | 90.9x |
| `model_init_200` | 0.119 | 10 rel | 84.0x |
| `status_view_2000` | 0.038 | 3 rel | 78.9x |
| `cursor_move_view` | 0.064 | 5 rel | 78.1x |
| `issues_view_frame` | 0.027 | 2 rel | 74.1x |
| `status_view_frame` | 0.035 | 2 rel | 57.1x |
| `glogx_calib` | 0.089 | 5 abs | 56.2x |
| `issues_view_2000` | 0.074 | 3 rel | **40.5x** |
| `issue_scan` | 1.005 | 25 rel | 24.9x |
| `render_large_patch` | 0.623 | 10 rel | **16.1x**（最も締まっている） |

中央値はおよそ **75 倍**。最も締まっている `render_large_patch` でも 16 倍。

### 確保 metric

11 件すべて **1.0〜1.1 倍**（`issue_scan_alloc_kb` の 1.1 倍が最大）。
これは「絶対値 +3%」の運用（`tests/glogx/bench_budgets.ci:43` が根拠を持つ）どおりで、
**こちらは機能している**。

🚨 **これは対称な問題ではない**。確保側は `issues/done/051-perf-glogx-bench-gates-time-only.md`
（2026-08-14）で「時間しか見ていない」と起票され、実際に `-benchmem` を取って
`*_alloc_kb` metric を足し、**実測 +3% の上限まで締めるところまでやり切られている**。
時間側は最初から緩いまま一度も締められていない。051 で足した側だけが今も効いている、
という非対称がこの issue の中身。

## 発火条件

- 桁が変わらない性能退行（1.5〜10 倍程度）は、時間予算をすり抜ける
- **確保を伴わない退行**は特にすり抜ける。issue 268 の退行は alloc が 213 → 215 allocs
  （+0.9%）でしかなく、確保予算（余裕 1.0 倍）でも観測できなかった。時間だけが 2.8 倍
- **silent**: Bench workflow は success を返す。誰も見なければ気づかない

## 予算ファイル自身が「締めろ」と書いている

`tests/glogx/bench_budgets.ci` のコメントは 3 箇所で同じことを言っている:

- 冒頭: 「CI (ubuntu-slim) の実測がまだ無いため『桁級の回帰だけ捕まえるログ観測フェーズ』として
  緩く始める。**CI の Step Summary に実測が数 run 溜まったら、その ~4 倍を目安に締めること**」
- 体感系 (2026-07-29 追加): 「既存と同じ ~20 倍の粗い上限で導入し、**CI 実測が溜まったら
  ~4 倍を目安に締める**」
- 較正器: 「基準 0.15 は…暫定値。**CI の静穏 run 実測が出たらその値へ取り直すこと**」

CI 実測はもう溜まっている（Bench workflow は push ごとに回っている）。
**締める作業だけが行われていない**。issue 268 がその代償。

## 推奨対応

1. **時間予算を CI 実測の ~4 倍へ締める**（予算ファイル自身が指定している方針）。
   上の表の「CI 実測」列 × 4 が新しい上限の目安。例:
   `issues_view_2000 3 rel` → `0.3 rel` / `view_steady 3 rel` → `0.15 rel` /
   `render_large_patch 10 rel` → `2.5 rel`
   - 1 run の値で決めない。**静穏 run（`glogx_calib` が基準以下）を数本集めて中央値**を取る。
     現在は Step Summary に溜まっているのでそこから拾える
2. **較正器の基準 `calibrate glogx_calib 0.15` を静穏 run の実測へ取り直す**。
   スケールは `check_bench_budgets.sh:131` が `s = 実測 / 基準; if (s < 1) s = 1` で出す
   （実装で確認）。静穏 run の実測が 0.089 なのに基準が 0.15 だと、**混雑が約 1.7 倍に
   達するまでスケールが 1 のまま**で補正が効かない。今は上限が 40〜125 倍緩いので誰も
   困らないが、1 で締めた途端に「補正されない中程度の混雑」が false red になる。
   **取り直しは 1 と同じ変更で行うこと**
3. **締めたことを一度検証する**: issue 268 の退行 commit (`fc0f65ab`) をそのまま CI に
   かけて `issues_view_2000` が赤くなるかを見る（`~/.claude/rules/mutation-verify-new-tests.md`
   の「変異を当てて red を見る」の予算版）。赤くならないなら締め方が足りない

## 却下した案

- **時間予算を全部やめて確保予算だけにする**: 採らない。issue 268 の退行は alloc が
  ほぼ不変（+0.9%）だったので、確保予算だけでは構造的に見えない。時間の退行は
  時間でしか観測できない
- **絶対値でなく前回 run との差分でゲートする**: 採らない。shared runner の混雑は
  乗法的に乗る（予算ファイル冒頭の実測が根拠）ので、隣接 run の差分は混雑ノイズに埋もれる。
  較正器つきの rel はその対策として既に入っており、**問題は仕組みではなく上限値**

## 関連

- 268 / 270（このゲートがすり抜けさせた、または視界に入っていない実際の退行）
- `issues/done/051-perf-glogx-bench-gates-time-only.md`（確保側を締め切った前例。
  本 issue はその時間側）
- `tests/glogx/bench_budgets.ci:43`（確保予算を「実測 +3%」で締める根拠。時間側には
  対応する記述が無い）
- `tests/check_bench_budgets.sh`（rel / calibrate の意味の正本）
- `~/.claude/rules/verify-execution-not-just-exit-code.md`「自作の計測に予算を付けるなら
  『その計測が構造的に見落とすもの』を先に書く」/ `perf-claims-need-measurement.md`
