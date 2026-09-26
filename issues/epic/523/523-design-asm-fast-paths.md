# 523 (design): asm 対応 — 端末文字列の走査を共通の lib に寄せてから、CPU 固有の速い道 (arm64 NEON) を入れる

起票日: 2026-09-26

## 概要

ユーザーの依頼 (2026-09-26): 「asm 対応っていう group issue を作成して。まず src の下に共通パッケージに切り出して〜みたいなことが必要だと思う」。

asm の速い道を入れる場所は 1 つにしたい。そのために、端末文字列の幅・切り詰め・切り出しを**共通の lib に寄せてから**、
その lib の中にだけ arm64 の実装を置く。

## 今どうなっているか (2026-09-26 に確かめた)

- 共通の lib は既にある: `src/tuikit/termwidth` (glogx・ratelimit・doctor が使う。`Of` / `fastDispWidth`)
- 寄っていないもの: `x/ansi` を直接呼ぶ `ansi.StringWidth` / `Cut` / `Truncate` が **pro-con に 44 か所、schedkeys に 25 か所** (tuikit の中 22・glogx 9・ratelimit 1)
- arm64 の実装は既にある (master に入っていない): remote の `claude/glogx-fastdispwidth-asm-7gv00g` に `termwidth` の NEON 版 (7 ファイル・約 350 行)。
  一致の担保 (境界の全位置 5600 入力・差分 fuzz 62 万 exec で不一致 0・変異 3 種が red) と実測がある:
  - 単体 (macos-15 の M1 / go1.25.0 / benchstat 中央値): ascii_80B 51.2 → 6.56ns (-87%)・ascii_1KB -92%・box_120 -46%・cjk_reject -42%・sgr_commit_line -25%
  - **glogx の 1 フレーム: geomean -7.4%、全項目で有意差なし** (520 の受け入れ条件「実画面で 10% 以上」に届いていない)

## 進め方 (子 issue)

1. [524](524-refactor-route-terminal-width-through-termwidth.md): pro-con と schedkeys の幅・切り詰め・切り出しを `termwidth` に寄せ、同じ行を何度も走査している所を 1 回にする (pure Go。asm は入れない)
2. 測り直す: 1 の後で、glogx と pro-con の実画面のフレームで、`termwidth` の走査が CPU の何 % かを見る (520 の Phase 0)
3. [520](520-perf-arm64-terminal-line-scanner.md): 2 で走査が有意に残っていれば、上のブランチの NEON 版を起点に入れる (一から書かない)。
   実画面で 10% 以上短くならなければ入れず、理由を書いて閉じる

- 寄せただけで十分速くなれば、asm は入れない (520 の方針どおり)
- 新しい lib は立てない (`termwidth` に「切る・詰める」の API を足す)。別の lib が要ると分かったら、そのときに決める

## 関連

- 046 (glogx の dispWidth の速い道) / 494 (pro-con の演出のフレーム) / 513 (pro-con の性能の監査)
