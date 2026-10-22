# 858 (bug): diskdoctor の DerivedData の使用中判定が、他のプロセスが掴むリソースファイルでも止まる

起票日: 2026-10-22

## 概要

issue 856 で足した `GuardDerivedDataLive` は、`lsof -d txt` に出たパスが `DerivedData/<プロジェクト>/` の配下にあれば
「使用中」として候補から外していた。txt には実行ファイル・dylib のほか、mmap したデータファイルも出る。
そのため、起動していないアプリのリソースを他のプロセスが掴んでいるだけで、そのプロジェクトが消せなくなる。

## 詳細

- 実測 2026-10-22: NotificationCenter (pid 1088、9/17 から常駐) が、glogx の削除で改名された置き場の中の
  `~/Library/Caches/.glogx-delete-<id>/chromium-1193/…/Chromium Helper (Alerts).app/Contents/Resources/base.lproj/InfoPlist.strings`
  を txt で掴み続けていた (置き場は既に無い。`chromium-1193` の名前から Playwright のキャッシュだったと推定)。
  通知を出したアプリのバンドルの文言を読み込んで、そのまま持っている。Chromium は動いていない
- 同じ日の lsof の txt では、システムの常駐 (chronod 28 件・proactiveeventtrackerd 4 件など) が `~/Library/Caches` や
  `~/Library/Developer` の配下のデータファイルを掴んでいた
- DerivedData から起動したアプリが通知を出すと、そのアプリの終了後も NotificationCenter がバンドルの文言を掴み、
  プロジェクトが NotificationCenter の再起動まで「使用中」のままになりうる (Obaket で起きるかは未確認)

### 訂正 (issue 856 の「範囲外の観測」)

issue 856 は「Playwright の Chromium が動いている間に `playwright-cache` を消したと推定」と書いたが誤り。
削除済みのパスを掴んでいたのは Chromium ではなく NotificationCenter だった (pid 1088 の実行ファイルが
`/System/Library/CoreServices/NotificationCenter.app/Contents/MacOS/NotificationCenter`)。改名先の
`~/Library/Caches/.glogx-delete-*` は既に無く、削除で壊れたものは見つかっていない。
`playwright-cache` に同じ guard を付けると、この掴みで消せなくなるので付けない。

## 対応方針

- 使用中と読むのは、配下の **Mach-O** (実行ファイル・dylib・framework の本体) を txt で掴んでいるときだけにする
  (`pathHeldUnder` → `machOOrUnknown`)。判定は先頭 4 バイト。実測 2026-10-22: DerivedData の実行ファイルと
  `.debug.dylib` は `cffaedfe`、`/bin/ls` と `/usr/lib/dyld` は `cafebabe`、`Assets.car` は `424f4d53` ("BOMS")
- 中身は**掴まれているファイルそのもの**から読む。lsof から inode も取り (`-Fni`)、開いた後の fstat の inode が違えば
  判定できないとして使用中と読む。lsof が出すのは vnode に最後に付いた名前で、消された後に同じ名前で作られた別の
  ファイルを読むと取りこぼすため (敵対的レビューが実機で再現: 起動中の実行ファイルを `mv` → `rm` → 同じ名前に
  テキストを書くと、lsof は消えた vnode を古い inode のまま出し続けた)
- 中身を読めないもの (消されたファイル・inode の違うファイル・inode の無い行・通常ファイルでないもの・開けない・
  読み取りの失敗) は使用中と読む (fail-closed)。4 バイトに満たないファイルは Mach-O ではない
- DerivedData から起動したアプリは自分の実行ファイルを txt に持つので、本体は取りこぼさない (起動中の Obaket で確認。
  macOS の dyld が実行ファイルを mmap する仕組みによるもので、確かめたのはこの 1 例)

### 守らないもの (この変更で増えたもの)

- **消されたファイルを掴まれているプロジェクトは止まったまま** (fail-closed)。lsof は掴んだ時点の名前を出し続けるので、
  NotificationCenter が DerivedData のビルドの文言を掴んだ後に、ディレクトリを残したままその文言ファイルが消える・
  置き換わると、そのプロジェクトは使用中と読まれ続ける。読めないものを使用中と読むのは、起動中のアプリの実行ファイルが
  ビルドし直しで消された・置き換わった場合 (そのアプリは遅延読み込みでまだ配下を使う) を守るため
- 実行ファイル以外 (リソース・データファイル) だけを mmap しているプロセス。例: DerivedData のビルドの
  `Assets.car` だけを読み込んでいる別のプロセス。アプリ本体は実行ファイルで捕まる
- `cafebabe` は Java の class ファイルとも同じ値なので、それを掴まれていると使用中と読む (守る側)
- **inode を変えずに中身を書き換えた実行ファイル** (`printf hello > <起動中の実行ファイル>`)。inode が一致するので新しい中身を読み、
  Mach-O でなければ使用中と読まない (2 周目の敵対的レビューが実機で再現)。直さないのは、inode の一致だけで使用中と読むと
  NotificationCenter が掴むリソースで止まる問題 (この issue の対象) が戻るため。起動中の実行ファイルを上書きした時点で、
  読み込み済みのページも同じファイルなのでアプリ自体が壊れている。Xcode のリンカは別のファイルを作ってから名前を差し替える (推測)
- dev (ボリューム) を比べていない。掴まれている間は inode が解放されないので、同じボリュームで番号が再利用されることは無い。
  一致しうるのは起動の後に配下へ別のボリュームをマウントしたときだけ
- 他のプロセスが Mach-O を掴んでいる間も止まる (守る側): lldb / Xcode が dSYM (中身の DWARF も Mach-O) を mmap している、
  swift-plugin-server が DerivedData の macro plugin を読み込んでいる、など (推測。2026-10-22 の lsof では未観測)

## 関連ファイル

- `src/doctor/disk/guard.go` (`pathHeldUnder` / `machOOrUnknown` / `machOMagics`)
- `src/doctor/disk/deriveddata_live_test.go`
- issue 856

## 進捗

- [x] 判定を Mach-O に絞る・テスト (`make -C src/doctor test` / `lint`、`make test-lint` 緑)
- [x] 変異検証 1 回目 (`mutate-verify` で 4 本。全部想定のテストで red。inode の比較を足す前)
  - Mach-O の検査を外す → `TestDerivedDataIgnoresMappedResources`
  - 読めないファイルを使用中と読まない (Stat 失敗で false) → `TestDerivedDataHeldFileKinds/消されたファイル`
  - `cafebabe` を magic から外す → `TestDerivedDataHeldFileKinds/universal_binary`
  - 4 バイト未満も使用中と読む → `TestDerivedDataHeldFileKinds/4_バイト未満`
- [x] 変異検証 2 回目 (inode の比較を足した後。全部想定のテストで red)
  - inode の比較を外す → `TestDerivedDataReplacedFileIsHeldByInode`
  - inode の無い行を使用中と読まない → `TestDerivedDataHeldWithoutInode`
  - 開けないファイルを使用中と読まない → `TestDerivedDataHeldFileKinds/読めないファイル`
  - Stat の失敗を使用中と読まない → `TestDerivedDataHeldFileKinds/消されたファイル`
  - Mach-O の検査を外す → `TestDerivedDataIgnoresMappedResources`
  - 通常ファイルの検査を外す → `TestDerivedDataHeldFileKinds/FIFO` が開いたまま止まり、30 秒のタイムアウトで落ちる。
    ディレクトリのケースは読み取りの失敗 (EISDIR) で使用中になるので、この変異では緑のまま (検査の役目は FIFO で止まらないこと)
  - 1 回目は 2 本が構文エラー (変数が未使用になる) で判定不能になり、`false &&` の形で当て直した
- [x] 反証・敵対的レビュー (下節。直した差分に 2 周目を当てた)

## 反証・敵対的レビューの結果 (2026-10-22)

観点を分けて 2 本 (壊す側 = opus / issue の反証とテストの素通り = sonnet)。

- 採用 (P2、opus が実機で再現): 消えた実行ファイルの名前で別のファイルが作られると、名前で読んで取りこぼす。
  lsof の inode と開いたファイルの inode を比べる形に直した (`TestDerivedDataReplacedFileIsHeldByInode`)
- 採用 (記述の訂正): コードのコメントの「置き換わったファイルは開けない」は誤り (開けて新しい中身を読む)。
  issue の下書きの「lsof は掴んだ時点の名前を出す」も誤り (rename に追従し、vnode に最後に付いた名前を出す)
- 採用 (P2、sonnet): 消されたファイルを掴まれているプロジェクトが止まったままになることを「守らないもの」へ。
  「Playwright の」は名前からの推定と明記
- 採用 (P3): テストが通らない分岐 (開けない・通常ファイルでない・空ファイル・magic の残り 4 値・inode の無い行) を
  `TestDerivedDataHeldFileKinds` / `TestDerivedDataHeldWithoutInode` で踏む
- 記録のみ: dSYM / macro plugin を他のプロセスが掴む間は止まる (守る側。上の「守らないもの」)
- 2 周目 (opus、inode の比較を壊しにいく): `-Fni` の解析と inode の比較は**壊せなかった**。本物の行は p / f / i / n だけ
  (p=659 / f=13088 / i=13088 / n=13088)、i が n の後なら inode 無し = 使用中、inode の持ち越しは f / p で切れる。
  採用 (P3): fat64 の magic (`cafebabf` / `bfbafeca`) を足した。記録のみ (P2): inode を変えない上書き、dev を比べないこと
  (どちらも上の「守らないもの」)
- 1 周目で壊せなかった攻め筋: 大文字小文字の違い、symlink (lsof は解決済みのパス)、ハードリンク (中身が同じ)、消えたファイル、
  ld の relink の途中 (先頭 4 バイトを 3997 回読んで全部 `cffaedfe`、残り 3 回はファイル無し = 使用中)
