# 595 (research): src 配下で arm64 アセンブリにできる候補の調査 (2026-10-01) — 結論は「今は候補 0 件」

起票日: 2026-10-01

## 概要

ユーザーの依頼で、`src/` 配下の Go のプロジェクトのうち、**arm64 限定のアセンブリ** (Plan 9 asm の `_arm64.s` + build tag。arm64 以外は Go の実装へ戻す)
に置き換えると効く箇所を探した。**今の時点で asm にして効く候補は 0 件**。条件付きの候補が 1 件ある (下記)。

調べ方: 読み取り専用の調査役 (sonnet) がコードと既存の実測 (issue 523) を読み、main が 523 の該当箇所を読んで裏を取った。
**今回は新しいベンチ・profile を回していない** (数字は全部 523 の 2026-09-27 の実測からの引用)。

## 前例と、既に決まっている方針 (issue 523・520)

- `src/tuikit/termwidth` に arm64 の NEON 版がある (`fastwidth_arm64.go` / `.s`。arm64 以外と 16 byte 未満は Go 版 `fastDispWidthGeneric`)。
  守りは差分 fuzz `FuzzFastDispWidthMatchesGeneric`・境界入力の総当たり・変異。単体では ascii_1KB が 474 → 36 ns、画面全体では glogx の 1 フレームが geomean -7.4〜-8.5%
- 採用の基準は「**実画面 (壁時計) で 10% 以上短くなること**」(520)
- 523 の 2026-09-27 の測り直しの結論は「**asm はここで打ち止め**」。幅・切り詰めを丸ごと 0 にしても最大 11% (pro-con FrameBump)、asm で半分にしても約 5% で基準に届かない
- 1 コマの時間の大半は GC (`runtime.gcStart` 系が全サンプルの約 28%、1 コマあたり約 340 KB を確保)。**次に効くのは確保を減らすことで、asm では減らない** (494 の続き)

## 条件付きの候補: `src/termsafe` の `needsSanitize`

- 形: `!utf8.ValidString(s) || strings.ContainsFunc(s, mustStrip)` で、UTF-8 の検査と制御文字探しの 2 回の走査。大半の入力は素通りするので、
  NEON のバイト分類 (ESC・C0・DEL・C1 の先頭バイト 0xC2 0x80..0x9F・U+202A 等の先頭バイトを 16 byte ずつ比較して最大値で早期判定) が効く形ではある
- **ただし 523 で「asm にしない」と決着済み**: 単体は ASCII で約 670 MiB/s (8 KB の 1 行で 11.5 µs)。glogx ViewWithDiff・pro-con FrameBump・DiffBoardView の
  profile で **0 サンプル** (呼び出しは全部読み込み・取り込み側で、フレームでは走らない)。glogx の `BenchmarkIssueScan` でも 0%
- 再評価の trigger (523 と同じ): 数 MB の外部テキスト (CI ログ等) の取り込みで待ちが目に見えると報告されたとき。**まず pure Go で 2 回の走査を 1 回にし、asm はその後**。
  正しさは Go の `needsSanitize` を正解役にした差分 fuzz で守れる

## 見たが候補にしなかったもの

| 箇所 | 理由 |
|---|---|
| `tuikit/termwidth/fasttrunc.go` の `ansiTruncate` / `ansiTruncateLeft` | 分岐が多い (幅の表引きと SGR の読み飛ばしが混ざる)。幅・切り詰め全体で最大 11% なので asm で半分にしても約 5%。余地は pure Go 側 (`fastDispWidth` で 1 回走査した後、切る位置を探してもう 1 回走査している) |
| `termwidth.StripSGR` / `schedkeys/style.go` の `stripSGR` | 支配的なのは確保。ESC が無ければそのまま返す早期 return が既にある。フレームごとの全行では呼ばれない |
| `pro-con/ui/switchfade.go`・`tuikit/markdown` の `inline` / `wrap`・`ratelimit/usage/braille.go`・`tuikit/lineedit` | 分岐と確保が支配的な rune 単位の処理で、入力も短い。SIMD の形ではない |
| glogx の diff の色付け・git ログ・doctor の走査・pro-con の transcript (JSONL) | 正規表現・JSON の解析・外部コマンドとファイルの待ちが支配的。`strings.Count` / `IndexByte` 系は Go の runtime が既に arm64 の asm を持つ |
| chromecookie / restartable の `sha256`、`pro-con/ui/style.go` の `fnv` | sha256 は標準ライブラリが arm64 の asm と暗号拡張を使う。fnv は短い語の色割りだけ |

## 進捗

- [x] 候補の調査 (0 件。条件付き 1 件は 523 の trigger に従う)
- 次に asm を検討するのは、上の trigger が来たとき、または新しいホットパス (フレームごと・全行で走るバイト走査) が profile に出たとき
