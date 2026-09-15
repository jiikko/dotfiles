# floating pane がある状態で window が縮むと tmux サーバが CPU 100% でハングする

種別: bug / priority: high (操作不能になる。復旧は `kill -9` のみ)
起票: 2026-09-15 / 対象: tmux 3.7b (Homebrew, 2026-07-04 install)

## 症状

画面サイズ変更・AirPlay・外部ディスプレイ抜き差しで client が縮むと、tmux サーバが
**CPU 100% で無応答**になる。`tmux ls` / `display-message` / `send-keys` すら返らない。
`kill-server` も届かないので復旧は `kill -9` しかない。

## 前提: floating pane は clamp されず、window も client まで縮まない

tmux 3.7b は縮小時に floating pane (`new-pane -X/-Y`) を移動も縮小もせず、作成時の座標のまま残す。
さらに **window 自身も client 幅まで縮まない**。実測 9 ケースすべてで

```
縮小後の window 幅 = max(client 幅, 元の window 幅 - (floating pane の幅 - 1))
```

(例: 200 桁で幅 40 の pane → client 80 でも window は 161 桁のまま。client はその一部しか映さない)

この「window が縮みきらない」だけでも実害がある (画面の右側が見えない) が、本題は下記のハング。

## 発火条件 (実測。単純な閾値ではない)

**横**: `pane_left` が **client 幅**より数セル大きいと発火する。client 幅と同じ (+0) / +1 では発火せず、
+5 以上はすべて発火した (閾値は +2〜+5 の間。未特定)。
**縦**: pane が縦方向に**大きく**外れていれば発火しない (#14 / #16)。ただし 1 行外れただけでは
発火する (#24: window 高さ 47 に対し `pane_top` 47)。

🚨 **当初 issue に書いた「`pane_left` が新しい window 幅を超える」は誤り**だった。実測を取り直すと
**ハングした全ケースで `pane_left` < window 幅**で、window 幅は上の式のとおり client まで縮んでいない。
完全な条件は upstream の実装を読まないと確定しない。**実務上確実なのは「右寄せの floating pane は
危険、左寄せ (X=0) は測った全ケースで無事」**という点。

### 実測マトリクス

ログ: `tmp/377-verify.log` (gitignore。結論は本 issue が正本)。
各ケースで ①`new-pane` の rc ②縮小**後**の window / pane ジオメトリ ③毎秒の redraw 強制
(`send-keys` で `CSI 2J`) を記録している。判定は `display-message` の round-trip
(`timeout 3` + Python 側 `timeout 6` の二重待ち。パイプ待ちで固まらないようにする)。

| # | floating pane | 縮小 (client) | 縮小後 window | 結果 |
|---|---|---|---|---|
| 22 | X60,Y0 40x3 | 80x24 | 161x23 | OK (pane_left < client 幅) |
| 32 | X80,Y0 40x3 | 80x24 | 161x23 | OK (+0) |
| 50 | X81,Y0 40x3 | 80x24 | 161x23 | OK (+1) |
| 51 | X90,Y0 40x3 | 80x24 | 161x23 | **HANG** (+10, CPU 100%) |
| 80 / 81 | X85 / X88,Y0 40x3 | 80x24 | — | **HANG** (+5 / +8) |
| 14 | X160,Y46 44x10 | 80x24 | 157x23 | OK (縦に大きく外れている) |
| 16 | X99,Y46 100x3 | 80x24 | 101x23 | OK (同上) |
| 24 | X160,Y47 40x3 | 100x48 | 161x47 | **HANG** (縦は 1 行外れただけ) |
| 90 | X160,Y0 40x3 | 100x48 | 161x47 | **HANG** (#24 の縦だけ変えた対照) |
| 25 / 42 | X220,Y0 150x10 (panel 相当 / **実物の `tmux_agent_panel.sh render`**) | 80x24 | 221x23 | **HANG** 両方 |
| 26 | X220,Y0 150x10 | 257x50 | — | OK (pane_left < client 幅) |
| 63 | X0,Y0 150x10 (左寄せ panel) | 80x24 | 221x23 | OK |
| 82 | X0,Y0 300x10 (左寄せ・幅は client の 3.75 倍) | 80x24 | 80x23 | OK |
| 0 | なし | 80x24 | — | OK |

- **pane の出力量は無関係** (`sleep` だけの pane でも発火。#11)
- **redraw を毎秒強制しても OK 側は OK のまま** (trigger 8/8 成功、CPU 0%)。「何も起きなかっただけ」ではない
- `display-popup` (glogx / fzf / 確認ダイアログ) は **安全**。popup 起動を canary (マーカーファイル) で
  確認し、縮小の実効も確認したうえで 3 条件すべて OK

ハーネス: `pty.fork` で attach client を作り `TIOCSWINSZ` で SIGWINCH を送る (実 AirPlay と同じ経路)。
隔離サーバ (`-L vrf*` / `-f /dev/null`) で実施、本番 socket は触っていない。
**限界**: #42 以外は `sleep` の手製 pane で、本番の conf (resurrect / continuum / hook) は読ませていない。
マトリクスが確定させたのは tmux 側の挙動であって、この repo のスクリプトの挙動ではない。

## 原因 (ハング中の `sample` から)

```
window_pane_read_callback → input_parse → input_csi_dispatch
  → screen_write_clearendofscreen → screen_write_collect_flush
  → screen_redraw_get_visible_ranges → server_client_ensure_ranges → xrecallocarray
    → recallocarray → memmove (2030) / bzero (279) / free_medium→madvise (247)
```

- pane が **CSI J (ED) を書いたとき**に通る経路。redraw が trigger
- **「確保が発散する」わけではない**: `Physical footprint 34.3M / peak 41.4M` で頭打ちで、内訳は
  medium ゾーンの malloc→全体 memmove→free の往復。**有界だが大きい配列を再確保し続けている**
- 「終わらない」の根拠は sample 1 本ではなく、**起動から約 2 分 52 秒後も同じ状態**だったことと、
  復旧が `kill -9` しかなかったこと
- このスタックは floating pane **2 枚** (X160,Y46 40x3 + X156,Y0 44x10) の構成で採った。
  マトリクスの各行 (1 枚) とは別構成である点に注意

## upstream

- [PR #5582 "Keep floating panes inside the window when it shrinks"](https://github.com/tmux/tmux/pull/5582)
  が **2026-09-11 に OpenBSD へ適用済み** (GitHub 側へは後日)。clamp が入れば発火条件自体が消える
- **3.7c の CHANGES にこの修正は入っていない** (`raw.githubusercontent.com/tmux/tmux/3.7c/CHANGES` で確認。
  jemalloc / scrollbar / message-format / unzoom-before-floating の 5 件のみ)。`brew upgrade tmux` では直らない
- PR にハング (CPU 100%) の記述はない。**ジオメトリ問題としてのみ報告されている**

## この repo で floating pane を作るもの

| 機構 | 位置 | 寿命 | 危険度 |
|---|---|---|---|
| `scripts/tmux_agent_panel.sh` (`C-t a`) | X = `win_w - 150`, Y=0 | 常駐 | **高**。ON の間ずっと条件を満たす (#42 で実物を実測) |
| `bin/tmux-toast` | X = `win_w - box_w`, Y = `win_h - 3` | 既定 2 秒 | 中。#24 で発火を実測。`after-split-window` / `pane-exited` / C-v ペースト / `tmux_schedule_keys.sh` / `tmux_server_watchdog.sh` から出る。watchdog は無人でも出る |
| `prefix + *` (tmux 3.7 の既定バインド。この repo は unbind していない) | 既定 X=4, Y=2 / 100x12 | 手動 | 低。左上なので単独では安全側。ただし floating pane はマウスで移動できるので右へ寄せると同条件 |

2026-09-15 時点の本番サーバは `@agent_panel_on` が未設定 (OFF) で floating pane は 0 件。
**panel を ON にすると常時、toast は出ている間だけ確率的に踏む。**

## 対応案 (未着手。判断待ち)

1. **右寄せをやめて左寄せにする** (X=0 側)。#63 / #82 で実測済み (#82 は幅が client の 3.75 倍でも無事)。
   根拠は観測行だけでなく不変条件でもある (`pane_left = 0` は常に client 幅未満)。見た目は変わる
2. upstream の clamp が入った版へ上げる (3.7d / HEAD ビルド)。3.7c では直らない
3. panel を ON にしない運用を続け、toast の露出を受容する (現状)
4. upstream へハングとして報告する (PR #5582 のスレッドに実測を出す)

## 敵対レビューで潰した論点 (再提出を防ぐ記録)

- **「`new-pane` の rc を捨てているので OK 行は pane が作られていなかったのでは」** → 却下。
  再実測で全ケース `rc=0`、`list-panes` にも floating pane が出ている
- **「OK 行は redraw が来なかっただけでは」** → 却下。毎秒 `CSI 2J` を強制しても OK 側は 8/8 で無事
- **「ケース間で残骸が汚染しているのでは」** → 却下。ケースごとに別 socket・別サーバで、
  サーバ pid を名指しで `kill -9` して socket を消している。実行後の残骸は 0 件
- **「drain スレッドが詰まって偽ハングでは」** → 却下。HANG 側も `drain生存=True`、CPU 99〜100%
- **「発火条件が実測に合っていない」** → **採用**。条件を上記のとおり全面的に書き直した

## 残タスク

- [ ] 対応案の選択 (ユーザー判断)
- [ ] 横の閾値 (+2〜+5 の間) と、縦が「1 行外」で発火し「大きく外」で発火しない境界は未特定。
      対応案 1 を採るなら不要
