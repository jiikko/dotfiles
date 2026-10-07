# 665 (bug): issues viewer の画面側が `issues/` パッケージの規則を写して持ち、waiting のバッジと空の状態ディレクトリの見張りが欠けている

> 🚨 **担当中: claude (glogx 監査の修正セッション)**（2026-10-08〜）

起票日: 2026-10-07

## 概要

`src/glogx/issues/` が正本として持つ規則 (どの状態を見せるか / どこを走査するか / 何で並べるか / 物理配置) を、
画面側や同パッケージ内の別の関数が手で写している。2 件はすでに食い違って誤動作しており (A / B)、残りは今は一致しているが、
次に規則を変えるときに黙ってずれる。

出典: glogx issues viewer 監査 (668) の polymorphism A / leaky-abstraction B・G・H・I / encapsulation C。

## 詳細

### A. タブ行のバッジから waiting (◌) が抜ける (誤動作。main がコードで確認)

- `issues.StatusFilter.shows` は `StatusPending, StatusWaiting` を同じ段 (`FilterPending`) で見せる (正本。default 無し switch で lint が守る)
- 一方 `StatusFilter.Badges` は `StatusPending.Badge()` / `StatusDone.Badge()` を手書きで足すだけで waiting を含まない。
  `VisibleBadges` も同じ形 (監査体の読み)
- 発火条件: `a` で pending 段に進めると waiting の issue が ◌ 付きで並ぶのに、タブ行のバッジは `○⏸` のまま。
  番号フィルタが waiting に当たる場合も括弧内に ◌ が出ない (監査体が overlay のテストで再現)。issue 296 が直したのと同じ種類の嘘。
  この repo の `issues/waiting/` には 2 件あるので実際に起きる
- issue 331 (waiting の追加) は一覧の行のバッジだけを扱い、タブ行のバッジに触れていない
- 方針: 表示順の状態リスト (Open, Pending, Waiting, Done) を 1 つ持ち、`Badges` / `VisibleBadges` を `shows` から導出する。
  ◌ を ⏸ に畳むと決めるなら、その判断を spec とコードに書く

### B. 空の global 状態ディレクトリへの新規作成を見張りが拾わない (誤動作。main がコードで確認)

- `issues_watch.go` の `issuesWatchDirs` は baseDir 自体・`next/`・`epic/` とその group (空でも) を見張るが、global の
  `done/` / `pending/` / `waiting/` は issue の `filepath.Dir(Path)` からしか入らない
- 発火条件: 空の `issues/waiting/` へ `git mv` ではなく新規作成で md を置くと、見張り対象外で指紋も変わらず、
  `r` か他の変化まで一覧に出ない (監査体が overlay のテストで `watched=false` / `fingerprint changed=false` を確認)
- spec §5 は global を「ファイルのある状態ディレクトリ」と範囲明記しており**仕様の範囲内**だが、group 側だけ空でも見張る非対称の理由は書かれていない。
  `startWatch` のコメント「新しいサブディレクトリの作成は…Add されて追従する」は空のディレクトリについて言い過ぎ
- 走査は `issues/parse.go` の `scanDir` / `scanEpicDir` が別に列挙しており、spec に「揃えろ」と書かれた写しの関係。
  `TestEpicChildStatusReachedByAllThreeConsumers` は group の予約名についてだけ揃いを守る
- 方針: `issues.WatchDirs(dirs, all)` を走査と同じ表 (`statusDirs` / `EpicChildStatus`) から導出する形で `issues/` に置き、fsnotify は main に残す。
  予約外のサブディレクトリまで全部見張る案は fd のコスト (issue 271) があるので採らない

### C. 「issue が物理的にどこに居るか」の述語が 3 通り (潜在。過去に実害あり)

- `issues/move.go` の `MoveToSubdir`: `GroupKind==Epic || GroupKey!=""` / `issues/nextlink.go` の `NextLinkPath`・`isOpenPlacement`: `GroupKind==Epic && GroupKey!=""` /
  `issues_view.go` の `unmarkDestLabel`: `GroupKey!=""`
- 今は迷子の子が `NextLinkPath` に届かないので等価 (監査体が経路を確認)。ただしこの取り違えで迷子を epic の外へ運ぶ bug が過去に出ている (`move.go` のコメント)
- 方針: `Issue.ContainerDir()` を置き、物理配置の判定はそれだけを通す

### G. 並び順の規則が二重 (潜在)

- `issues.sortIssues` / `numOf` と画面側の `sortDisplayUnits` / `issueNumberOK`。後 2 者は書き方が違う (`numOf` はエラー時 `(0,false)`、
  `issueNumberOK` は `(n, err==nil)` で範囲外のとき Atoi の飽和値が入る) が、bool の結果は同じで並びは実質等価。画面側の再ソートは rows に group があるときだけ効く
- 方針: `issues` が比較関数を公開し、画面はそれを使う。`sortDisplayUnits` の末尾 3 分岐が同じ `a.key < b.key` を返している点も併せて畳む

### H. `issues.Filter` は全件を渡される前提 (潜在)

- other タブの所属判定と閉じた epic の判定を、渡された集合から作る。絞った集合を渡すと黙って誤判定する。今の呼び出し 3 か所はすべて `v.all`
- 方針: doc に事前条件を書く (型で強制するほどではない)

### I. 移動を追う同一性の範囲が `issues/` と食い違う (潜在。コード上の経路のみ)

- `issues_view.go` の `matchByBase` は全 issue ディレクトリを basename だけで照合する。`issues.conflicts` は (Dir, basename)
- 発火条件: root の `issues/` と `macOS/issues/` を両方持つ repo で、開いていた issue が消え、もう一方の dir に同じ basename が 1 件ある → 別 dir の issue に繋ぎ直す。
  ただし `rebindOpenIssue` は `LoadMeta` で見出しを突き合わせ、違えば「見出しが変わりました (別 issue の可能性)」を出す (issue 277 の防御)。
  **黙って繋がるのは見出しまで同じとき (と見出しが読めないとき) だけ**
- 方針: 照合を同じ Dir に絞る (`n` も `git mv` も Dir 内の移動なので、追う範囲は失わない)。issue 277 は macOS/issues の事例を検討したが、Dir に絞る案は検討していない

### ついでに直す文書の乖離

- `src/glogx/issues/issues.go` の package doc が、存在しない `markdown.go` / `wrap.go` / `render.go` と depguard の `render-pure` を挙げている
- `docs/issues-viewer-spec.md` §3 の「受けない名前の配下は走査対象外 = 一覧に出ない」と `parse.go` の `EpicChildStatus` の doc が、
  直後の「迷子として出す」(issue 291 以降の実装) と矛盾している

## 関連ファイル

- `src/glogx/issues/parse.go` (`StatusFilter.shows` / `Badges` / `VisibleBadges` / `Filter` / `sortIssues` / `scanDir` / `scanEpicDir`)
- `src/glogx/issues/move.go` / `src/glogx/issues/nextlink.go` / `src/glogx/issues/issues.go`
- `src/glogx/issues_watch.go` (`issuesWatchDirs` / `issuesFingerprint` / `startWatch`)
- `src/glogx/issues_view.go` (`unmarkDestLabel` / `sortDisplayUnits` / `issueNumberOK` / `matchByBase`)
- `docs/issues-viewer-spec.md` §3・§5

## 進捗

- [x] A: バッジと hint の「a で増えるもの」を `shows` から導出 (`badgeOrder` / `Badges` / `VisibleBadges` / `AddedBadges`)。
  hint の `a: +⏸` も waiting を言わない 3 本目の写しだったので一緒に寄せた。段階のバッジは `○⏸◌` / `○⏸◌✓` になる (spec も更新)。
  回帰: `TestStatusFilterBadgesFollowShows` (全段階 × 全状態)・`TestVisibleBadgesMarksFilterBypass` に waiting 3 件。
  変異: `Badges` を旧来の手書き (open / pending / done) に戻す → 両テストが red
- [x] B: `issues.IsStatusDir` (走査と同じ `statusDirs`) で baseDir 直下の状態ディレクトリを空でも見張る。next/ の特別扱いはこれに吸収。
  spec §5 と `startWatch` のコメントを直した。回帰: `TestIssuesWatchDirsIncludeEmptyGlobalStatusDirs` (予約外の notes/ は見張らないことも固定)。
  変異: `issues_watch.go` を旧版に戻す → red
- [x] C: `Issue.ContainerDir()` に `MoveToSubdir` / `NextLinkPath` / `isOpenPlacement` / `unmarkDestLabel` を寄せた。
  Scan を通っていない Epic の Issue を拒む守りは `MoveToSubdir` に残した。迷子では `NextLinkPath` の結果が変わるが、`isOpenPlacement` が
  どちらの基準でも偽 (Rel に区切りが入る) なので目印を置く経路には届かない。変異: `ContainerDir` を GroupKind で判定する形 → `TestMoveKeepsStrayGroupChildInsideEpic` が red
- [x] G: `issues.OrderLess` / `issues.Number` を公開し、`sortIssues` と `sortDisplayUnits` の両方がこれを使う。画面側の `issueNumberOK` と同じ値を返す 3 分岐を削除
- [x] H: `issues.Filter` の doc に「全件を渡す」事前条件を書いた
- [x] I: `matchByBase(dir, base)` で開いていた issue と同じ issue dir に絞った。回帰: `TestIssuesViewRebindOpenStaysInsideIssueDir`。変異: Dir の条件を外す → red
- [x] 文書の乖離 2 件 (`issues/issues.go` の package doc / spec §3 と `EpicChildStatus` の doc)
- `make -C src/glogx lint` 0 issues / `make -C src/glogx test` rc=0 (2026-10-08)
- [ ] 敵対的レビュー (663〜667 の修正をまとめて通す)
