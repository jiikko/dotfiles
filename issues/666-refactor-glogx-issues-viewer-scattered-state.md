# 666 (refactor): issues viewer の状態の更新が複数の関数に散り、畳み忘れが compile を通る

起票日: 2026-10-07

## 概要

issues viewer (`issuesView`) と周辺で、対で更新すべき状態の更新責務が 1 つの型に寄っておらず、新しい経路や状態を足すと
畳み忘れが黙って入る箇所。どれも今は正しく動く (誤動作は未発火)。trigger 待ちのものは trigger を明記する。

出典: glogx issues viewer 監査 (668) の responsibility D・E / encapsulation F / polymorphism J / duplication D1・D3 / ui-components U3。

## 詳細

### E. group 展開状態の 3 map (`expandedGroups` / `autoExpandedGroups` / `collapsedGroups`)

- 書くのは 7 関数 (`applyScreen` / `finishClose` / `anchorCursorInternal` / `pruneExpandedGroups` /
  `refresh` / `toggleGroupAtCursor` / `clearNumberFilter`)、nil なら make が 5 か所。表示の判定として読むのは `groupExpanded` だが、
  `toggleGroupAtCursor` / `clearNumberFilter` が `autoExpandedGroups[key]` を、`pruneExpandedGroups` と保存処理が `expandedGroups` を直接読む (反証レビューの数え直し)
- 不変条件: `collapsed` は番号フィルタ中だけ意味を持ち、auto 展開を打ち消すためだけにある。これを `clearNumberFilter` と `finishClose` の 2 か所が守る
- 発火条件: 番号フィルタを解く 3 本目の経路でリセットを忘れると、解除後も畳んだ状態が残り、手動で展開した group が開かない
- 方針: `groupExpansion` 型 (toggle / reveal / endFilter(promote) / prune / saved / expanded)。**寄せる前に各呼び出し側の例外を移す**:
  `anchorCursorInternal` は collapsed を意図的に上書き (コメントの P3-5) / `clearNumberFilter` は現在行の group だけ auto を expanded へ引き継ぐ /
  `applyScreen` は Cursor / Open の group を足す

### D. 本文 pager の差し替え時のリセットが 5 関数に散る (trigger: issue 662 に着手するとき)

- フィールド `open` / `body` / `docStack` / `docLine` / `bodyPager` / `urlPick` / `linkJump` / `linkRepos` / `drawer` を、
  `openIssue` / `openDoc` / `popDoc` / `discardBody` / `closeBody` がそれぞれ別の組み合わせで畳む
- 発火条件: 文書ごとの状態を 1 つ足し、どれかで畳み忘れると前の文書の状態が次へ残る
- `openDoc` は issue でない `.md` を `issues.Issue` に包むので Status がゼロ値 = Open になる (今は `docStack` の分岐で無害)
- 方針: `docPager` 型 (open / push / pop / replaceBody / discard)。文書は Issue ではなくパス・表示名・Dir で持つ。
  issue 662 (内蔵ファイラー) が本文 pager を流用候補に挙げているので、第二の利用者が出るそのときに切り出す。今は先回りしない

### F. tui.go の resize が viewer 内部の滑走を直接 Stop

- `issuesOv.bodyPager.Stop()` / `issuesOv.curGlide.Stop()` を tui.go が直接呼ぶ (進める側は `advanceGlide` メソッド)。diff / status の板も同じ形
- 発火条件: viewer に滑走を足して tui.go を直し忘れると、resize 後に古い基準で滑る
- 方針: viewer に `stopGlides()` を置く

### D1. fsnotify の見張りチェーンが issues_watch.go と gitlog_watch.go に 2 実装 (既に分岐)

- 開始・停止・イベント待ち・ポーリング・札の降ろし方が同じ形 (gitlog_watch.go の冒頭に「方式は issues viewer の見張りと同じ」)
- 既にずれている: watcher が死んだとき gitlog は `gen++` して `pollArmed` を降ろしポーリングを張り直すが、issues はしない。
  消えて戻ったディレクトリの再登録の契機も違う (gitlog はポーリング、issues はスキャン結果)。**issues 側の扱いが誤りかは未確認**
- 方針: `watchChain` (EventCmd / PollCmd / Release / OnClosed / Stop) を共有し、見張る集合と指紋は各画面に残す (issue 271 で集合は別物と決まっている)

### D3 / U3. 入力・リストの部品の手組み (小)

- `urlPicker.lines` (url_picker.go) が `listnav.WindowOffset` を使わず窓の位置を手で計算し、offset を持たない。
  一致数が窓より多いとき下へ送ってから上へ戻すと、一覧 (窓をできるだけ動かさない) と手触りが違う。同関数の `else` 節は作った `line` を捨てて作り直している。
  同関数のコメント「issues 一覧と同じ規律。offset を状態で持たない」は古い (issues 一覧は今 `listnav.WindowOffset(v.offset, …)` で offset を状態で持つ) ので一緒に直す
- カーソル行の描き方 (`cursorGutterMark` + `cursorPaint`) が `rowLine` / `groupLine` / `urlPicker.lines` の 3 か所
- 見出し + `lineedit` の表示窓 + キャレット桁を `urlPicker.field`/`caretCol` と `issuesView.numberField`/`caretCol` の 2 か所で手組み
- 方針: `urlPicker` に offset を持たせ `listnav.WindowOffset` を通す / `paintCursorRow` にまとめる (`statusCursorPaint` は意図的に別なので含めない) /
  `promptField{prompt, line}` の `Render` と `CaretCol`

### J. 疑似タブ ([next] / All) の特別扱いが約 8 か所 (trigger: 2 つ目の疑似タブが要るとき)

- `tabIndexOf` / `currentTab` / `rowsForTab` / `refresh` / `moveTab` (-1 起点の算術) / `tabLine` / `tabChip` ×2
- 方針: そのときに `{name, label, color, rows}` の表にする。今はやらない

## 関連ファイル

- `src/glogx/issues_view.go` / `src/glogx/issues_watch.go` / `src/glogx/gitlog_watch.go` / `src/glogx/url_picker.go` / `src/glogx/tui.go`

## 進捗

- [ ] E: `groupExpansion` 型
- [ ] F: `stopGlides()`
- [ ] D1: `watchChain` の共有 (issues 側の watcher 死亡時の扱いを先に確かめる)
- [ ] D3 / U3
- D / J は trigger 待ち (上記)
