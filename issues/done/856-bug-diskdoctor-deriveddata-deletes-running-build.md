# 856 (bug): diskdoctor の DerivedData 候補が、起動中のアプリのビルドも消す対象に含める

起票日: 2026-10-22

## 概要

カタログの `xcode-deriveddata` (`src/doctor/disk/catalog.go`) は `~/Library/Developer/Xcode/DerivedData/*` を
guard なし・`DeleteVia: "rm"` で候補にしていた。DerivedData から起動したアプリ (Debug ビルドを直接起動したもの・
テストのホスト) が動いている間も、そのプロジェクトのディレクトリが候補に入り、glogx の `D` 画面から消せた。

## 詳細

- 2026-10-22 に DerivedData (約 220GB) を手で消したとき、起動中の Obaket の Debug ビルド
  (`DerivedData/Obaket-<hash>/Build/Products/Debug/Obaket.app`) が中に在った。そのディレクトリだけ残して他を消した
- 起動中に消すと、遅延して読み込む framework・resource が無くなり、アプリが落ちうる (実行ファイルそのものは
  開いている inode が残るので、すぐには落ちない)。データは失わないが、使っている最中のアプリを壊す
- 削除の経路: glogx の `D` 画面 → `disk.Delete` → `planDelete` が削除の直前に `Scan` を回し直す。
  走査の guard がそのまま削除の直前の判定になる (`guard.go` の `excludedRootFor` の注記)

## 対応方針

- 新しい guard `GuardDerivedDataLive` を足し、`xcode-deriveddata` に付ける
- 判定は `lsof -nP -d txt -Fn` (起動中のプロセスが実行ファイル・読み込み済みのライブラリとして掴んでいるパス)。
  そのパスが `DerivedData/<プロジェクト>/` の配下にあるプロジェクトだけを候補から外す。他のプロジェクトは候補に残す
  - プロセス名 (pgrep) は使えない: アプリの名前はプロジェクトごとに違う
  - 実行ファイル (ps の comm) だけでなく txt を見る: 本体が別の場所にあり、DerivedData の dylib だけを読み込む
    プロセス (テストのホスト) も拾う
  - 実測 2026-10-22 (lsof 4.91): 26,428 行 / 0.28 秒、stderr は空、rc=0。起動中の Obaket が本体と
    `Obaket.debug.dylib` の 2 行で当たった
- 比較の前に、両側を同じ正規化 (`canonicalPath`。symlink の解決) にそろえ、大文字小文字は無視する
- lsof が失敗した (実行できない・rc≠0・パスが 0 件) ときは failed (fail-closed)。全プロジェクトが落ちたら blocked
  (0 件で畳まれて「候補なし」に見えないように)

### 守らないもの

- **ビルドの最中** (xcodebuild / SWBBuildService が書いている) のディレクトリ。消してもビルドが失敗するだけで、
  やり直せば再生成される。ビルドサービスのプロセスの有無で止める案は採らなかった (Xcode を開いている間は常駐して
  毎回 blocked になると見込んだが、**常駐するかは未実測**: 2026-10-22 の観測時は Xcode を起動していなかった)
- **mmap されないファイルだけを使っているプロセス** (txt に出ない)。実行ファイル・dylib・`Assets.car` は txt に出る
  (Obaket で 3 行当たった) が、read で読んで閉じた resource は見えない。そのアプリの実行ファイルが txt に出るので、
  DerivedData から起動したアプリ本体の取りこぼしにはならない
- 自分以外のユーザーのプロセス (一般ユーザーの lsof には見えない)。DerivedData は本人のものなので対象外
- **TOCTOU の窓**: `planDelete` は lsof を含む再走査をエントリにつき 1 回だけ回し、その後 Items を順に消す。
  k 番目のプロジェクトの窓は 1〜k 番目の削除時間の合計 (= エントリ全体の削除時間) で、その間に起動したアプリは守らない
- DerivedData そのものを symlink にした構成: 親に symlink を挟む候補は `validateTarget` が元から拒否するので、
  候補にならない
- lsof が `^X` の形で出す制御文字を名前に含むプロジェクト (`^` そのものはエスケープされず区別できない)。
  `canonicalPath` が `EvalSymlinks` に失敗して未解決のパスを返した場合 (`/var` → `/private/var` 以外の symlink を
  挟むと照合が外れうる)。どちらも照合が外れる側 (fail-open) だが、起きる条件が狭いので受け入れた
- 大文字小文字を無視するので、case-sensitive なボリュームでは名前が大小違いの別プロジェクトも守る側に入る (安全側)
- lsof が rc≠0 を返す環境では常に failed になり、DerivedData を消せない (安全側。実測では rc=0)

## 反証・敵対的レビューの結果 (2026-10-22)

観点を分けて 2 本 (壊す側 = opus / issue の反証と素通り = sonnet)。

- 採用 (P2): **locale の無い環境では lsof が非 ASCII を `\xNN` で出し、guard が外れる** (fail-open)。
  実測で再現: `env -u LANG -u LC_ALL -u LC_CTYPE lsof -Fn` は `\xe3\x83\x97…` を出し、`LANG=ja_JP.UTF-8` では原文。
  `\` は `\\`、タブは `\t` で出ることも実測。`unescapeLsofName` で戻してから比べる形に直した
- 採用: 削除経路でこの guard が効くことを通すテストが無かった。走査の後にアプリを起動し、`Delete` が
  そのプロジェクトだけを残して他を消すテストを足した (`TestDeleteSkipsProjectThatStartedRunningBeforeDelete`)
- 採用 (記述の訂正): TOCTOU の窓の単位 (プロジェクト 1 件 → エントリ全体)、ビルドサービスが常駐するという
  未実測の断定、mmap されないファイルの限界を「守らないもの」へ
- 記録のみ: lsof の rc≠0 で常に failed になりうる (安全側)。`EvalSymlinks` の失敗時の未解決パス (上に記載)
- 壊せなかった攻め筋: Reused / FromSnapshot の結果での削除 (`planDelete` が拒否)、firmlink 付きのパス
  (実機の lsof 出力に 0 件)、想定外の行の種類 (`p` / `f` / `n` だけ)、接頭辞が同じ別プロジェクト、大文字小文字、
  `/var` → `/private/var`、lsof の失敗・0 件・rc≠0

2 周目 (opus。`unescapeLsofName` を壊しにいく): **壊せなかった**。実機の lsof 4.91 で 10 種の名前 (空白・0x7f・0x01・
文字どおりの `bs\x41` と `caret^A`・日本語・タブ・`%`・改行・末尾の `\`) を txt に出し、戻したパスが 9/10 で実在のパスに一致した
(残る 1 つは `^A` の制御文字。上の「守らないもの」のとおり)。境界の入力 (`a\`・`a\x4`・`a\xZZ`・`a\x+1`) は元のまま返る。

- 採用 (P3): テストの偽 lsof (`lsofEscape`) が非 ASCII と `\` しか写しておらず、`\t` の分岐を消しても緑だった。
  タブ・改行・0x7f も本物と同じに出す形にし、タブの名前のケースを足した
- 記録のみ (P3): `^X` の制御文字の取り違え (既知の限界)

### 範囲外の観測 (別 issue の候補)

> **訂正 (2026-10-22)**: 下の推定は誤り。削除済みのパスを掴んでいたのは Chromium ではなく NotificationCenter だった。
> この掴みで DerivedData の判定が止まる問題とあわせて issue 858 で扱った。

- lsof の txt に、起動中の Chromium Helper が `~/Library/Caches/.glogx-delete-<id>/chromium-1193/…`
  (削除のために改名された置き場) を掴んでいる行が出ていた。`playwright-cache` (`~/Library/Caches/ms-playwright`。
  起動中の判定の guard が無い) を、Playwright の Chromium が動いている間に消したと推定している (未確認)

## 関連ファイル

- `src/doctor/disk/catalog.go` (`xcode-deriveddata` / `GuardDerivedDataLive`)
- `src/doctor/disk/guard.go` (`liveTextPaths` / `pathHeldUnder`)
- `src/doctor/disk/scan.go` (`scanEntry` の guard の switch)
- `src/doctor/disk/deriveddata_live_test.go`

## 進捗

- [x] guard の実装とテスト (`make -C src/doctor test` / `lint` 緑。新規テスト 7 本 + subtest 3)
- [x] 変異検証 (`mutate-verify` で 8 本。全部想定のテストで red、巻き添えは DerivedData のテストだけ)
  - catalog から Guard を外す → `TestCatalogDerivedDataUsesLiveGuard`
  - 絞り込みを無効化 → `TestDerivedDataDropsProjectWithRunningBuild`
  - パスの区切り `/` を外す → `TestDerivedDataMatchesWholeDirectoryName`
  - `canonicalPath` を外す → `TestDerivedDataDropsProjectWithRunningBuild` (テストの一時ディレクトリが `/var/folders` の下で、
    lsof 側は `/private/var` を出すため)。🚨 TMPDIR が symlink を含まない環境ではこの変異は緑になる
  - 大文字小文字の同一視を外す → `TestDerivedDataIgnoresCase`
  - rc の検査を外す / 0 件の検査を外す → `TestDerivedDataFailsClosedWhenLsofFails` の各 subtest
  - blocked への格上げを外す → `TestDerivedDataAllHeldIsBlocked`
  - 1 周目の指摘を直した後の追加分 (5 本、全部想定どおり red): unescape の呼び出しを外す → `TestDerivedDataUnescapesLsofNames/プロジェクト-abc`、
    `\\` を戻す分岐を外す → `.../back\x41slash-abc`、`\t` を戻す分岐を外す → `.../tab_name-abc`、
    絞り込みを無効化 / 1 件でも起動中ならエントリごと blocked にする → `TestDeleteSkipsProjectThatStartedRunningBeforeDelete`
- [x] 敵対的レビュー (下節。1 周目の指摘を直した差分に 2 周目を当てた)
