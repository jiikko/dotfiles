# 701 (chore): tuikit のテスト・CI・依存・文書 (遅いテスト・版のずれ・幅の検査の抜け)

起票日: 2026-10-09

## 概要

702 の監査のテスト・CI・依存・文書の分と、品質の P3。

## 詳細

テストと CI:

1. **termwidth のテストが CI で 112〜137 秒** (P2) — `termwidth/fasttrunc_test.go` の TestAnsiTruncateMatchesAnsi が部品 21 個の深さ 4 の総当たり (約 19 万列)。
   run 37808337067 のログ `ok .../termwidth 131.859s` (他は 1 秒前後)。手元で -race 70 秒・非 race 4.7 秒。変異 6 本で深さ 2 は 6/6 red。
   このテストを走らせるのは src_tuikit.yml だけ (src/tuikit か src/termsafe を触る push ごと。同じ push で glogx・pro-con・treefiler・schedkeys・ratelimit の
   workflow も起動するが、それぞれ自分の module のテストを走らせる。反証レビューで訂正: 起票時は「5 本が走る」と誤解を招く書き方だった)。直し方: 深さ 3 (-race 3.7 秒) にし、
   深さ 4 は `//go:build !race` で残す。コメントの「最大 5 個」は実際 4 個
2. **幅の環境の検査を pro-con と treefiler が呼ばない** (P2・false-green) — `widthenv.ExitIfUnsupported` を TestMain で呼ぶのは tuikit の 6 パッケージ・glogx (本体と issues)・ratelimit (usage) で、pro-con と treefiler は呼ばない。
   `RUNEWIDTH_EASTASIAN=1 go test ./ui/` (pro-con) は「行の幅 131 が画面の幅 120 を超える」の大量の失敗、treefiler/filer も `TestScrollbarIsInsideBorder` などが落ちる
   (原因が環境だと分からない)。実行時の警告 (`widthenv.EastAsianAmbiguous`) を出すのも glogx の main だけ。直し方: 各 TestMain と main に足す・「tuikit を import する
   module は呼ぶ」を検査する
3. test-cleanup の候補 (消すかは判断): `widthenv_test.go` の TestEastAsianAmbiguousUnset (別のテストの 1 行と同じ・名前が実態と違う)・`width_fast_test.go` の
   TestSymbolWidthRejectsOutsideTable の後半 (構造上 red にならない)・TestAmbiguousIsNarrowByDefault (tuikit の関数を通らない。x/ansi の番犬としてだけ)・
   `markdown_test.go` の TestStaticGlyphsHaveNoVS16 (他のテストが先に落ちる)。fastWidthBoundaryInputs の通常版と Fuzz のシードが同じ入力で 2 回走る
4. test-helpers (母集合: *_test.go 32 ファイル・194 本): `termwidth.Of(` を使うテストが 8 ファイル 38 回 (そのうち「全行の幅が w に等しい」の掃引の形) →
   `assertRowsWidth(t, rows, w)` (反証レビューで訂正: 起票時の「13 ファイル 59 回」は再現できなかった)、
   TestMain の幅の検査の 3 行が 5 箇所、枠の内側を Trim する手組みが 3〜4 箇所、highlight_test.go の `stripForTest` (自前の走査) → `ansi.Strip`

依存:

5. runewidth と go-colorful の版が揃っていない — tuikit・treefiler・schedkeys (と、replace を使わず擬似版で固定する restartable) は runewidth v0.0.24・colorful v1.4.0、glogx・pro-con・ratelimit は v0.0.30・v1.4.1
   (issue 603 で揃えた記録があり、再びずれた)。`tests/scripts/test_tuikit_consumers_aligned.sh` は x/ansi・bubbletea・ultraviolet しか見ない。tuikit を 0.0.30・1.4.1 に
   上げても全テスト ok (実害は今は無い)。直し方: 検査の対象に足すか、版の差が無害という判断を 603 に追記する (treefiler の 696 の 9 と同じ件)
6. CI は Go 1.25.0 ちょうど (setup-go がパッチを上げない)。govulncheck (手元 go1.26.0) で到達する stdlib の脆弱性は GO-2026-6088 (encoding/xml。chroma の埋め込みの
   XML だけなので実害は低い)。go1.25.0 での結果は未確認。直し方: `toolchain` 行か `go-version: 1.25.x` (696 の 10 と同じ件)

文書:

7. `src/tuikit/CLAUDE.md` の「消費者」に treefiler が無い (`grep -l 'src/tuikit =>' src/*/go.mod` は 5 件)。`src_tuikit.yml` の冒頭のコメントは「glogx も走る」としか
   言わない (実際は 5 本)

品質の P3:

8. `Of(a+b) != Of(a)+Of(b)` (b の先頭が VS16・結合文字)。layout の Panel などが pad と行を連結するので、行頭の孤立した VS16 で行が 1 桁はみ出す
9. `layout.OverlayCentered` は箱が幅より広いと切らずにはみ出す (`OverlayRight` は切る)。呼び出し 8 箇所の箱の幅は未確認
10. 不正な UTF-8 で `Of` と `FirstCluster` / `DropColumns` / `Truncate` が食い違う (termsafe が落とす前提で到達しにくい。markdown・toast は通す)
11. `editor.Command` がパスの前に `--` を置かない (`-`・`+` 始まりの相対パスで vim の `+cmd`。今の呼び出しは絶対パスで発火しない)
12. toast.Stack の Visible / Text / OK / Info / Phase / Frame / Entries / Clear / Seq は本番の呼び出しが 0 (glogx のテストだけ)。`framebench.Frame` は attrs を `%+v`
    で比べ、ポインタを含むと毎回違う文字列になる (描画を省かない側。計測が重く出るだけ)
13. lint: tuikit に ruleguard が無く、glogx の「空白の連結は PadSpaces」が掛からない (今の違反は 0。alloc_test.go がその代わりと明記している)

## 関連

- 監査の記録: 702

## 進捗

- [ ] 未着手
