# floating pane がある状態で window 幅が大きく縮むと tmux サーバが CPU 100% でハングする

種別: bug / priority: high (操作不能になる。復旧は `kill -9` のみ)
起票: 2026-09-15 / 対象: tmux 3.7b (Homebrew, 2026-07-04 install)

## 症状

画面サイズ変更・AirPlay・外部ディスプレイ抜き差しで client が縮むと、tmux サーバが
**CPU 100% で無応答**になる。`tmux ls` / `display-message` すら返らない (timeout)。
`kill-server` も届かないので復旧は `kill -9` しかない。

## 発火条件 (隔離サーバで実測 2026-09-15)

tmux は縮小時に floating pane (`new-pane -X/-Y`) を clamp せず、作成時の座標のまま残す。
残った pane が

- **`pane_left` が新しい window 幅を 10 セル以上超える** (+1 では発火しない。閾値は +2〜+9 の間で未特定) かつ
- **`pane_top` が新しい window 高さより小さい** (縦方向は可視範囲と重なる)

を満たすとハングする。縦にも完全にはみ出す場合は起きない。
**左端が可視内 (`pane_left` < 新 window 幅) なら、右へ 220 セルはみ出していても安全** (#52)。

### 実測マトリクス

判定は `display-message` の round-trip (パイプを使わず `timeout 3` + `subprocess` の二重待ち)。
縮小は 200x50 → 80x24 (注記のあるものを除く)。`+N` は縮小後の window 右端からのはみ出し量。

| # | floating pane | 結果 |
|---|---|---|
| 0 | なし | OK |
| 22 | X60,Y0 40x3 (左端可視) | OK |
| 33 | X79,Y0 40x3 (+-1) | OK |
| 32 | X80,Y0 40x3 (+0) | OK |
| 50 | X81,Y0 40x3 (+1) | OK (30 秒観測。遅延発火なし) |
| 51 | X90,Y0 40x3 (+10) | **HANG** (CPU 99.7%) |
| 40 | X100,Y0 40x3 (+20) | **HANG** |
| 15 | X156,Y0 40x3 (+76) | **HANG** |
| 1 / 11 | X156,Y0 44x10 (出力あり / 出力なし) | **HANG** 両方 — pane の出力量は無関係 |
| 14 | X160,Y46 44x10 (縦も完全に外) | OK |
| 24 | X160,Y47 40x3 → 100x48 (縦は可視・横だけ大きく縮む) | **HANG** — toast の形でも踏む |
| 25 | X220,Y0 150x10 / 370x50 → 80x24 (panel 相当) | **HANG** |
| 42 | 同上だが **実物の `tmux_agent_panel.sh render`** を走らせる | **HANG** |
| 26 | X220,Y0 150x10 / 370x50 → 257x50 (左端可視) | OK |
| 52 | X0,Y0 300x10 / 370x50 → 80x24 (左寄せ・右へ +220) | OK (30 秒観測) |
| 63 | X0,Y0 150x10 / 370x50 → 80x24 (左寄せ panel) | OK (30 秒観測) |

ハーネス: `pty.fork` で attach client を作り `TIOCSWINSZ` で SIGWINCH を送る (実 AirPlay と同じ経路)。
隔離サーバ (`-L rsz*` / `-f /dev/null`) で実施、本番 socket は触っていない。
**限界**: #42 以外は `sleep` / `printf` の手製 pane で、本番の conf (resurrect / continuum / hook) は
読ませていない。マトリクスが確定させたのは tmux 側の挙動であって、この repo のスクリプトの
挙動ではない。ただし #42 で実 `render` (2 秒ごとに `display-message` を叩く常駐ループ) を
本番ジオメトリで走らせても同じくハングすることは確認済み。

## 原因 (ハング中の `sample` から確定)

```
input_csi_dispatch → screen_write_clearendofscreen → screen_write_collect_flush
  → screen_redraw_get_visible_ranges → server_client_ensure_ranges → xrecallocarray
    → recallocarray → memmove/bzero  (延々と回り続ける)
```

window 外の floating pane に対する可視範囲の配列確保が収束しない。

## upstream

- [PR #5582 "Keep floating panes inside the window when it shrinks"](https://github.com/tmux/tmux/pull/5582)
  が **2026-09-11 に OpenBSD へ適用済み** (GitHub 側へは後日)。clamp が入れば発火条件自体が消える
- **3.7c の CHANGES にこの修正は入っていない** (実測: jemalloc / scrollbar / message-format /
  unzoom-before-floating の 5 件のみ)。`brew upgrade tmux` (3.7b → 3.7c) では直らない
- PR にハング (CPU 100%) の記述はない。ジオメトリ問題としてのみ報告されている

## この repo で floating pane を作るもの

| 機構 | 位置 | 寿命 | 危険度 |
|---|---|---|---|
| `scripts/tmux_agent_panel.sh` (`C-t a`) | X = `win_w - 150`, Y=0 | 常駐 | **高**。ON の間ずっと条件を満たす (#42 で実測) |
| `bin/tmux-toast` | X = `win_w - box_w`, Y = `win_h - 3` | 2 秒 | 中。表示中に画面が縮むと踏む (#24)。`after-split-window` / `pane-exited` / C-v ペースト / `tmux_schedule_keys.sh` / `tmux_server_watchdog.sh` から出る。watchdog は無人でも出る |

2026-09-15 時点の本番サーバは `@agent_panel_on` が未設定 (OFF) で floating pane は 0 件。
**panel を ON にすると常時、toast は出ている 2 秒間だけ確率的に踏む。panel が OFF の今、
生きている露出は toast 経路。**

## 対応案 (未着手。判断待ち)

1. **右寄せをやめて左寄せにする** (X=0 側)。#52 / #63 で「左端が可視内なら、右へ大きくはみ出していても
   30 秒間安全」を実測済み。upstream を待たずに構造的に回避できるが、見た目が変わる
2. upstream の clamp が入った版へ上げる (3.7d / HEAD ビルド)。3.7c では直らない
3. panel を ON にしない運用を続け、toast の露出を受容する (現状)
4. upstream へハングとして報告する (PR #5582 のスレッドに実測を出す)

## 残タスク

- [ ] 対応案の選択 (ユーザー判断)
- [ ] ハングする閾値の正確な値 (+2〜+9 の間) は未特定。回避策を「左寄せ」で採るなら不要
