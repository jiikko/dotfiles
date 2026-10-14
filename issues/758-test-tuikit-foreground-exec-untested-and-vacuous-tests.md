# 758 (test): tuikit のテスト: foreground.Exec の SetStdin が未検査・何も検査しないテスト 2 本・pty テストの Skip が見えない

> 🚨 **担当中: Claude (tuikit の監査の対応セッション)**（2026-10-14〜）

起票日: 2026-10-14

## 概要

2026-10-14 の tuikit の監査 (記録は 760) の test-cleanup / test-helpers と、false-green の候補。変異は監査体が scratchpad の複製で当てた
(build の成功と対象テストの実行を確認。baseline の `go test ./...` は全 ok)。母集合は `*_test.go` 42 ファイル (うち `func Test` を持つもの 37)・Test 関数 203 本 (TestMain 7 本を含む。反証レビューの数え直し)。
main は「foreground の Test 関数は Reclaim と Detached の 2 本だけ」を grep で確かめた。変異の結果は監査体の報告。

## 詳細

1. **`foreground.Exec` の挙動を守るテストが tuikit の中に無い。`SetStdin` の条件はどこでも検査されていない** (P3。反証レビューで P2 から下げた)
   - `src/tuikit/foreground/exec.go` で `if held {` を `if false && held {`・`x.Was, x.Err = reclaim()` を `0, nil`・`SetStdin` の `x.Cmd.Stdin == nil` の条件を外す、の
     3 つの変異で tuikit 全体が緑。`foreground_test.go` にあるのは `Reclaim` と `Detached` の pty テストだけ
   - 使う側が守っている: pro-con の `ui/terminal_test.go` (`TestTermExecReclaimsOnlyWhenItHeldForeground` / `TestTermExecHandsOverWhileChildRuns` /
     `TestTermExecWithoutCommandRunsNothing`) が変異 1・2 を赤にする (反証レビューが実測)。treefiler の `TestBangGivesTerminalBackOnPTY/job/sh/shell-took-back` も
     `shellTookBack` の枝を消す変異で赤。pro-con も treefiler も tuikit を replace で取り込み、各 workflow の paths に `src/tuikit/**` があるので、tuikit の変更で CI が走る。
     `src/tuikit/CLAUDE.md` の「Owned・Reclaim はテストが差し替える」が指すのは pro-con の stub (issue 707 の本文)
   - 本当に残っている穴: 変異 3 (`SetStdin` の `Stdin == nil` の条件) はどこでも緑。テストが tuikit ではなく pro-con に置かれているので、tuikit だけを見ると守られていないように見える
   - 直し方: `SetStdin` の条件を見るテストを 1 本足す (置き場は tuikit の `foreground/exec_test.go`)。pro-con の 4 ケースを tuikit へ移すかは任意
2. **`termwidth/width_fast_test.go` の `TestSymbolWidthRejectsOutsideTable` の後半は構造上赤にならない** (701 の 3 を確定)
   - 後半のループが回す `acceptedSymbols` は `symTableLo..symTableHi` を走査して集めたものなので、「表の範囲を超えない」は走査範囲そのものが保証する (読解。変異は設計できない)
   - 直し方: 後半を消す (前半は有効なので残す)。失うカバレッジは無い
3. **`widthenv_test.go` の `TestEastAsianAmbiguousUnset` は表の `{"", false}` と同じ入力・同じ期待値の重複** (701 の 3 を確定。読解)
   - 直し方: 消す (名前が「未設定」なのに空文字を見ている食い違いも消える)
4. **pty テストが CI で Skip されても見えない** (false-green F4 の候補。未確認)
   - `foreground/ptytest` の `Run` は、`script` / zsh が無いときや擬似端末の前面に居られないとき `t.Skipf` で抜ける。CI は `-v` 無しなので、ログには `ok .../foreground 4.399s` しか残らない (run 38246811383)
   - 所要からの推定では実行されている (手元で 0 件のとき 1.167s・実行して 4.419s。CI 4.159〜4.525s。dependency/ci の監査体)。ただし PASS 行での確認は無い
   - 直し方: CI のときは Skip を失敗にする (環境変数で)、または Skip の理由を必ず 1 行出す
   - ついで (未確認): `ptytest.Run` は `script` の実行に時間の上限が無い。子が止まる退行では suite ごと待ち続けうる。`bin/runtimeout` か `CommandContext` で上限を付ける
5. 任意 (P3):
   - `TestAmbiguousIsNarrowByDefault` は tuikit の関数を通らない x/ansi の既定の番犬。残すなら名前で分かるようにする
   - `toast/toast_test.go` の枠を剥がす `strings.Trim(ansi.Strip(row), "│▖▁▗▓█▒░ ─┌┐")` が同じ形で 2 か所。toast_test.go の中に 1 つ置く程度
   - `sgr/xterm_test.go` の `TestXtermRGB` は `basic16` の 6/16 しか見ない。足すなら xterm の仕様表と突き合わせるときに限る

## 却下 (再提出しない)

- `TestStaticGlyphsHaveNoVS16`: 水平線と表の罫線への VS16 の変異はこのテストだけが赤。701 の「他のテストが先に落ちる」は半分だけ正しい
- `TestToastHeightMatchesFullBox` (独立した 2 経路の整合)・`TestInfoToastHasNoMark` (info の枠を見る)・TestMain の `ExitIfUnsupported` の定型 (701 の 4 で見送り済み)

## 関連ファイル

- `src/tuikit/foreground/`・`src/tuikit/termwidth/width_fast_test.go`・`src/tuikit/widthenv/widthenv_test.go`・`src/tuikit/toast/toast_test.go`・`src/tuikit/sgr/xterm_test.go`
- 前回: 701。監査の記録: 760

## 進捗

- [ ] 1〜5 は未着手
