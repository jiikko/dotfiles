# 523 (design): asm 対応 — 端末文字列の走査を共通の lib に寄せてから、CPU 固有の速い道 (arm64 NEON) を入れる

起票日: 2026-09-26

## 概要

ユーザーの依頼 (2026-09-26): 「asm 対応っていう group issue を作成して。まず src の下に共通パッケージに切り出して〜みたいなことが必要だと思う」。

asm の速い道を入れる場所は 1 つにしたい。そのために、端末文字列の幅・切り詰め・切り出しを**共通の lib に寄せてから**、
その lib の中にだけ arm64 の実装を置く。

## 今どうなっているか (2026-09-26 に確かめた)

- 共通の lib は既にある: `src/tuikit/termwidth` (glogx・ratelimit・doctor が使う。`Of` / `fastDispWidth`)
- 寄っていないもの: `x/ansi` を直接呼ぶ `ansi.StringWidth` / `Cut` / `Truncate` が **pro-con に 44 か所、schedkeys に 25 か所** (tuikit の中 22・glogx 9・ratelimit 1)
- arm64 の実装は **2026-09-26 に master へ入った** (PR #14、`a413f1ef`。ブランチ `claude/glogx-fastdispwidth-asm-7gv00g`)。`termwidth` の NEON 版 (`fastwidth_arm64.go` / `.s`。7 ファイル・約 350 行)。
  一致の担保 (境界の全位置 5600 入力・差分 fuzz 62 万 exec で不一致 0・変異 3 種が red) と実測がある:
  - 単体 (macos-15 の M1 / go1.25.0 / benchstat 中央値): ascii_80B 51.2 → 6.56ns (-87%)・ascii_1KB -92%・box_120 -46%・cjk_reject -42%・sgr_commit_line -25%
  - **glogx の 1 フレーム: geomean -7.4%、全項目で有意差なし** (520 の受け入れ条件「実画面で 10% 以上」に届いていない)
  - 本物の Apple Silicon (この Mac、darwin/arm64、2026-09-26。`go test -bench BenchmarkFastDispWidth -count 3` の中央値。benchstat ではない粗い値):
    ascii_1KB 474 → 35.8ns (-92%)・ascii_80B 38.4 → 6.6ns (-83%)・box_120 491 → 220ns (-55%)・cjk_reject 26.6 → 16.1ns (-39%)・sgr_commit_line 55.6 → 41.0ns (-26%)。
    `go test ./termwidth/` も green。実画面のフレームは本物の arm64 ではまだ測っていない

## 進め方 (子 issue)

1. [524](done/524-refactor-route-terminal-width-through-termwidth.md): pro-con と schedkeys の幅・切り詰め・切り出しを `termwidth` に寄せ、同じ行を何度も走査している所を 1 回にする (pure Go。asm は入れない)
2. 測り直す: 1 の後で、glogx と pro-con の実画面のフレームで、`termwidth` の走査が CPU の何 % かを見る (520 の Phase 0)。
   **pro-con のアニメーションのコマ** (揺れ・カードの移動・カーソルの移動。494 と同じ測り方) も対象に入れる (2026-09-26 のユーザーの質問「アニメーションの箇所も asm で速くできない?」)
   - アニメーションのコマの約 6 割は幅の走査 (`fit` / `splice` / `ansi.Cut` / `cellsOf`。494) で、1 で `termwidth` を通るようになれば NEON 版がそのまま効く。asm を別に書かない
   - 残りの確保と GC (494 で揺れ 1 回の GC 15 → 4 回) と動きの計算 (イージング・色の補間) は、asm では減らない。494 の後の揺れの 1 コマは約 1.4 ms (1 コマの枠 33 ms の約 5%)
3. [520](520-perf-arm64-terminal-line-scanner.md): NEON 版は既に入っているので、2 の実測で残す価値を判断する (実画面で 10% 以上短くならなければ、520 の方針どおり外すか、
   残すなら理由を書く)。524 で pro-con・schedkeys が `termwidth` を通るようになると、NEON 版がそこにも効く

4. **ほかの候補 (2026-09-27 に形で洗った。未実測)**: asm (SIMD) が効くのは「大量のバイトを 1 つずつ見て、ほとんどが素通り」の形だけ
   - **`src/termsafe` (本命)**: 外から来た文字列 (git・CI のログ・issue の markdown・PG の出力・diff) を端末へ出す前に制御文字を落とす関門。
     全バイトを見て ESC・C0 を探し、ほとんどは素通り = `termwidth` の NEON 版と同じ形。glogx (CI のログ・diff)・pro-con (カードの詳細・diff・PG の出力) が長い文字列を丸ごと通す。
     benchmark はまだ無い。まず単体 (長い ASCII / ESC の混じる CI のログ / 日本語) と、画面の中の割合 (glogx で CI のログ・diff を開く / pro-con でカードの詳細を開く) を測る。
     画面で 10% 以上短くならなければ入れない (520 と同じ条件)
   - 効かない形 (asm にしない): pro-con の transcript (JSONL) の読み直し (JSON の読み解きと読み直しの回数が重い = 作りの話) /
     glogx の git・CI の取得と doctor の走査 (外のコマンドとファイルの待ち) / diff の色付け (049。正規表現と確保)

- 寄せただけで十分速くなれば、asm は入れない (520 の方針どおり)
- 新しい lib は立てない (`termwidth` に「切る・詰める」の API を足す)。別の lib が要ると分かったら、そのときに決める

## 関連

- 046 (glogx の dispWidth の速い道) / 494 (pro-con の演出のフレーム) / 513 (pro-con の性能の監査)
