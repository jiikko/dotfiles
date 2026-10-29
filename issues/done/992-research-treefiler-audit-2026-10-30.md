# 992 (research): src/treefiler の 22 観点の監査 (2026-10-30。直接実行) — 全数勘定と、直さないと決めたもの

起票日: 2026-10-30

## 概要

`/audit` で src/treefiler に 22 観点を直接実行 (main が読んで判定) で回した。前回の監査 ([737](737-research-treefiler-audit-2026-10-12.md)、2026-10-12) は
全体を見ているので、それ以降の commit (`87eeae19b..c6c6b6329` で src/treefiler に触れた 57 本、78 ファイル・+4466 / -801 行。監査中に別のセッションの pulse の commit が入った) で入ったコードを重点にした。
直す項目は [991](991-bug-treefiler-ask-board-invisible-on-small-screen.md) の 1 本。

## 走査した母集合

- production の Go: 35 ファイル・9,176 行 (`c6c6b6329` で `find src/treefiler -name '*.go' -not -name '*_test.go'`)。テストを含めると 80 ファイル
- 全部を読んだ: `filer/goto.go`・`filer/explode.go`・`filer/ask.go`・`filer/places.go`・`main.go`・`termsync/termsync.go`。
  部分: `filer/model.go` (rerootUp・draw・statusBar・LayerHints)・`filer/tile.go` (restoreTile)・`filer/git.go` (withSep)・`tools/tfprobe/main.go` (run・settle・一時ディレクトリ)・
  書き込む 3 か所 (history・places・settings) の mode。**layout / virt / pulse / tileview の変更は精読していない** (別のセッションが同じ時間帯に pulse を直していた)
- 機械の検査: `go vet ./...` rc=0 / `go test -race -count=1 ./...` 全 package ok / `deadcode` と `deadcode -test` ともに 0 件 /
  `bin/dupscan sweep src/treefiler` (単位 688)

## 観点ごとの結果

| 観点 | 結果 | 攻めた所・報告しなかった理由 |
|---|---|---|
| design / responsibility / polymorphism / leaky-abstraction / encapsulation / ui-components | 0 | 737 で見た構造 (Model の欄・仮想化) から変わったのは機能の足し込みだけ。確認の板は `askBoard` に寄っていて (`d` と `>` が同じ板)、層の札と `layers_test.go` の対もある |
| duplication | 0 | sweep の節 1・5・6 は 0。節 2 は `settings.go` の項目の表のクロージャ (表の行どうしの形が同じなのは意図どおり) と、`model.go` の init の薄い委譲 (印つき)。節 3 の `explodeJob` の lock 付きの getter 3 本は 3 行ずつで寄せる価値が無い |
| ux | 1 (→ 991) | ほかに、tmux の popup の `TREEFILER_TOGGLE_KEY` は prefix のキーを飲む (`main.go` の `takeToggle`)。prefix が `C-b` なら popup の中で `ctrl+b` (← の別名) が効かない。今の `_tmux.conf` の prefix は `C-t` で、treefiler は `ctrl+t` を使わないので発火しない。**再提起の条件**: prefix を treefiler が使うキーに変えたとき |
| security | 0 | `!` の履歴は 0600 + `O_NOFOLLOW`。`:` の `~` は自分の家だけを展開し、`~user` は root からの相対として読んで「移動できません」になる。places / settings は 0644 (秘密を書かない) |
| resource-leaks | 0 | tfprobe の一時ディレクトリは defer で消す。設定の置き場を一時ディレクトリへ差し替えて本物を書かない (`TREEFILER_CONFIG_DIR`)。`settle` に上限がある |
| broken-code | 0 | `:` の root が `/` のとき (`withSep` が `//` にしない)・root の外のパスで rerootUp が上がれないとき (`rootUndo` で戻す) を読んだ |
| dead-code | 0 | `deadcode` / `deadcode -test` で 0 |
| error-handling | 0 | places の読み切れない記録は書き戻さない (`readPlaces` の ok)。`savePlace` の書き込みの失敗は理由コメントつきで捨てる (次に開いたときに戻らないだけ) |
| false-green | 0 | tfprobe は合否を返さない計測の道具で、段の後の状態 (空振り) を summary に出す |
| performance | 0 | `explodeJob` は辿るたびに `dirs` を複写する (最大 400 件で 2 乗でも約 8 万要素。直す価値が無い) |
| concurrency | 0 | `explodeJob` の `canceled` / `shown` を `takeExplode` が lock 無しで読むが、`done` を lock の中で見た後で、`cancel` も同じ UI の goroutine から呼ぶ。`-race` で 0 |
| test-cleanup / test-helpers | 0 (浅い) | 変異は当てていない。直近 (2026-10-29) に helper を `testing.TB` に寄せる commit が入っている |
| dependency | 0 | `go.mod` の replace と workflow の paths は 737 で突き合わせ済み。今回の差分で足された依存は無い (`go.mod` の変更は 5 行) |
| ci | 0 | 未実施 (前回の監査で paths の漏れ 0。今回の差分は workflow を触っていない) |
| lint-from-done | 0 | 991 の形 (描けない大きさで描画を捨てるのに状態は残す) は、層の札 (`layer:"…"`) の検査に「描けないときの代わりの表示」を足せるかで固定できる。991 の対応で見る |
| general | 0 | 上の各行 |

全数: 発見 1 (991。確認の板と設定の板の 2 か所)、記録だけ 1 (ux の prefix)。

## 反証レビュー

- 2026-10-30、opus の読み取りのみのサブエージェント 1 本に「誤りだという前提で反証」させた。991 の中心の主張 (しきい値・`askKey` への経路・`d` は Enter で実行・`statusBar` に分岐が無い・最小の端末サイズの検査が無い) と 992 の数字は反証できなかった
- 採った: P2「設定の板 (`drawPanel`) も描かずにキーを取り、見えないまま値を変える」→ 991 に含めた (私は ask だけを見て、同じ形の層を横に洗っていなかった。lint-from-done の行の「層の札の検査に代わりの表示を足す」はこの横展開の形) /
  P3「popup は枠があるので、しきい値は端末の大きさと違う」→ 991 の発火条件に書いた / P3「受け入れ条件が弱い (切れた案内でも通る)」→ 答えのキーが残ることを条件にした

## 結果 (2026-10-30)

- 991 は対応して done。991 の修正の反証レビュー 2 周目で、設定の板の修正前からの panic (読めない設定の行が多いと範囲外) が見つかり、991 の中で同じ判定に寄せて直した
- 残課題は無い (記録だけの項目には再提起の条件を書いた)
