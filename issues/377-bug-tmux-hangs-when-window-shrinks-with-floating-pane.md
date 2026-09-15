# floating pane がある状態で window 幅が大きく縮むと tmux サーバが CPU 100% でハングする

種別: bug / priority: high (操作不能になる。復旧は `kill -9` のみ)
起票: 2026-09-15 / 対象: tmux 3.7b (Homebrew, 2026-07-04 install)

## 症状

画面サイズ変更・AirPlay・外部ディスプレイ抜き差しで client が縮むと、tmux サーバが
**CPU 100% で無応答**になる。`tmux ls` / `display-message` すら返らない (timeout)。
`kill-server` も届かないので復旧は `kill -9` しかない。

## 発火条件 (隔離サーバで実測 2026-09-15)

floating pane (`new-pane -X/-Y`) が 1 つでもあり、縮小後に

- **`pane_left` が新しい window 幅より十分大きい (横方向に完全にはみ出す)** かつ
- **`pane_top` が新しい window 高さより小さい (縦方向は可視範囲と重なる)**

を満たすとハングする。縦にも完全にはみ出す (`pane_top >= 新 window 高さ`) 場合は起きない。
tmux は縮小時に floating pane を clamp せず、作成時の座標のまま残す (実測)。

### 実測マトリクス (200x50 → 80x24 など。判定は `display-message` の round-trip)

| # | floating pane | 縮小 | 結果 |
|---|---|---|---|
| 0 | なし | 200x50 → 80x24 | OK |
| 1 | X156,Y0 44x10 (出力あり) | 200x50 → 80x24 | **HANG** (CPU 99.7%) |
| 11 | X156,Y0 44x10 (出力なし) | 200x50 → 80x24 | **HANG** — pane の出力は無関係 |
| 14 | X160,Y46 44x10 | 200x50 → 80x24 | OK (縦も完全に外) |
| 15 | X156,Y0 40x3 | 200x50 → 80x24 | **HANG** — サイズでなく位置が効く |
| 22 | X60,Y0 40x3 | 200x50 → 80x24 | OK (左端が可視内) |
| 24 | X160,Y47 40x3 (toast 相当) | 200x50 → 100x48 | **HANG** — 横だけ大きく縮むと toast でも踏む |
| 25 | X220,Y0 150x10 (panel 相当) | 370x50 → 80x24 | **HANG** |
| 26 | X220,Y0 150x10 | 370x50 → 257x50 | OK (左端が可視内) |
| 30 | X0,Y0 150x10 (左寄せ panel) | 370x50 → 80x24 | OK |
| 31 | X0,Y47 40x3 (左寄せ toast) | 200x50 → 100x48 | OK |
| 32 | X80,Y0 40x3 | 200x50 → 80x24 | OK (はみ出し量が小さいと踏まない。閾値は未特定) |

ハーネス: `pty.fork` で attach client を作り `TIOCSWINSZ` で SIGWINCH を送る (実 AirPlay と同じ経路)。
隔離サーバ (`-L rsz*` / `-f /dev/null`) で実施、本番 socket は触っていない。

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
- **3.7c の CHANGES にこの修正は入っていない** (実測: 3.7c の CHANGES は jemalloc / scrollbar /
  unzoom-before-floating の 5 件)。つまり `brew upgrade tmux` (3.7b → 3.7c) では直らない
- PR にハング (CPU 100%) の記述はない。ジオメトリ問題としてのみ報告されている

## この repo で floating pane を作るもの

| 機構 | 位置 | 寿命 | 危険度 |
|---|---|---|---|
| `scripts/tmux_agent_panel.sh` (`C-t a`) | X = `win_w - 150`, Y=0 | 常駐 | **高**。ON の間ずっと条件を満たす |
| `bin/tmux-toast` | X = `win_w - box_w`, Y = `win_h - 3` | 2 秒 | 中。表示中の 2 秒に画面が縮むと踏む。split / pane-exited / C-v ペースト / 予約入力 / watchdog から日常的に出る |

2026-09-15 時点の本番サーバは `@agent_panel_on` が未設定 (OFF) で floating pane は 0 件。
**今この瞬間のリスクはゼロだが、panel を ON にすると常時、toast は確率的に踏む。**

## 対応案 (未着手。判断待ち)

1. **右寄せをやめて左寄せにする** (X=0 側)。#30 / #31 で「左端が可視内なら安全」を実測済み。
   upstream の修正を待たずに構造的に回避できるが、見た目が変わる
2. upstream の clamp が入った版へ上げる (3.7d / HEAD ビルド)。3.7c では直らない
3. panel を ON にしない運用を続ける + toast の 2 秒を受容する (現状)
4. upstream へハングとして報告する (PR #5582 のスレッドに実測を出す)

## 残タスク

- [ ] 対応案の選択 (ユーザー判断)
- [ ] ハングする X の下限 (#32 が OK で #15 が HANG の境界) は未特定。回避策を「左寄せ」で採るなら不要
