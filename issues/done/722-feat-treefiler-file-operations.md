# 722 (feat): treefiler でファイルを操作する (新規作成・名前の変更・ゴミ箱へ移す)

起票日: 2026-10-12

## 概要

treefiler から、カーソルの項目に対してファイルの操作をできるようにする (ユーザーの選択 2026-10-12)。今は見る・開く・`!` でコマンドを打つ
だけで、名前の変更や削除は `!` で `mv` / `rm` を打つしかない。

## 仕様 (キーと範囲はユーザー確認済み 2026-10-12)

| 操作 | キー (案) | 中身 |
|---|---|---|
| 新規作成 | `a` | カーソルのフォルダ (ファイルならその親) に名前を入力して作る。末尾が `/` ならフォルダ |
| 名前の変更 | `m` | 足元の入力欄に今の名前を入れて開き、Enter で変える。同じフォルダの中だけ (移動はしない) |
| ゴミ箱へ移す | `d` | 確認のダイアログ (`tuikit/confirm`) を出し、`y` で `~/.Trash` へ移す。**消さない** (`rm` しない) |

- キーの決定 (ユーザー 2026-10-12): 案の `R` / `D` は glogx の F の中で ratelimit (`R`)・doctor (`D`) への横断キーを潰す (filer が `Binds` で持つキーは
  横断より先に取られる) ので、`m` (move / rename) と `d` (delete) にした。`d` はタイルの中では diff のまま (木の上だけの意味)
- 範囲 (ユーザー 2026-10-12 に聞き直した): **複製は外す** (ui-guide §2 の表で `p` は「PR または番号」と決まっていて、§10 の「同じ字は同じ動詞」とぶつかる)。
  新規作成・名前の変更・ゴミ箱の 3 つ。新規作成の `a` は ui-guide §2 の「all / 巡回」とぶつかるが、ファイラーの慣習 (add) として例外に書く (ユーザー確認)。
  `d` の削除は pro-con と doctor に前例がある
- 操作の後は木を読み直し、カーソルは作った・変えた項目へ (ゴミ箱へ移したなら隣の項目へ) 移る。失敗は ✗ の toast
- glogx の `F` の中でも同じキー (filer が `Binds` で持つ)。キー一覧・最下行の案内・README のキー表・glogx-ui-guide §2・spec §0.1 を同じ変更で直す

## 🚨 安全 (破壊的な操作を含むので、着手前に読む)

- ゴミ箱は Finder の「ゴミ箱に入れる」と同じ置き場 (`~/.Trash`) へ `os.Rename` で移す。同じ名前が既にあれば名前をずらす。別のボリュームの項目は
  `~/.Trash` へ rename できない (EXDEV) ので、その場合は操作を断る (複製して消す形にしない。途中で落ちると半分だけ移る)
- 名前の変更・新規作成は既存の項目を上書きしない (先に存在を確かめるだけでなく、上書きしない API で作る: `O_EXCL` / rename の前の再確認)。
  検査から実行までの窓は短く保つ (`~/.claude/rules/sandbox-real-destructive-test-apis.md` の TOCTOU の節)
- 名前は termsafe を通した表示名ではなく、ディスク上の名前 (`node.raw`) と絶対パス (`node.abs`) で操作する。入力された名前に `/` や `..` が
  含まれたら断る (同じフォルダの外へ出さない)
- root (`m.root`) と root の外は操作しない
- テストは本物の rename / 作成を呼ぶので、一時ディレクトリの外への操作を**実行前に拒否する**仕掛けを同じ変更で入れる
  (`~/.claude/rules/sandbox-real-destructive-test-apis.md`)。ゴミ箱の場所もテストでは差し替える (本物の `~/.Trash` に書かない)
- 判断ロジック・破壊的操作の新設なので、commit 前に観点を分けた敵対レビュー (壊す・素通り・並行) を通す
  (`~/.claude/rules/adversarial-review-own-safeguards.md`)

## 対応方針

- 🚨 **ゴミ箱は doctor の `trashMove` (`src/doctor/disk/delete.go`) を共有の module へ切り出して使う。2 つ目を書かない** (`adversarial-review-own-safeguards.md` §0-B)。
  doctor は既に `renameatx_np(RENAME_EXCL)` で上書きしない移動・同名の連番 (`name 2`、64 回まで)・EXDEV を断る・fd を掴んでからの (dev, ino) の照合・
  経路に symlink が無いことの検査・テスト用の砂場の仕掛け (`allowDestructive` + `src/doctor/disk/main_test.go` の 4 段構え・`src/doctor/testtmp`) を持つ。
  doctor は別 module (`module doctor`) で `trashMove` は非公開なので、切り出す先 (新しい module) と、doctor の砂場の仕掛けを切り出した側でも保つ形を決めてから動かす。
  filer の node は ino を持たないので、(dev, ino) は操作の直前に lstat で取り直して渡す
- 入力を握る層は 2 つ増える: 名前の入力欄 (新規作成・名前の変更。`promptState` を一般化して 1 つにできるか先に見る) と、ゴミ箱の y/N 確認 (`tuikit/confirm` は
  板の描画と判定だけで、状態は使う側が持つ)。🚨 `HandleInput` の ctrl+c は検索と `!` の入力中だけ Quit から外しているので、確認の層を足すと ctrl+c がアプリの
  終了になる。issue 715 の再開の trigger に当たるので、715 の案 A (網羅テスト) か案 B を一緒に入れる。層の判定の箇所は filer の中で 9 か所以上
  (`OwnsKeys` / `HandleInput` の ctrl+c と switch / `inputField` / `draw` / `Paste` / `CaretPos` / `SearchQuery` / `drawNames` 等)。glogx 側は `swallowKeyRepeat` と
  `overlay_ownership_test.go` の網羅テストも見る。glogx の `repeatGuardedKeys` に `d` と `a` があり、F の中では 250ms 以内の連打が飲まれる (削除の誤爆には効く側)
- 名前の規則: 新規作成で末尾が `/` ならフォルダ (末尾の 1 つだけを外してから検査)。それ以外の `/`・`.`・`..`・空・255 バイト超は断る。
  🚨 大文字小文字だけの名前の変更 (`readme` → `README`) は、APFS の既定では同じ名前なので `RENAME_EXCL` が EEXIST を返す。許すか断るかを決める
  (Unicode の正規化の違いも同じ)。入力は `lineedit` を通るので改行・タブは空白に、制御文字は落ちる
- 操作の後: 親フォルダだけを読み直す (`applyChange` と同じ `n.reload()`。`Refresh` は root から全部を読み、光らせない)。読み直した基準は watcher の `loadedSig` に
  入るので光は二重に走らない。ただし操作と読み直しの間にポーリングが差分を拾うと `applyChange` が光らせ、カーソルを親へ飛ばしうる。
  カーソル: 名前を変えた項目は新しい node になる (`reload` は同名の node だけを使い回す) ので `findNode` で探して `setCur`。ゴミ箱へ移すなら隣の項目を操作の前に決めておく。
  開いているタイル (`m.tiles`) が消した・名前を変えた項目を指したままにならないようにする
- 操作そのもの (パスの検査・rename・作成) は UI と分けた小さな関数群にし、そこに拒否の仕掛けとテストを集める
- issue 712 の仮想化: 操作で一覧が変わると見せる子の覚えが作り直され、巨大なフォルダでは 0.6 秒全部を動かす (設計どおり)
- 制限として書いておく: 「戻す」(Finder の Put Back) の情報は付かない。別ボリュームの項目は断る (Finder は各ボリュームの `.Trashes/<uid>` を使う)。
  🚨 未確認: フルディスクアクセスの無い端末から `~/.Trash` への移動が EPERM になるか (doctor の実運用で問題が出ていないかを先に見る。実機で確かめるまで事実として書かない)

## 受け入れ条件

- [x] 3 つの操作 (新規作成・名前の変更・ゴミ箱) がキーで動き、読み直しとカーソルの移動が正しい
- [x] 上書きしない・root の外を触らない・`/` や `..` を断る、をテストで固定し、変異で red を確認 (別ボリュームの EXDEV は手元で作れないので未テスト。
  doctor から引き継いだ分岐のまま)
- [x] テストが一時ディレクトリの外を触らない仕掛け (実行前の拒否)
- [x] 敵対レビューを通した

## 関連

- issue 715 (pending。入力を握る層。層を足すので再開の trigger)
- issue 712 (仮想化。一覧が変わるときの扱い)

## 進捗

- 2026-10-12: 起票
- 2026-10-12: キーと範囲をユーザーに確認 (名前の変更 `m`・ゴミ箱 `d`・4 つ全部)
- 2026-10-12: 反証レビュー (sonnet・読み取りのみ) で訂正: ゴミ箱は doctor の `trashMove` が既にあるので切り出して使う (main が `delete.go` で確かめた) /
  `p` `a` が ui-guide §2 の語彙とぶつかる → ユーザーに聞き直し、複製を外して `a` は例外に / 入力を握る層は 2 つ (ctrl+c の扱い) / 大文字小文字だけの名前の変更 /
  新規作成の名前の規則の矛盾 / 操作後の読み直しとカーソル・タイル / 715 の層の判定の全数。反証できなかったもの: `a` `m` `d` が filer と F の横断で空いている /
  タイルの `d` は diff のまま / `node.raw` `node.abs` `m.root` / `tuikit/confirm` `tuikit/lineedit` / x/sys v0.47.0 に `RenamexNp` と `RENAME_EXCL` / 715 と 712 の記述
- 2026-10-12 (dotfiles-4e): 実装した。commit「feat(treefiler): a / m / d でファイルを作る・名前を変える・ゴミ箱へ移す (issue 722)」
  - **共有 module `src/safefs`** を新設: `TrashMove` (doctor の trashMove の本体をそのまま移した)・`Rename`・`Create`・`ValidName`・`Identity`。
    どの操作も syscall の直前に呼び出し側の `Guard` を通し、nil は失敗。テスト用の砂場 `safefs/sandbox` (登録した一時ディレクトリの外を実行前に拒否・
    親を実解決・拒否を記録)。doctor は `trashMove` を薄い包みにして `allowDestructive` を渡す (op 名 `trash-dest` / `trash`・順序・文言は元のまま。
    文言は safefs の sentinel error を doctor 側で付け直す)。doctor の砂場 (4 段構え) は寄せていない (振る舞いを変えないため)
  - treefiler: `filer/fileops.go`。名前の入力は `!` の欄を種類付き (`promptState.kind`) にして使い回し (層を増やさない)、確認の板 (`Model.ask`) だけが新しい層。
    ctrl+c は入力欄と確認の板では取り消し。issue 715 の案 A (`layer` の札 + `filer/layers_test.go` の網羅テスト) を一緒に入れた
  - 決めたこと: 大文字小文字・正規化だけの名前の変更は **RENAME_EXCL がそのまま通る** (実測: APFS の $TMPDIR で `readme`→`README`・NFC→NFD とも err=nil。
    この issue の「EEXIST を返す」は外れ)。一時名を経由する経路を書いたが、届かないので消した。別の名前の hardlink へは EEXIST で断る (テストあり)
  - 入力欄を開いたまま Enter (制御文字を落とした表示のまま) では名前を変えない (`prefill`)
  - 最下行の案内には足さなかった (既存の `y` `S` `O` も載せておらず、幅が足りないと凡例から落とす作り)。`?` のキー一覧・README・spec §0.1・ui-guide §2 に載せた
  - ライブ更新: 操作の後の次のポーリング (1 秒) が変化を拾って作った・変えた項目を光らせる (この issue の「光は二重に走らない」は外れ。害は無く、操作の合図になる)
- 検証: 変異 23 本 (safefs の上書き・識別・名前の検査・検査点・砂場の symlink と登録範囲、filer のカーソル・ctrl+c・OwnsKeys・prefill・砂場の差し替え忘れ、
  doctor の字句ゲートと文言、glogx の F の中の d) を `bin/mutate-verify-list` で当て、全部想定の検査で red。緑だった 1 本 (大文字小文字の一時名の経路) は上の実測で
  届かない経路と分かり削除した
- 敵対レビュー (opus・読み取りのみ、2 本を直列):
  - 1 本目 (壊す・並行): **P2 を実測で再現** — 木を読んだ後で途中のフォルダが外への symlink に差し替わると、古い node 越しに d / m / a が root の外に効いた。
    → `safeDir` (root だけ symlink を辿り、そこから 1 段ずつ Lstat してフォルダでないものを断る) で直し、`TestOpsDoNotFollowSwappedSymlinkFolder` で固定 (変異で red)。
    P3 doctor の文言が変わった → sentinel error にして doctor 側で元の文言へ戻した
  - 2 本目 (素通り・回帰・1 本目の修正の 2 周目): P1 なし・`safeDir` は壊せず。素通り 2 件 (doctor の文言の付け直し・safefs の移動元の検査点) をテストで固定 (変異で red)。
    2 周目の修正は判定ロジックを足さない・変異で確認済みなので周回はここで打ち切った
- 未確認リスク・記録 (直していない):
  - safefs の照合 (Fstatat) から renameatx_np までの間に、同じフォルダで同じ名前が別の項目へ差し替わると、別の項目を移す・名前を変える (失うもの・上書きは無い)。
    doctor の trash も切り出し前から同じ (removeItem だけが事後に照合して戻す)。`safeDir` の確認から safefs が親を開くまでの窓も残る (root の fd から openat で辿れば閉じられる)
  - root が付け替えられる symlink (`current -> releases/N`) のとき、操作は付け替えた後の先に効く (root のパスの外には出ない)
  - `~/.Trash` はフルディスクアクセスの無いプロセスから開けないことがある (レビューのセッションで EPERM。原因は未切り分け)。失敗は「ゴミ箱を開けません」で止まる。doctor も同じ
  - 名前の前後の空白は落とさない (末尾が空白の名前は正当なので)。貼った改行は空白になる
  - safefs の中には doctor の `TestDestructiveCallsGoThroughHook` に当たる字句ゲートが無い (検査点は実行時のテストだけが守る)。`sandbox.Allow` の範囲は $TMPDIR 次第
- 最後の編集 (5f の 726 / 727 の後へ rebase。applyChange の `dropGoneRecs` を共有の `reloadDir` へ寄せた) の後に: `make -C src/treefiler lint test`・`make -C src/glogx lint`・
  glogx の `go test -race`・`TREEFILER_VIRT_SEEDS=300 go test -run TestVirtualizedRandomOps ./filer`・root の `make test` が rc=0。
  1 回目の root の `make test` では glogx の `TestOwnSourceRootsCoverLocalReplaces` (safefs の replace を走査の根に足し忘れ。直した) と、
  無関係の 3 本 (`tests/tmux/test_reap_orphan_servers.sh` は master でも単独で rc=127・`test_restore_runner.sh` と `tests/claude/test_claude_mods.sh` は単独では緑) が赤だった。2 回目は全部緑
