# 163 audit: doctor 実装 (①②③) のおかしい箇所を探す — 次の枠で走らせる red team の手順書

- カテゴリ: audit
- 起票: 2026-09-02
- 状態: open (走らせるのは次の 5h 枠が開いたとき)
- 対象: `src/doctor/` (svc / disk / runner / brewledger / cachedir / cmd) と `src/glogx/doctor_*.go` + `tui.go` の doctor 配線
- 設計の正本: `issues/148-feat-glogx-doctor-disk-diagnosis.md` (1 章 カタログ / 2 章 安全性ハーネス / 3 章 doctor 画面 /
  4 章 サービス診断 / 7 章 受け入れ条件 / 「敵対的レビュー」「セルフレビュー」「追記」の各節)

## なぜ今これをやるか

2026-09-02 に ①②③ を一気に実装し、敵対的レビューを 2 回 (共有 module 化 / 判定ロジック + 画面) 通した。
2 回目は P1 が 4 件出て全部直したが、**その修正差分 (再帰 bundle 走査 / brewledger / Previews セット / KeepAlive dict /
Setpgid / partial 保存の規律 / 案 A レイアウト + カーソル + Enter 展開 / y・Y コピー / 重いエントリの再利用 / 5 分 snapshot)
にはまだ誰も攻めていない**。`adversarial-review-own-safeguards.md` 節 7 (指摘を直した差分にもう 1 周) がそのまま当たる。
④ (削除) の入力になる判定と、削除の土台になる画面の状態機械が対象。

## 走らせ方

read-only のサブエージェント 4 体を並行で (体 4 は実測を伴うので、計測コマンドの実行は許可する。ファイルの編集は不可)。**「レビューして」ではなく「壊す手順を見つけろ。壊せなければ壊せなかったと明記しろ」**
で投げる (`_claude/rules/issue-creation-codex-review.md` の反証の作法)。走行中は対象ファイルを編集しない
(`parallel-write-agents-need-worktree-isolation.md`)。5h 枠の残量が少ないと途中で落ちるので (2026-09-02 に 3 体とも
session limit で死んだ実例)、**枠が開いた直後に起動する**。報告は各 2000 字以内を指定する。

### アウトプットの置き方 (ユーザー指定 2026-09-02)

**指摘はトピックごとに `issues/` へ 1 ファイルずつ起票する** (この issue の中に書き溜めない)。1 トピック = 1 つの
壊れ方 / 1 つの計測結果で、他と独立に着手・却下できる粒度。

- ファイル名: `NNN-<カテゴリ>-doctor-<スラッグ>.md`。カテゴリは `bug` (誤った候補 / 壊れる) / `perf` (実測つき) /
  `leak` (残留) / `test` (vacuous なテスト) / `ux`。番号は起票時点の最大 + 1 を連番で
- 本文の必須項目: 対象 (関数名 / シンボル名で。行番号で pin しない)、**再現手順** (または実測値と条件)、重要度 (P1/P2/P3)、
  対応案、`関連: issues/163` と `issues/148` へのリンク。perf は before / 並行数別の数字を表に
- **却下したもの**は起票せず、この issue の「結果」節に 1 行ずつ「却下: 理由」で残す (同じ指摘の再生成を止めるため。
  `move-report-conclusions-to-issues.md`)。「壊せなかった攻め口」も同節に列挙
- 起票した issue は `issue-creation-codex-review.md` の反証 (観点を分けた read-only サブエージェント) を通してから commit。
  P1 は必ず、P2/P3 は主要なものだけ
- サブエージェントの報告本文は `./tmp` に落としてよいが、起票と「結果」節への転記が終わるまで作業を終えない

この issue 自体は、起票した issue の一覧 (番号とタイトル) と却下・壊せなかった攻め口だけを持つ索引になる。

### 体 1: 判定を壊す (disk / svc / brewledger) — 修正差分を狙う

読むもの: `src/doctor/disk/{catalog,paths,size,guard,scan,report}.go`、`src/doctor/svc/{plist,launchctl,brew,scan}.go`、
`src/doctor/brewledger/brewledger.go`、issue 148 の 1・2・4 章と「敵対的レビュー 2 回目」節 (再提出しない指摘の一覧)。

攻め口 (前回の修正が新たに作った面):
- `collectBundleIDs` の再帰: 深さ上限 8 で届かない置き場 (`/Applications/Setapp/*.app`、`~/Applications/JetBrains Toolbox/apps/…`、
  `/System/Applications` は AppDirs に無い)。symlink の `.app` (Homebrew cask は `/Applications/X.app` が Caskroom への symlink) を
  `os.ReadDir` の `e.IsDir()` が dir と見ないので**読まれない**のではないか (cask アプリのコンテナが全部孤児になる)
- `containerOwnedByInstalled` の接頭辞判定 `strings.HasPrefix(id, app+".")`: 逆向き (コンテナ id が app id の接頭辞) や、
  `com.apple.` 除外との相互作用。偶然の接頭辞一致 (`com.foo` と `com.foo.bar` が別ベンダ) で孤児を見逃す偽陰性は許容か
- `brewSharedVarDirs` の allowlist 化されていない除外: 実機の `/opt/homebrew/var` に無い共有 dir 名 (`nginx` / `postgres` /
  `mongodb` の data dir は formula 名と一致するか)。`brewledger.Parse` の `@` 落とし (`python@3.12` → `python`) が
  `var/python` のような無関係な dir を現役扱いにしないか
- `simDeviceUDIDs` の Previews セット: `--set` のパスに空白を含む (`Simulator Devices`) が、fake runner の prefix 一致で
  テストが本物の argv を検証しているか。`xcrun` が Xcode 未導入 (CommandLineTools のみ) で何を返すか → fail-closed に倒れるか
- `canonicalPath` の `EvalSymlinks`: `~/.rbenv` 自身が symlink のとき、比較は解決後、`validateTarget` は解決前 → 削除対象と
  比較対象がずれる形はないか (④ の削除で効く)
- svc: `keepAliveRestartsOnFailure` で dict の `SuccessfulExit=false` + `Crashed` 併記 / `PathState` の扱い。`st.PID == 0` の
  判定で `launchctl list` の PID 列が `-` 以外の非数値のとき
- `xctest-logarchive` の新 glob `XCTest*` / `xctest*`: 実機の実ファイル名 (2026-09-01 時点で `/var/tmp/*.logarchive` が
  1.8GB あった) と一致するか。**現在は 0 件 (掃除済み) で未実測**。名前が違えば黙って 0 件になる (false negative)

### 体 2: 素通り (fail-closed / キャッシュ / テスト) — 3 つのキャッシュの相互作用を狙う

読むもの: `src/glogx/doctor_cache.go`、`doctor_view.go` の `start` / `saveCache` / `maybeSaveSnapshot` / `receiveDisk`、
`doctor_view_test.go`、issue 148 の 3 章「トースト」「スキャン中の振る舞い」と「追記」節。

攻め口:
- **3 つの保存が並ぶ**: `doctor-disk.json` (トースト要約) / `doctor-snapshot.json` (5 分再利用 + 重いエントリ再利用の元) /
  partial 保存の規律。同じ走査で 3 つが食い違う形 (snapshot は完了時だけ、disk.json は partial も条件付きで書く)。
  トーストの数字と開いた画面の数字が違う経路
- 重いエントリの再利用 (`doctorReuseFrom`): 再利用された Result を含む完了で snapshot を書くと、その Result の `MeasuredAt` は
  古いまま → 1 時間の TTL は元の計測から数える (意図どおり) が、`Elapsed >= 2s` の判定も元のまま → 一度重かったものは
  実体が消えて軽くなっても 1 時間は再測されない。実体が消えたのに「20.9GB」を出し続ける (最大 1 時間) を許容と書いたが、
  **トースト側にも乗る** (disk.json は再利用値で更新される)。閾値超えの偽陽性トーストになる経路を具体化する
- `saveCache` の partial ガード `prev.Total >= rep.Total`: partial の合計が偶然大きい (重いエントリが先に終わった) とき、
  完全な結果を partial で潰す。トーストは `Partial` を見ないので、次の完全走査まで partial の数字が出る
- `loadDoctorSnapshotAny` は期限を見ない: 数日前の snapshot の重いエントリでも `MeasuredAt` が 1 時間以内なら再利用、は
  成立しない (MeasuredAt は snapshot 以前) → 実質 1 時間。ただし**時計を戻した**とき `age < 0` で弾く判定が
  `doctorReuseFrom` と `loadDoctorSnapshot` の両方にあるか
- テストの vacuous 度: `TestDoctorReusesHeavyEntries` は fake カタログ 1 件。「軽いエントリは再利用しない」を Scan 経由で
  固定していない (純関数 `doctorReuseFrom` だけ)。`TestDoctorCopyPathAndText` は `rows` を直接いじって `doctorNothing`
  を出している (実経路で到達するか)。各テストに「変異しても green」がないか 1 つずつ
- `brew doctor` の連結 `stdout + "\n" + stderr`: Warning ブロックが stdout と stderr に分かれて同じ見出しが 2 回出る形

### 体 3: 並行・中断・状態機械 (doctor 画面) — カーソル / 展開 / 再利用の状態を狙う

読むもの: `src/glogx/doctor_view.go` 全文、`tui.go` の doctor 配線 (`doctorOv` で grep)、`src/doctor/runner/runner.go`。

攻め口:
- `rows` は `lines()` (描画) が作り直し、`handleKey` はその `rows` を見る。**描画とキーの間で結果が届く** (disk の Msg で
  `diskResults` が伸びる) と、`cursor` の index が別の行を指す。Enter / y / Y が意図と違う行に効く再現手順を作る
  (bubbletea は Update → View の順だが、Msg 2 つが連続すると View を挟まない)
- `expanded` のキー: brew は `fmt.Sprintf("brew:%d:%s", i, summary)`、disk は `"disk:"+ID`。再スキャンで順序が変わると
  展開状態がずれる / 別の行が開く。`r` で `expanded` を作り直しているか (start で `map{}` にしている → 展開は全部畳まれる。
  それは意図か)
- `start(force=false)` で snapshot 復元したとき `diskCh` は nil。その後 `receiveDisk` に旧世代の Msg が来ると gen で弾く
  (確認)。`waitDiskCmd` が nil channel を読むと**永久にブロック** → gen 一致で呼ばれる経路が無いことを証明する
- `Setpgid` の副作用: 子を別プロセスグループにしたので、glogx が受ける SIGINT (Ctrl-C) は子に届かない。glogx 側の
  `cancelAll` が必ず走るか (Ctrl-C 2 回の緊急脱出 `quitNow` 経由を含む)。`main.go` の `syscall.Exec` 再起動 (r) の直前に
  cancel した子が生きたまま新イメージの子になる形 (前回「記録」にした。今回は Setpgid で `Kill(-pgid)` が非同期に
  走るタイミングを見る)
- `TestExecKillsGrandchildOnCancel` は `sleep` を使う時間依存 (300ms で cancel、2 秒待つ)。CI の負荷で flaky にならないか
  (`avoid-wall-clock-assertions.md`)。回数 / 状態で判定する形に書き直せるか
- `-progress` (CLI) と `OnResult` の並行呼び出しは記録済み。glogx 側は channel で直列化 → `catalogN+1` の容量計算に
  `Reuse` が影響しないか (再利用でも OnResult は 1 回呼ばれるか。`scan.go` の goroutine は再利用時も `OnResult` を呼ぶ経路か)

### 体 4: リソースリークとパフォーマンス — 「閉じた後に何が残るか」「毎フレーム何をしているか」を狙う

読むもの: `src/glogx/doctor_view.go` / `doctor_cache.go` / `doctor_brew.go`、`tui.go` の doctor 配線 (`spinnerActive` /
`tickInterval` / `viewLines` / `Init`)、`src/doctor/disk/{scan,size,guard}.go`、`src/doctor/runner/runner.go`、
`src/doctor/svc/scan.go`。実測は `go test -bench` / `-race` / `-cpuprofile` と、実機で `bin/glogx` を開いた状態の
`ps -o rss,vsz` / `lsof -p` / `fs_usage` で取る (数字を出す。`perf-claims-need-measurement.md`)。

リーク (閉じた後 / quit 後に残るもの):
- **goroutine**: 開閉を 100 回繰り返した後の `runtime.NumGoroutine()` が増え続けないか。`waitDiskCmd` の 1 件待ち goroutine
  (bubbletea が Cmd を goroutine で走らせる) は旧 channel に必ずイベントが来て終わる契約だが、**snapshot 復元 (`diskCh` nil)
  の後に gen が一致する `receiveDisk` 経路は無いか** (nil channel の受信は永久ブロック)。テストで
  `runtime.NumGoroutine()` を open/close の前後で比べる (回数で判定。時間で判定しない)
- **fd**: `runner.Exec` の pipe (stdout / stderr の bytes.Buffer 用)、`os.ReadDir` / `os.ReadFile` (Info.plist を再帰で数百本読む)、
  `filepath.WalkDir` が開く dir fd。cancel された Cmd の pipe は `WaitDelay` 後に閉じられるか。`lsof -p <glogx pid> | wc -l` を
  開閉 20 回の前後で比べる
- **子プロセス / プロセスグループ**: `Setpgid` 後の孫が cancel で本当に消えるかを実機で (`pgrep -g <pgid>`)。`launchctl print`
  を候補ごとに起こす経路で候補が多いとき (数十件) の同時本数。brew doctor (60 秒 timeout) を開いて即閉じ、
  60 秒待たずに `ps` から消えるか
- **メモリ**: `disk.Result.Items` に全 item を持つ (DerivedData は数十 dir)。`Contents` (Inspect の中身一覧) は上限なし →
  `~/Library/Containers` が 600 件超のとき孤児候補ごとに `ReadDir` の名前を全部持つ。snapshot JSON が肥大しないか
  (実機で `doctor-snapshot.json` のサイズを見る)。`doctorDiskCache.Entries` は候補 0 件を除いているか
- **キャッシュファイル**: `doctor-disk.json` / `doctor-snapshot.json` の `.tmp.<pid>` が異常終了で残らないか (rename 前に kill)。
  `doctor-history/` は ④ で増える一方なので上限を決める必要があるか (今は無い)

パフォーマンス (体感に効くもの):
- **毎フレームの再構築**: `lines()` が呼ばれるごとに `buildRows` → 3 セクションのソート (`sort.SliceStable`) + 文字列生成 +
  `truncateDisp` (幅計算) を全行やり直す。走査中は 12.5fps、演出中は 30〜60fps でこれが回る。行数 (実機で 60〜100 行) ×
  幅計算のコストを `-cpuprofile` で見る。`rows` を Msg 到着時だけ作り直す (描画では窓を切るだけ) 形にできるか。
  比較対象: `status_view.go` の `idxCache` (行構成のメモ化と、その無効化の規律)
- **起動パス**: `Init` の `loadDoctorDiskCache` (ファイル 1 本読み) は許容としたが、**受け入れ条件「起動時間が悪化しない」は
  未実測**。`bin/glogx` の起動〜初回描画を before/after で 10 回ずつ測る (Bench の既存経路 `bench.yml` があれば乗せる)
- **walk のコスト**: `duSize` は全ファイルを `Lstat` する (`du -sk` と同じ)。DerivedData 17.6GB で 5.7 秒。ctx 確認が 256 件ごと
  なので中断の遅れは最大 256 stat。`seen` map の dedupe は entry ごとに作り直す (エントリ間の重複は数えない、は既知)。
  4 並行のセマフォは SSD で最適か (I/O が競合して 1 並行より遅くなっていないか。並行 1 / 2 / 4 / 8 で合計時間を実測)
- **重いエントリの再利用**: 再利用しても `sizePaths` → `validateTarget` の前に `expand` (glob) は走る? (`reusable` は
  `scanEntry` の前に返すので走らない、を確認)。svc 側は毎回全 plist を読み直す (数十ファイル、軽い) が、`launchctl print`
  を候補ごとに毎回起こす
- **brew**: `brew info --json=v2 --installed` (1.5 秒) を disk と svc で **2 回**起こしている (`brewledger.Installed` を
  それぞれが呼ぶ。同じ走査で 1 回に寄せられる)。`brew cleanup --dry-run` (2.8 秒) + `brew doctor` (4.5 秒) と合わせて
  brew だけで 10 秒近い。並行はしているが、brew は内部で lock を取るので直列化されていないか実測する
- **snapshot の読み書き**: `start()` が `loadDoctorSnapshot` と `loadDoctorSnapshotAny` で同じファイルを 2 回読む。
  `receiveDisk` / `receiveSvc` / `receiveBrew` の各着弾で `maybeSaveSnapshot` が呼ばれ、3 つ揃った時点で 1 回書く (確認)。
  `saveCache` が `loadDoctorDiskCache` → 書き、を Update の中で同期にやる (小さいファイルなので許容か、数字で)

判定の書き方: リークは「回数 / fd 数 / プロセス数が開閉前後で不変か」で、パフォーマンスは「実測値 (before/after or 並行数別)」で。
「遅そう」「漏れそう」だけの指摘は却下側に分類する (`perf-claims-need-measurement.md`)。

## 前回 (2 回目) で「記録に留めた」もの — 再提出しない

issue 148「敵対的レビュー 2 回目」節の「記録に留めたもの」5 件と「壊せなかった攻め口」。特に: 2 プロセス同時の lost update /
cancel 後の有界な残り I/O / restart 直前の子 / 空白入りラベルの B 未評価 / ハードリンクの 2 件目 0 表示。

## 結果 (走らせた後にここへ書く。索引)

- 起票した issue (番号 / カテゴリ / タイトル / 重要度):
- 却下 (理由つき):
- 壊せなかった攻め口:

## 受け入れ条件

- [ ] 4 体の報告が、トピックごとの issue (`issues/NNN-*-doctor-*.md`) と、この issue の索引に分かれて残っている (体 4 は数字つき)
- [ ] 起票した issue はそれぞれ反証レビューを通している (P1 は必須)
- [ ] 却下した指摘は理由つきでこの issue に残っている (次の audit が同じ指摘を出さない)
- [ ] 修正はこの issue の仕事ではない。起票した issue を個別に着手する (着手時に変異検証 / もう 1 周の規律が付く)
