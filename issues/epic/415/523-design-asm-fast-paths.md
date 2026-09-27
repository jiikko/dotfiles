# 523 (design): asm 対応 — 端末文字列の走査を共通の lib に寄せてから、CPU 固有の速い道 (arm64 NEON) を入れる

起票日: 2026-09-26

親: [415](415-design-claude-pm-worker-orchestration.md) (2026-09-27 に epic 523 を epic 415 へまとめた。子は 520・524)

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

### 2026-09-27 の実測の結果 (2 と 4 の termsafe)

- **2 (実画面での割合)**: 520 の「Phase 0 の実測」節。glogx は NEON 版で geomean -8.5%・ViewWithDiff -12.4% (520 の条件 2 を 1 本で満たした)。
  pro-con は差なし: NEON 版の入口 (`fastDispWidth`) は pro-con の CPU の約 2% しか無く、幅・切り詰めの 19〜22% の大半は
  切り詰め・切り出し (`truncateOver` / `TruncateMeasure` / `SplitAround` / `CutMeasure` → `x/ansi` の書記素単位の走査)
- **3 (520 の判断)**: 条件 1〜3 を満たしたので NEON 版は残す。実端末の frame cadence (条件 4) は未確認
- **4 の termsafe: asm にしない (不採用)**。
  - 単体 (`src/termsafe/termsafe_bench_test.go`。この Mac、`-count 6` の中央値): 無害化の要らない文字列の fast path は ASCII で約 670 MiB/s
    (8KB の 1 行で 11.5µs、120B で 191ns)。重さはほぼ全部 `needsSanitize` (UTF-8 の検査 + `strings.ContainsFunc(s, mustStrip)`)。
    `DropEmojiVS16` は 1% 未満。改行・タブを含む塊 (`*_block_8KB`) は fast path を通らず 31〜37µs
  - 画面の中の割合: glogx ViewWithDiff・pro-con FrameBump・DiffBoardView の CPU profile で **0 サンプル** (termsafe の呼び出しは全部、読み込み・取り込み側
    = issue の解析・dispatcher の進捗・PG の出力の取り込みにあり、フレームでは走らない)。読み込み側の glogx `BenchmarkIssueScan` (1.10ms/op) でも
    **0%** (CPU の 47% はファイルを開くシステムコール)
  - 再評価の trigger: 長い外部テキスト (数 MB の CI ログ等) の取り込みで待ちが目に見えると報告されたとき。まず pure Go の ASCII の速い道
    (1 byte ずつの比較で `needsSanitize` の 2 回の走査を 1 回にする) を試し、asm はその後
- **5. 切り詰め・切り出しの速い道 (2026-09-27、pure Go。asm は入れていない)**: `termwidth` が x/ansi の `Truncate` / `TruncateLeft` を
  呼んでいた 5 か所を `ansiTruncate` / `ansiTruncateLeft` (`fasttrunc.go`) に替えた。行が `fastDispWidth` の受理集合 (印字可能 ASCII・SGR・
  幅を表に持つ記号) だけなら、書記素を 1 つずつ走査せずに x/ansi と同じ規則で切る位置を求め、受理しない行は x/ansi に任せる。
  - 一致: x/ansi 本体を正解役にした総当たり (`TestAnsiTruncateMatchesAnsi`: 部品 18 種を最大 4 個並べた全列 × 幅 -1〜9 × tail 5 通り × 左右) と
    差分 fuzz (`FuzzAnsiTruncateMatchesAnsi`、60 秒・1,134 万 exec) で不一致 0。変異 3 本 (切った後ろの SGR を落とす / 境界を 1 ずらす /
    左側の SGR を落とす) がそれぞれ red
  - 実測 (この Mac、変更前と変更後のテストバイナリを交互に 8 回、benchstat の中央値。🚨 計測中のロードアベレージが 7〜14 で、ばらつきが ±30〜70% と大きい):

    | benchmark | 変更前 | 変更後 | 差 |
    |---|---|---|---|
    | pro-con DiffBoardView/closed | 431.9µs | 294.5µs | -31.8% (p=0.028) |
    | pro-con DiffBoardView/open | 479.6µs | 360.6µs | -24.8% (p=0.038) |
    | pro-con FrameBump / FrameMove / FrameGlide | — | — | 3 本とも ~ (有意差なし) |
    | glogx 6 本 (ViewSteady ほか) | geomean 35.04µs | 25.61µs | **-26.9%** (6 本とも p≤0.01) |

    allocs/op は最大 -1.3%、B/op は最大 +1.7% (切った後ろに SGR がある行で SGR を並べ直す分)
  - pro-con のアニメーション (Bump / Move / Glide) に効かない理由: 画面の 50 行中 12 行が受理集合の外で、12 行とも原因は日本語 (カードの
    タイトルの漢字・ひらがな)。アニメーションで何度も切り貼りされるのがその行で、x/ansi へ回っている (変更後の FrameBump の profile で
    `ansi.StringWidth` 12.8% + `ansi.Truncate` 7.5%)
- **6. 受理集合に日本語を足す (2026-09-27。NEON 版も書き換えた)**: `acceptCJK` (漢字 拡張 A・統合・互換 / ひらがな / カタカナ /
  全角の英数と記号 / 和文の句読点。結合用の濁点・半角の濁点・声調記号・ハングルは除く)。表の上限 U+28FF → U+FFEF、
  `fastwidth_arm64.s` の 3 byte の先頭の判定 0xe2 → 0xef。幅・切り詰め・切り出し・NEON 版の 4 つが日本語の行にも効く
  - 一致: NEON 版と Go 版の境界入力 8,320 件 + 差分 fuzz 60 秒 (3,199 万 exec)、幅と x/ansi の fuzz 45 秒、切り詰めの総当たり + fuzz 45 秒で
    不一致 0。受理した 29,272 字の結合の検査 (`TestAcceptedSymbolsNeverCombineWithEachOther`) は本番の分割器 (x/ansi) で数える形にした
  - 実測 (変更前後のテストバイナリを交互に 10 回。ばらつき ±2%):

    | benchmark | 変更前 | 変更後 | 差 |
    |---|---|---|---|
    | pro-con FrameBump | 388.2µs | 214.6µs | **-44.7%** |
    | pro-con FrameMove | 170.2µs | 85.1µs | **-50.0%** |
    | pro-con FrameGlide | 283.1µs | 163.5µs | **-42.3%** |
    | pro-con DiffBoardView/closed・open | 245.5 / 325.8µs | 192.9 / 270.4µs | -21.4% / -17.0% |
    | glogx ViewSteadyJA / ViewWithPanelJA / ViewWithDiffJA | 27.6 / 34.2 / 34.1µs | 13.4 / 16.5 / 21.3µs | -51.6% / -51.9% / -37.4% |
    | glogx 6 本の geomean | 24.92µs | 16.44µs | -34.0% |

    allocs/op は最大 -4.4%。起動時の init は一時 約 0.07 ms → 約 1.1 ms に延びた (3 万字を ansi.StringWidth で測っていた)。
    同じ日に、日本語の幅を規則 (`cjkWidth`: 幅 2、U+303F だけ 1) で埋める形に替えて **約 0.10 ms・5 allocs** に戻した
    (規則と x/ansi の一致は `TestCJKWidthRuleMatchesLibrary` が全字で・RUNEWIDTH_EASTASIAN の子プロセスでも確かめる)
  - 敵対的レビュー (opus・読み取りのみ): P1 / P2 なし。受理字 × 文脈 (Hangul・Regional Indicator・Prepend・Indic・ZWJ・ExtPict) の結合、
    NEON 版の 3 byte 列 655,360 件 + BMP の全 rune、切り詰めのランダム 30 万本で、x/ansi・Go 版と全件一致。P3 の 2 件:
    - 結合の検査が uniseg (Unicode 15) で数えていた → 本番の分割器 (x/ansi、Unicode 16) で数える形に直した
    - init の約 1 ms は、毎プロンプト起動される `bin/ratelimit` (UserPromptSubmit の hook) にも乗っていた → 上の規則で埋める形にして解消
      (ユーザーの指摘「起動は遅くなって、描画で速くなる?」。描画をほとんどしないコマンドでは純粋な退行だった)
  - 範囲に未割当の字 (U+FA6E / U+FA6F / U+FADA..U+FAFF 等) が入っている。幅は x/ansi から焼くので今の版とは一致する。
    依存を上げたら上の検査を回し直す
- **次の候補 (未着手)**: pro-con / glogx のフレームで、幅・切り詰めがまだ CPU の何 % かを profile で測り直す (asm を切り詰め側にも入れる価値があるか)

- 寄せただけで十分速くなれば、asm は入れない (520 の方針どおり)
- 新しい lib は立てない (`termwidth` に「切る・詰める」の API を足す)。別の lib が要ると分かったら、そのときに決める

## 関連

- 046 (glogx の dispWidth の速い道) / 494 (pro-con の演出のフレーム) / 513 (pro-con の性能の監査)
