# 593 (test): tuikit の termwidth のテストが `-race` で手元 4 分・CI 6.5 分かかる (3 本の総当たりがほぼ全部)

> 🚨 **担当中: dotfiles-05**（2026-10-01〜）

起票日: 2026-10-01

## 概要

`make -C src/tuikit test` (`go test -race ./...`) で、`termwidth` パッケージだけが **248 秒**かかる (他のパッケージは各 1〜2 秒)。
ほぼ全部が 3 本の総当たりのテストで、そのうち 1 本は子プロセスで、`RUNEWIDTH_EASTASIAN=1` の下でもう 1 本の総当たりを丸ごと走らせる
(env で答えが変わる検査なので、無駄な重複ではない。対応方針 1)。

## 詳細

### 実測 (2026-10-01、`go test -race -count=1 -v ./termwidth/`、この開発機)

| テスト | 時間 |
|---|---|
| `TestDispWidthAgreesUnderEastAsianEnv` | 85.5 秒 |
| `TestAcceptedSymbolsNeverCombineWithEachOther` | 84.8 秒 |
| `TestAnsiTruncateMatchesAnsi` | 69.3 秒 |
| それ以外 (最大 `TestFirstClusterWidthsSumToOf` 3.1 秒) | 合計 6 秒程度 |

調査役の測定では、`-race` なしだと `TestAnsiTruncateMatchesAnsi` 4.5 秒・`TestAcceptedSymbols…` 4.6 秒 (race で 15〜18 倍)。

### 何をどれだけ回しているか (コードを読んだ結果。数は調査役の数え。main では数え直していない)

- `TestAcceptedSymbolsNeverCombineWithEachOther` (`width_fast_test.go`): 受理字 29,272 × 相手 1,130 = 約 3,300 万ペア。
  各ペアで書記素の走査と `ansi.StringWidth`
- `TestAnsiTruncateMatchesAnsi`: 部品 21 個の深さ 4 の全列 (約 20 万) × 幅 13 × tail 5 × 関数 2 × (自前 + x/ansi)
- `TestDispWidthAgreesUnderEastAsianEnv`: `RUNEWIDTH_EASTASIAN=1` の子プロセス (`os.Args[0]`、つまり race のテストバイナリ) で
  `-test.run=…|TestFastDispWidthMatchesLibrary|TestAcceptedSymbolsNeverCombineWithEachOther)$` を走らせる (`wantPass = 4`)。
  親が走らせたのと同じ `TestAcceptedSymbols…` の総当たりを、env だけ変えてもう 1 回払っている (main で確認。重複ではない理由は対応方針 1)

### どこで払うか (2026-10-01 の見直しで訂正)

「commit 前の `make test` で毎回 4 分」ではない。go のテストキャッシュが効くので、手元で 2 回続けて回すと 2 回目の termwidth は `(cached)` になる。
手元で 4 分払うのは termwidth・tuikit の go.mod / go.sum を変えたときだけ (tuikit を作った 9/24 から 19 commit、9/28 以降は 3)。
**毎回払うのは CI**: `src/tuikit/**` と `src/termsafe/**` を触る push のたびに 388 秒 (これまで 59 commit が該当)。優先度の根拠はこちら。

## 対応方針 (どれも検出力を変えうるので、採る前に変異で red を確かめる)

1. 子プロセスの filter から `TestAcceptedSymbolsNeverCombineWithEachOther` を外す案は**採らない方向**。
   そのテストは各ペアで自前の幅と `ansi.StringWidth` を突き合わせており、`ansi.StringWidth` は env で値を変える
   (同じファイルの子プロセス側の前提確認が `ansi.StringWidth("─") == 2` を要求している)。つまり子での再実行は
   「env の下でも自前の幅が x/ansi と一致する」を見ていて、親の実行とは別の検査になっている (反証レビューの指摘)
2. 総当たりの母集合を**さらに**間引く。後ろに置く相手の日本語は既に「範囲の端 + 97 字おき」に間引き済み (テスト冒頭のコメント。
   全部を相手にすると 140 秒)。残るのは前に置く字 (全受理字) で、ここを同じ形に間引けるかを見る (コメントは「前に置く字に Extend が
   紛れ込んだら ASCII の相手と組んで落ちる」ことを根拠に全部を回している)。あわせて `TestAnsiTruncateMatchesAnsi` の深さ 4 を幅の小さい範囲に限れるかを見る。
   間引いた版で、受理範囲に Extend の字を混ぜる変異・自前の切り詰めを 1 桁ずらす変異が red になることを確かめてから採る
3. 3 本を `t.Parallel()` にして wall time を合計から最大へ縮める (CPU の総量は変わらない)

CI (`.github/workflows/src_tuikit.yml`) でも同じ形 (2026-10-01 の run 36811455082、`gh run view --log`): termwidth が **388 秒**、
ほかのパッケージは各 1.0〜1.3 秒。workflow の所要は 03:38:39〜03:48:36 の約 10 分で、その大半が termwidth の 1 パッケージ。

## 関連ファイル

- `src/tuikit/termwidth/width_fast_test.go` (`TestAcceptedSymbolsNeverCombineWithEachOther` / `TestDispWidthAgreesUnderEastAsianEnv`)
- `src/tuikit/termwidth/fasttrunc_test.go` (`TestAnsiTruncateMatchesAnsi`)
- `src/tuikit/Makefile` (`test: go test -race ./...`)

## 進捗

- [x] CI での所要時間を確認 (388 秒。上の詳細)
- [x] 1: 子の filter から外す案は採らない (env で答えが変わる。上の対応方針 1)
- [ ] 2 / 3: 間引き・並列化の判断 (変異で検出力を確かめてから)
