# 1025 (research): src/dupscan の 22 観点の監査 (直接実行、2 回目) — 全数勘定と、直さないと決めたもの

起票日: 2026-11-04

## 概要

`/audit` で src/dupscan に 22 観点を直接実行 (main が読んで判定) で回した。前回の [984](984-research-dupscan-audit-2026-10-30.md) (2026-10-30) から 5 日。
起点の HEAD は `49691c625` (1021 の sweep の畳みを入れた直後)。前回が精読しなかった範囲 (984 の記述: sweep_cmd.go・near / search / index / context / evidence の各コマンド・`internal/lang/golang/conv.go`) と、`internal/typed`・callers / def を中心に読み、前回の結論と却下の理由は出し直さない。

発見 3 件はこの監査の中で直した (下の「直したもの」)。新しい issue は起こしていない。

## 走査した母集合

- production の Go: 54 ファイル・13,999 行 (`find src/dupscan -name '*.go' -not -name '*_test.go' -not -path '*/third_party/*'` の件数と、`-print0 | xargs -0 cat | wc -l`)。テストを含めると 135 ファイル
- 今回全部を読んだもの: near_cmd.go・similar_cmd.go・search_cmd.go・internal/report/search.go・context_cmd.go・evidence_cmd.go・index_cmd.go・callers_cmd.go・query_ux.go・def_cmd.go・
  internal/lang/golang/conv.go・sweep_cmd.go (全部)・sweep_inline.go・sweep_types.go・internal/typed/typed.go。capabilities_cmd.go は前半だけ
- **今回は精読していない** (984 は Parse と読み直し以外の swift.go を未精読と書いた。ほかの 3 つは今回の申告): internal/lang/swift/swift.go (2177 行。前回読んだ Parse と読み直し以外)・internal/report の outputs.go / symbols.go / index.go・internal/exclude/set.go (次の監査の起点)
- 機械の検査: `go vet ./...` rc=0 / `staticcheck ./...` rc=0 / `deadcode` (production から届かないのは `internal/lang/langtest` の 2 関数だけ。テストだけが import する共有の helper と package の doc に書いてあるので数えない) と `deadcode -test` (0 件) /
  `dupscan sweep src/dupscan` (単位 725) / `go list -m -u all`。**`errcheck` は回せていない** (手元の errcheck が go1.25 でビルドされていて go1.26 の module を読めない。`make lint` の golangci-lint の errcheck は rc=0)。**`govulncheck` は手元に無く回していない**

## 直したもの

| 観点 | 内容 | 発火条件 | 対応 |
|---|---|---|---|
| broken-code | `callers -typed` の鍵が、alias を受け手に書いたメソッドで package のパスを 2 度書く (`fix/a.fix/a.T.M`)。同じ型の他のメソッドは `fix/a.T.N` | `type AT = T; func (AT) M()` (値でもポインタでも)。`types.Unalias(nt).String()` が package のパスつきの型名を返していた | `internal/typed` の `Key` で受け手の alias を剥がしてから Named として読む。テスト `TestTypedCallersKeysAliasReceiverAsOriginType`、変異 2 本 (値の受け手・ポインタの受け手で剥がさない) が red |
| duplication | swift の `markOuter` の冒頭 4 行が、同じ package の `set` と同じ処理 (sweep の節 5 が出した) | — | `set(params...)` を呼ぶ |
| ux (文書) | README の sweep の実測「`src` 全体 (単位 8705) で 0.2 秒 (3 は上限で飛ばした)」が古い。今は節 3 で比べる単位が 6454 で上限 8000 の内側にあり、節 3 も走る | `dupscan sweep src` | 実測 (各 2 回): `src/glogx` 0.34 秒、`src` 全体 5.0 秒、節 3 を飛ばすと 0.48 秒。README を書き換えた |

## 観点ごとの結果

| 観点 | 結果 | 攻めた所・報告しなかった理由 |
|---|---|---|
| design | 0 | 各コマンドの入口が `repo.Resolve` → `loadConfig` → `scanSource` の同じ並びで、出力の形だけを分けている。食い違いは見つからなかった |
| responsibility | 0 | 前回の見送り (swift.go) のまま。trigger も同じ |
| duplication | 1 (直した) | sweep の節 3 の `golang.go` の `add` ~ `swift.go` の `add` (Dice 0.625) は、Swift 側だけが読めない印と `isUnit` を持つ (責務が違う) ので寄せない。節 1 の `writeFile` ~ `write`・節 2 の 8 本の test wrapper は前回 984 の見送りのまま |
| polymorphism | 0 | 前回と同じ (言語は adapter の表、コマンドは `commandSpecs` の表) |
| leaky-abstraction | 0 | `context` が Go だけの範囲で rc=2 (判定不能) を返すのは、Go の def / refs を読まない既知の非対応で、[944](../pending/944-feat-dupscan-go-def.md) で保留中 (rc の値は 944 に書かれていない)。Go の adapter は `Symbols` を立てない (ゼロ値) ことで宣言し、`langs.Symbols` が見て案内を出すので L4 の嘘ではない |
| encapsulation | 0 | sweep の `nested` は「同じファイルで行の範囲が含まれる」で入れ子を決めるので、1 行に 2 つの単位が並ぶと入れ子とみなす。gofmt した Go では関数は 1 行に 1 つなので、実害の出る入力を示せない (記録だけ) |
| ui-components | 0 | CLI |
| ux | 1 (直した。文書) | 今日 treefiler で使って困ったのは節 2 の表の行 (1021 で直した) と節 3 の mutex の getter の対 (965 と今日の 2 回目。1021 の見送りに「3 回目が出たら定型の部分木を割り引く案を測る」と書いた) |
| security | 0 | search / evidence の `read_argv` は argv の JSON (shell を通さない)。`next_action` の argv も同じ |
| resource-leaks | 0 | typed の `packages.Load` は module ごとに 1 回で、結果を持ち越さない |
| broken-code | 1 (直した) | 上の表。ほかに `pick` (near / explain) の名前・行の選び方、`cutSelector` の最後の `:` で切る形を見た |
| dead-code | 0 | deadcode の 2 件は `langtest` (上の母集合) |
| error-handling | 0 | context / evidence の `Fprintf` の誤りを捨てる件は 984 の却下のまま (再提起の条件も同じ) |
| false-green | 0 | sweep は合否を返さない (rc は入力の誤りだけ)。`near -similar` は比べた数・飛ばした数・読めないファイルの数を出す |
| performance | 0 (文書を直した) | search 0.31 秒・evidence 0.22 秒・sweep 5.0 秒 (src 全体、`/usr/bin/time -l`)。sweep の節 3 の総当たりは opt-in の掘る道具の所要として許容。search は語 × 欄 × 単位ごとに欄を分かち書きし直すが、0.31 秒なので直さない |
| concurrency | 0 | 前回と同じ (`parseAll` の worker は別々の添字に書く) |
| test-cleanup | 0 (浅い) | 新しく足したテスト (1021 と今回) には変異を当てた。既存のテストの変異は当てていない |
| test-helpers | 0 | 前回と同じ |
| dependency | 0 (観測を閉じた) | 984 が「上げると 958 の読み直しが要らなくなるかは未確認」とした Swift の文法の新しい版 (`v0.0.0-20261013201735-9e3b87f66f1f`) は、upstream の main の commit で生成済みの `src/parser.c` を含まない。上げると cgo のビルドが `'../../src/parser.c' file not found` で落ちた (2026-11-04)。今使っている `82bb3a533e08` はタグ `0.7.4-with-generated-files` で、これより新しいタグは無い (`gh api repos/alex-pinkus/tree-sitter-swift/tags`、2026-11-04) ので上げる候補が無い。**再評価の trigger**: 生成物つきのリリース (「Updating grammar files for version …」の commit) が 0.7.4 より新しく出たとき。`go list -m -u` が出す版は main の commit なので、そのまま上げない |
| ci | 0 | src_dupscan の直近 8 run (2026-10-30〜11-04) は全部 success。run 41833889418 の所要は test 126 秒 (うち Test 105 秒。cgo と -race のビルドを含む)・lint 32 秒。dupscan-gate の直近 5 run は success 4・cancelled 1 (concurrency で後の push に取り消された) |
| lint-from-done | 0 | alias の鍵の誤りは `types.X.String()` を鍵に使う形だが、1 か所で lint にする価値は無い |
| general | 0 | 上の各行 |

全数: 発見 3 (broken-code 1・duplication 1・文書 1。どれもこの監査で直した)、既知 3 (Go の context の非対応 (944 で保留)・nested の同じ行・Fprintf)、観測を閉じた 1 (文法の版)。

## 反証レビュー (2026-11-04、sonnet 1 本、読み取りのみ)

P2 2 件・P3 3 件。採った: production の数 (61 ファイル・13,912 行 → 数え直して 54・13,999) / Go の context の rc=2 を「944 で決めた仕様」と書いた言い過ぎ → 「944 で保留中の既知の非対応」に /
「今回も精読していない」の「も」に根拠が無い → 984 の記述と今回の申告を分けた / 前回の未精読の範囲を 984 の文言に合わせた。
README の新しい単位の数 (9180) は `dupscan sweep src` の 1 行目が出典 (裏は取れた)。CI の run と gh のタグの一覧は、レビューは gh を打たない縛りで未確認 (本文の値は main が打って得た)。

## 進捗

- [x] alias の受け手の鍵・`markOuter` (commit「WIP: 監査 — typed の alias の受け手の鍵 / markOuter を set に寄せる」。骨組みの push で WIP のまま master に出た。lint・gate・root の make test rc=0 を後から確認)
- [x] README の sweep の実測 (commit「WIP: README の sweep の実測を今の値に」。同上)
- 残課題なし (この issue は記録なので done へ送る)
