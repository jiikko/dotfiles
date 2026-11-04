# 1019 (research): src/treefiler の全観点の監査 (2026-11-04。直接実行) — 全数勘定と、直さないと決めたもの

起票日: 2026-11-04

## 概要

`/audit` で src/treefiler に全観点を直接実行 (main が読んで判定) で回した。前回 ([992](992-research-treefiler-audit-2026-10-30.md)、2026-10-30) の
監査範囲の終わり `c6c6b6329` から `f47ecf97e` までに src/treefiler に入った差分 (テストを除く 31 ファイル (うち Go 26)・+2046 / -211 行) と、992 が
「精読していない」と書いた layout / virt / pulse / tileview の変更を重点にした。直したのは 3 点 (下の「対応」)、起票は 1 本 ([1020](1020-test-treefiler-virt-seed269-flaky-gomaxprocs1.md))。

## 走査した母集合

- 差分: `git diff c6c6b6329..f47ecf97e --stat -- src/treefiler ':!*_test.go'` で 31 ファイル (Go 26) (`load.go`・`pane.go`・`perflog/`・`debuglog.go`・`tools/tfperf/` は新規で、どの監査も通っていなかった)
- 全部を読んだ: `filer/load.go`・`filer/pane.go` (読み込みと描画)・`filer/search.go`・`filer/history.go`・`filer/pulse.go`・`filer/anim.go`・`perflog/perflog.go`・`debuglog.go`。
  差分を読んだ: `filer/goto.go` (goHome)・`filer/places.go`・`filer/layout.go`・`filer/render.go`・`filer/walk.go`・`filer/direntries_darwin.go`・`filer/tileview.go`・`filer/virt.go` (`87eeae19b..` の全差分)・`main.go` (perflog / debug の配線)・`filer/tree.go` (`readKids`・`setRaw`)。
  **読んでいない差分** (Go 26 本のうち): `filer/model.go` の大半 (flushLoad・Busy / Animating の配線)・`tile.go`・`preview.go`・`ask.go`・`debug.go`・`direntries.go`・`gorules/rules.go`・`tools/tfprobe/main.go` (反証レビューが model.go と tile.go の差分を読み、発火条件を示せる不具合は無かった)
  部分: `tools/tfperf/main.go` (tmux の隔離と後片付けだけ)・`filer/settings.go` (`configDir`)
- 機械の検査 (本体の checkout、`f47ecf97e`): `go vet ./...` rc=0 / `go test -race -count=1 ./...` 全 package ok / `make lint` 0 issues /
  `deadcode ./...`・`deadcode -test ./...` ともに 0 件 (go1.25.4 の deadcode) / `bin/dupscan sweep src/treefiler` (単位 808・94 ファイル)

## 観点ごとの結果

| 観点 | 結果 | 攻めた所・報告しなかった理由 |
|---|---|---|
| duplication | 1 (直した) | sweep の節 3 の 1 位 `spawnFrom` ~ `ghostTarget` (Dice 0.77) は、同じ「いちばん近い消えかけでない祖先の右端」の探索を 2 つ持っていた。節 2 の settings の表・`init` の委譲・tfperf の集計のクロージャは表の行どうしの形 (992 と同じ判断)。節 3 の explode / diff / watch の lock 付きの小さい関数は 3 行ずつで寄せる価値が無い |
| test-helpers | 1 (直した) | sweep の節 2 の `[L1] test`: `load_test.go` の `statusLine` と `smallscreen_test.go` の `lastRow` が同じ本体 |
| broken-code | 1 (コメント。直した) | `history.go` の `appendHistory` のコメントに「読む側の loadHistory も symlink を辿らない」とあったが、`readHistory` は `os.Open` で link を辿る (辿らないのは `loadHistory` の書き直し = `atomicfile.Write` の rename)。ほかに `'` (goHome) は root が上へしか動かない (`rerootUp`・`:` の `rootUndo`) ので戻り先はいつも木の中・`RestorePlace` で glogx の位置へ更新、`'` は glogx の F でも `Binds` で filer に届く (`filerview.HandleKey`) |
| concurrency | 0 | `load.go` の goroutine が読む `node` の欄は `abs` だけで、`abs` は作る関数の中 (`newNodeAttr`・タイルの `nodeFor`) でしか書かれず、goroutine に渡る前に決まる (rename は親の読み直しで新しい node。`setRaw` は raw と lower だけ)。`requestLoad` は `readBulk` を goroutine の外で読む。`pane.go` の goroutine は値の鍵だけを読み、結果は世代つきで lock の中に置く。`layoutWith` の `colBuf` の使い回しは再入が無い (呼び出しは `layout` と、テストの `layoutAll` の `layoutWith(false)`。中から自分を呼ばない)。`canvas.lines` の `prev` は前に返した slice を書き換えない。`-race` 緑 |
| resource-leaks | 0 | pane の読み込みが返らないファイルでは、カーソルが離れると `waiting` を下ろす (`stepPane`)。load の goroutine は `loadMax` で 2 本まで。tfperf の tmux は全呼び出しが `-S <out>/tmux.sock` で、TMUX / TMUX_PANE を env から落とし、`stop` で kill-server の後に自分の socket だけを消す |
| error-handling | 0 | perflog の書き込みの誤りは Close の Flush で返す。debuglog の書き損じを捨てるのは理由コメントつき。perflog の `Since` / `Output.Write` は nil で panic するが、`Since` の呼び出しは `a.perf != nil` の内側で、`Output` は perf があるときだけ作る (`main.go`)。`Now`・`Event`・`Flush`・`Close` は nil 安全 |
| security | 0 | 検索の履歴は `!` と同じ 0600 + `O_NOFOLLOW` の追記 |
| performance | 0 | b68b56e19 の確保の削減を読んだ。検索の一致の引き直しは打鍵ごとに開いた木を全部辿るが、`>` の上限 (400 フォルダ) の中で、992・723 の判断と同じ |
| false-green | 0 | 新しい道具 (tfperf・make debug) は合否を返さない計測。tfperf は q を 8 回送っても閉じなければ誤りを返す |
| dead-code | 0 | `deadcode` / `deadcode -test` で 0 |
| ci | 0 | `src_treefiler.yml` の paths は go.mod の replace 7 本 (testwait を含む) を全部覆う。直近 5 run は success |
| ux | 0 | `'` のキー一覧・README への反映あり。キー一覧を分類して広く出す件は、監査中にユーザーから別の依頼として来た (この監査の発見ではない) |
| design / responsibility / polymorphism / leaky-abstraction / encapsulation / ui-components / dependency / test-cleanup / lint-from-done / general | 0 | 992 から構造は変わっていない (load と pane は Model の欄 + 世代で取り込む既存の形に沿っている)。`go.mod` の差分は testwait の module 名の変更 3 行 |

全数: 直した 3 (duplication 1・test-helpers 1・コメント 1)、起票 1 (1020。検証中に見つけたテストの flake で、監査の観点の発見ではない)。

## 反証レビュー

- 2026-11-04、opus の読み取りのみのサブエージェント 1 本に 1019・1020 を「誤りだという前提で反証」させた。P1 は無し。
  採った: 1020 の「`r` で光が出る」は誤り (光は Advance の中でしか出ない) / 1020 の持ち越しの候補 (goroutine より、決まった持ち越し = パッケージ変数・閉じない Model) /
  1019 の母集合に読んでいないファイルが抜けていた / 文言 3 つ (perflog の nil・`layoutWith` の呼び出し・`abs` を書く場所)。
  反証できなかった: load / pane の goroutine が読むもの・`readHistory` が link を辿る・goHome の戻り先・CI の paths・修正 3 点が挙動を変えないこと

## 対応

- `filer/model.go`: `spawnFrom` と `ghostTarget` を `liveAncestorEdge` に寄せる (見つからないときの値だけが違う)
- `filer/load_test.go`: `statusLine` を消して `lastRow` (smallscreen_test.go) を使う
- `filer/history.go`: `appendHistory` のコメントを実装に合わせる

検証: `make -C src/treefiler lint` 0 issues・`go test -race -count=1 ./...` 全 ok。`TREEFILER_VIRT_SEEDS=300` は seed 269 が赤 → 変更の無い本体でも
`GOMAXPROCS=1` で再現したので変更とは無関係 (1020)。

## 進捗

- 2026-11-04: 監査・対応・起票
- 2026-11-04: 決着。「対応」の 3 点は master にある (`liveAncestorEdge` が `filer/model.go` に在り、`load_test.go` の `statusLine` は無く、`history.go` のコメントは `readHistory` が link を辿ると書く。`grep` で確認)。
  seed 269 の flake は [1020](1020-test-treefiler-virt-seed269-flaky-gomaxprocs1.md) が持つので、この記録に残課題は無い。done へ送る
