# 520 (perf): glogx / pro-con の端末文字列走査を ARM64 assembly fast path へ落とす

起票日: 2026-09-26

親: [523](523-design-asm-fast-paths.md) (2026-09-26 に 415 から移した)

> **2026-09-26 に NEON 版が master へ入った** (PR #14、`a413f1ef`。実測は 523 の本文)。Phase 2 の実装はこれで済んでいる。残りは、実画面での効果を測って残すかを決めること。
> Phase 1 (共通の境界へ寄せる) は [524](524-refactor-route-terminal-width-through-termwidth.md) に切り出した

## 背景

DHH の「高水準実装を agent に低レイヤへ掘らせ、CPU 固有の実装まで落とす」実験を、
この repo の実測済み hot path で試す。

対象は TUI の端末文字列処理。`glogx` では過去の性能監査 (#046) で
`dispWidth` が View CPU の 64% を占め、`src/tuikit/termwidth` に
ASCII / SGR / 既知記号の scalar fast path を入れることで実運用相当でも約 2 倍まで改善した。
`pro-con` でも #494 の計測で、演出フレームの残りコストの大半が
`ansi.StringWidth` / `ansi.Cut` / `fit` / `splice` / `cellsOf` など
ANSI + Unicode の走査へ寄っている。

この issue では「TUI 全体を assembly で書き直す」のではなく、
**大量に反復する byte scanner の common path だけを darwin/arm64 用 assembly に置換し、
特殊 Unicode / 不正 UTF-8 / 非 SGR CSI は既存 Go 実装へ fallback する**。

## 対象

- `src/tuikit/termwidth/termwidth.go` — `Of` / `fastDispWidth`
- `src/glogx/width.go` — 既に `termwidth.Of` を利用
- `src/pro-con/ui/view.go` — `fit` が `ansi.Truncate` + `ansi.StringWidth` を直接利用
- `src/pro-con/ui/motion.go` — `splice` が同じ行を `StringWidth` / `Cut` で複数回走査
- `src/pro-con/ui/cursor.go` — `cellsOf` が strip → rune → string → StringWidth を文字ごとに行う

## ARM64 fast path の責務

arm64 側が扱うのは、まず次だけに限定する。

- printable ASCII (`0x20...0x7e`) の連続 run
- SGR の開始候補 `ESC [` の検出
- high-bit byte / C0 / DEL / SGR 以外の escape の検出
- 「ここまでは scalar parser なしで処理できる」という boundary の返却

**Unicode grapheme の意味論を assembly に複製しない。**
非 ASCII 記号の幅・結合文字・ZWJ・VS16・国旗・keycap 等は既存の
`termwidth` / `x/ansi` を正本にする。

## Phase 0: 現在のボトルネックを再測定

#046 / #494 の修正後なので、古い profile を根拠に assembly を入れない。

- glogx: `BenchmarkViewSteadyJA` / `BenchmarkViewWithDiff`
- pro-con: #494 と同じ実データ相当の animation frame
- cursor glide / bump / card move を分けて測る
- CPU profile で terminal width / cut / cell scan が依然として有意か確認する

再開条件:
- 対象 scanner 群が対象 benchmark CPU の **20% 以上**を占める、または
- 単体 scanner benchmark で SIMD 化の余地が十分にある

満たさなければ「assembly の実験価値は確認したが製品価値なし」として閉じる。

## Phase 1: pro-con を共通 scanner 境界へ寄せる

ARM64 を直接 `pro-con/ui` に埋め込まない。

- width の正本を `tuikit/termwidth` に揃える
- `fit` / `splice` / `cellsOf` が同じ行を何度も先頭から解析している箇所は、
  可能なら「1 pass で width + cut boundary + cell metadata を得る」APIへ寄せる
- この段階では Go の scalar 実装だけで正しさを固定する

目的は assembly より先に**重複走査そのものを消すこと**。
アルゴリズム変更で十分速くなったら assembly は入れない。

## Phase 2: darwin/arm64 assembly fast path

配置案:

```
src/tuikit/termwidth/
  scan.go
  scan_arm64.go
  scan_arm64.s
  scan_generic.go
```

- `scan_arm64.s`: Go assembler (Plan 9 syntax) で ARM64 / NEON fast path
- `scan_generic.go`: scalar reference / 他 architecture fallback
- assembly は 16/32 byte 単位で load し、
  - printable ASCII の全 lane 判定
  - ESC / high-bit / control byte の有無
  をまとめて処理する
- 特殊 byte を見つけた位置で Go scalar parser に戻す

ABI 境界を細かく跨ぐと呼び出しコストで負けるので、
「1文字ごとに assembly を呼ぶ」のは禁止。1 行 / 1 buffer 単位で渡す。

## 正しさの契約

既存 Go 実装を oracle として残す。

- `Of(asm) == Of(reference)`
- cut / cell boundary を返す場合も scalar reference と完全一致
- 既存 fuzz corpus をそのまま arm64 実装へ当てる
- ASCII / SGR / CJK / combining mark / VS16 / ZWJ emoji / 国旗 / keycap /
  tab / DEL / C0 / 非 SGR CSI / 途中で切れた escape / invalid UTF-8 を含める
- `RUNEWIDTH_EASTASIAN=1` 子プロセス検証も維持する

assembly の結果を期待値にしたテストは禁止。
**scalar reference と production library が正本**。

## 性能の受け入れ条件

局所 benchmark だけ速い、では採用しない。

1. scanner 単体で scalar 比 **1.5x 以上**
2. glogx / pro-con の少なくとも一方の実画面 benchmark で **10% 以上**短縮
3. allocs/op を悪化させない
4. 実端末で frame cadence を悪化させない
5. Release build で比較する

1 を満たしても 2 を満たさなければ、Amdahl の法則で製品価値がないため
assembly は mainline に残さない。

## 非対象

- Bubble Tea renderer 自体
- terminal I/O / tmux
- Unicode grapheme algorithm 全体の再実装
- x86-64
- `chroma/regexp2` の syntax highlight (#049): allocation / tokenizer 構造が主因なので別問題
- pro-con の cards / transcript / relay など I/O 系 perf issue

## 完了条件

- [ ] Phase 0 の profile と benchmark を issue に記録
- [ ] pro-con の重複走査を共通 scanner 境界へ寄せる
- [ ] scalar reference を独立に保持
- [ ] darwin/arm64 fast path を追加
- [ ] fuzz / corpus で reference と不一致 0
- [ ] scanner 単体 1.5x 以上
- [ ] 実画面 benchmark 10% 以上
- [ ] 条件を満たさない場合は assembly を revert / 不採用として理由を記録
- [ ] `go test -race ./...` / lint green

## 関連

- #046 glogx dispWidth fast path
- #047 glogx frame allocation
- #494 pro-con animation frame allocation / GC
- #513 pro-con performance audit
