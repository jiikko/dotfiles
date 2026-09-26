# 520 (perf): pro-con の端末行走査を darwin/arm64 ASM (NEON) fast-path へ置き換える

起票日: 2026-09-26

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

pro-con の描画 hot path に残る ANSI / Unicode の表示幅計算・切り出しを対象に、**Go 実装を正しさの oracle として残したまま darwin/arm64 向けの hand-written assembly (NEON) fast-path を導入する**。

狙いは「Go だから遅い」を前提にすることではない。issue 494 でカード複製・毎行の重複加工を減らした後も、描画では `ansi.StringWidth` / `ansi.Cut` / `ansi.Truncate` と、それらを組み合わせる `fit` / `splice` / `cellsOf` が同じ行を複数回走査する。まず current profile でこの層がまだ hot であることを確認し、**pure Go の 1-pass 化 → ARM64/NEON fast-path** の順で比較する。

ASM を入れること自体は受け入れ条件ではない。end-to-end で意味のある改善が出なければ Go 実装を残して close する。

## 既知の実測

issue 494 の 2026-09-26 計測では、200×50・実データ写しで:

- 揺れ 1 コマ: 直す前 1.18 MB / 4,413 allocs → 0.42 MB / 3,206 allocs
- 揺れ 1 コマ時間: 1.79 ms → 1.29 ms
- `overlayBump` の多重 `fit` / `splice` を 1 行 1 回へ減らした別計測では 2.1 ms → 1.4 ms
- 325×75 の実端末観測では演出直後 View が p50 2.3 ms / p90 5.9 ms / 最大 9.3 ms

つまり、構造的な重複処理を消す方が既に大きく効いている。ASM はその後に残った scanner 自体へ限定する。

また `src/tuikit/termwidth.Of` には issue 046 由来の fast-path があり、ASCII + SGR + 受理済み記号を自前走査し、それ以外は `ansi.StringWidth` へ fallback する。glogx ではこの方式で View benchmark が約 2〜3 倍改善した実績がある。この契約を壊さず、pro-con の直接 `ansi.*` 呼び出しも可能な範囲で同じ層へ寄せる。

## 対象

主対象:

- `src/tuikit/termwidth/termwidth.go`
  - `Of`
  - `fastDispWidth`
  - `ClipMeasure` / `CutMeasure` 系
- `src/pro-con/ui/view.go`
  - `fit`
  - `wrapTitle`
- `src/pro-con/ui/motion.go`
  - `splice`
- `src/pro-con/ui/cursor.go`
  - `cellsOf`
  - `softEdge`
  - `softSide`

非対象:

- Bubble Tea renderer 自体
- `x/ansi` の fork
- Unicode grapheme の独自完全実装
- cgo
- x86_64 向け手書き ASM
- 描画 semantics を変える最適化

## Phase 0: current profile で再判定

issue 494 後の current HEAD で、実際の pro-con 相当の画面を使って pprof / benchmark を取り直す。

最低限:

1. idle
2. cursor glide
3. lane bump
4. card move
5. 日本語・罫線・SGR・spinner を含む実データ

以下を記録する:

- View / Update の ns/op
- B/op / allocs/op
- CPU profile で `termwidth` / `ansi.StringWidth` / `ansi.Cut` / `ansi.Truncate` / `cellsOf` が占める割合
- 1 行あたりの byte 長
- printable ASCII run の長さ分布
- ESC / high-bit byte を含む割合

**着手 gate:** scanner / width / cut 系の合計が View CPU の 15% 未満なら、ASM 置換は行わず本 issue を「計測上不採用」で決着させる。

## Phase 1: Go reference を 1-pass に寄せる

ARM64化の前に、同じ行を何度も先頭から走査する構造を減らす。

候補:

- width と cut byte offset を同じ scan で返す
- ANSI SGR の位置を 1 回だけ解析する
- `cellsOf` が必要な行は strip → rune→string → StringWidth の往復を減らす
- `termwidth` を pro-con の表示幅の単一情報源にし、直接 `ansi.StringWidth` を呼ぶ箇所を減らす

この段階の実装を `*_go` reference とし、ASM 版の oracle にする。

🚨 pure Go の構造改善だけで end-to-end の改善が十分なら、ASMを入れない判断も許容する。

## Phase 2: darwin/arm64 NEON fast-path

### 方針

Go assembler で ARM64 実装を追加する。cgo は使わない。

例:

- `scanASCIIARM64`
- `fastDispWidthARM64`

build constraint は darwin/arm64 に限定し、それ以外は pure Go fallback。

NEON が担当するのは **長い printable ASCII run の分類・加算**を基本とする。

概念:

1. 16〜32 byte を vector load
2. 各 byte が `0x20...0x7e` に入るかを vector compare
3. 全 byte が printable ASCII なら width を chunk 分加算して進む
4. ESC / high-bit / control が 1 byte でもあれば、その位置まで進めて Go/scalar 側へ戻す
5. SGR と UTF-8 symbol table は既存 semantics を使う

最初から SGR parser や UTF-8 grapheme 全体を ASM に持ち込まない。vector 化の利益が明確な ASCII run だけを岩盤化し、特殊入力は既存 Go へ fallback する。

短い文字列では call/setup cost が勝つ可能性があるため、ARM64 path へ入る長さ threshold も benchmark で決める。

## 正しさの契約

ASM の正しさは「見た目が同じ気がする」ではなく、pure Go / x/ansi と完全一致で閉じる。

既存の以下を維持する:

- `FuzzDispWidthMatchesLibrary`
- `TestFastDispWidthMatchesLibrary`
- `TestAcceptedSymbolsNeverCombineWithEachOther`
- `TestDispWidthAgreesUnderEastAsianEnv`
- 不正 UTF-8
- SGR 以外の CSI
- 途中で切れた escape
- combining mark / VS16 / ZWJ / regional indicator / keycap / CJK

追加:

- Go reference と ARM64 実装の differential test
- 長さ 0〜数 KB の corpus
- ESC が chunk 境界に来るケース
- multibyte UTF-8 が chunk 境界を跨ぐケース
- ASCII run の先頭 / 中央 / 末尾に特殊 byte があるケース
- fuzz seed に実 pro-con frame を入れる

## benchmark

### micro

最低限:

- ASCII only: 16 / 32 / 64 / 128 / 512 / 2048 bytes
- ASCII + SGR
- pro-con の罫線・spinner・記号
- 日本語 title 混在
- fallback が即発生する文字列
- 実 frame から採取した行 corpus

比較:

1. 現行
2. pure Go 1-pass
3. ARM64/NEON

### end-to-end

- cursor glide
- bump
- card move
- 200×50
- 325×75
- card 50 / 500
- 実データ corpus

見る値:

- ns/op
- B/op
- allocs/op
- p50 / p90 / max View time
- 1 秒演出中の GC 回数

## 採用条件

ARM64/NEON版を master に残すのは、少なくとも以下を全部満たす場合だけ:

- differential / fuzz /既存 width tests が完全 green
- eligible な micro benchmark で pure Go 比 2 倍以上
- fallback 多発 corpus で有意な退行なし
- pro-con end-to-end View が pure Go 改善後からさらに **10%以上**短縮、または実端末 p90 / hitch が明確に改善
- B/op / allocs/op を悪化させない
- ASM の分岐・fallback 契約をコメントとテストで固定できる

micro だけ勝って end-to-end が変わらない場合は採用しない。

## 反証条件

次のどれかなら「ASMはこの層では不要」と結論してよい:

- current profile で width/cut scanner が 15% 未満
- pure Go 1-pass 化だけで hotspot が消える
- Go→ASM call overhead で短い行の実 workload が遅くなる
- x/ansi / runtime の既存 arch 最適化に勝てない
- Unicode/ANSI fallback 比率が高く vector path の到達率が低い
- end-to-end 10% 改善に届かない

## 進捗

- [ ] current HEAD を再 profile
- [ ] 実 frame の行 corpus / ASCII run 分布を採取
- [ ] pure Go 1-pass reference を作る
- [ ] reference の benchmark を固定
- [ ] darwin/arm64 Go assembler fast-path を作る
- [ ] differential / fuzz / EAW 子プロセス test
- [ ] micro benchmark
- [ ] pro-con end-to-end benchmark
- [ ] 実端末で framelog を比較
- [ ] 採用 / 却下を数値付きで本 issue に記録

## 関連

- 494: 演出フレームの allocation / GC と重複描画の削減
- 513: pro-con 性能監査
- 046: glogx / termwidth fast-path の実測と correctness 契約
