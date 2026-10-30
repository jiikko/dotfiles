# 998 (perf): glogx の maybeTick が、張ってある 80ms の tick の間に始まった動きを待たせる

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-30〜）

起票日: 2026-10-30

## 概要

glogx の `maybeTick` (src/glogx/browse_anim.go) は tick を bool の single-flight で 1 本に束ね、周期を張った時点に決める。80ms の
スピナーの tick が張ってある間に、filer (`F`)・scroll glide・toast・スライドの動きが始まると、動きの次のフレームがその tick
(最大 80ms 後) まで待たされる。treefiler の main.go の同じ形は issue 995 で実測し (直す前は p95 49〜75ms・最大 80ms、直した後は最大 22ms)、
tuikit の `tickchain` に寄せて直した。glogx は同じ形だが、**glogx での遅れは未実測** (tools/tfperf は bin/treefiler 用)。

## 対応方針

- `maybeTick` / `onTick` / `tickMsg` を `tickchain.Chain` に寄せる (`Want(app.TimeNow(), m.tickInterval())`・tickMsg に seq)
- 🚨 treefiler と違い、glogx の onTick は**フレームの数で進む動き**を持つ (pull で増えた行を 1 フレームに 1 行降らせる・glide のフレーム数)。
  追い越された古い tick (`Fired` が false) は捨てる (処理すると、それらの動きが余分に 1 歩進む)
- テストの書き換え: `m.ticking` を直接読み書きする 15 か所 (autobuild_test・cli_health_test・filer_view_test・scroll_glide_test・
  toast_integration_test・tui_nav_test) と、`tickMsg{}` を流してアニメを進める 22 か所。古い tick を捨てると `tickMsg{}` (seq 0) は
  捨てられるので、張ってある seq の tick を流す helper に寄せる
- 測るなら: glogx に perflog の配線を足すか、filerview のテストで「遅い tick の間の打鍵で次の tick が frameInterval 以内に張られる」を見る
