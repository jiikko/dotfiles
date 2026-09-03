# 163 audit: doctor 実装 (①②③) のおかしい箇所を探す — 次の枠で走らせる red team の手順書

- カテゴリ: audit
- 起票: 2026-09-02
- 状態: **done (2026-09-03)。未達は 205 / 206 / 207 へ切り出した**
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

read-only のサブエージェント 6 体を並行で (体 4・5・6 は実測 / 偽環境の実験を伴うので、計測コマンドと一時ディレクトリでの実験は許可する。repo のファイル編集は不可。体 6 の「1 つ足す試行」は使い捨て worktree で)。**「レビューして」ではなく「壊す手順を見つけろ。壊せなければ壊せなかったと明記しろ」**
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

### 体 5: 環境差とキャッシュを信用する境界 — 「実機 1 台でしか動かしていない」穴を狙う

読むもの: `src/doctor/disk/{guard,scan,paths}.go`、`src/doctor/svc/scan.go`、`src/glogx/doctor_cache.go`、
`doctor_view.go` の `start` / `saveCache` / `doctorReuseFrom`、issue 148 の 2 章「パス安全性」「TOCTOU」。
実験は偽 HOME / PATH を細工した子プロセスで (実環境を触らない。`adversarial-review-own-safeguards.md` 節 1)。

環境差 (fresh な Mac / CI runner で動くか):
- **Xcode 無し** (CommandLineTools だけ): `xcrun simctl` は何を返すか (エラー文 / rc)。`coresimulator-orphan` と
  `simulator-runtimes` が「診断できず」に倒れるか、それとも panic / 空白 / 候補 0 件 (false green) か。
  PATH から `xcrun` を外した偽環境で実測する
- **brew 無し**: `brewledger.Installed` の失敗が disk (brew-orphan-state / brew-cleanup-residue) と svc (C 判定) と
  brew doctor の 3 箇所で全部「診断できず」になるか。1 箇所でも「候補 0 件」に化けていないか
- `~/Applications` 無し / `/Applications` に .app が 0 件 (installedBundleIDs が error を返す → 孤児判定をしない、を確認) /
  `/opt/homebrew` 無し (Intel Mac の `/usr/local`。カタログのパスがハードコード) / TMPDIR が `/var/folders` 以外 /
  HOME に空白を含む / `LANG=C` (sort や表示幅)。それぞれで全セクションが正しく縮退するか
- **CI (`_go-project.yml` の macOS runner)** で `make -C src/doctor test` が何をスキップしているか。`TestStdPathMatchesPathsH` の
  skip、`du` の有無、`brew` の有無。runner で走らないテストが手元でだけ緑になっていないか (`verify-execution-not-just-exit-code.md`)

キャッシュを信用する境界 (④ の削除の入力になる):
- `doctor-snapshot.json` / `doctor-disk.json` は一般ユーザー権限で書き換え可能。**snapshot の `Items[].Path` に任意パスを
  差し込んだとき、④ の削除がそれを対象にする経路が設計上ありうるか**。結論として「削除は必ず再スキャン + `validateTarget` を
  通し、snapshot の Path を削除対象にしない」を ④ の不変条件として issue 148 に書く (今は削除が無いので、ここでは
  設計の穴の有無だけ確定する)
- 重いエントリの再利用 (`doctorReuseFrom`) は snapshot の Result をそのまま `Results` に載せる。Reused の Result の Path は
  **再スキャンしていない**。④ で「Reused の行は削除前に必ず再スキャン」が要る。今の `Reused` フラグがそこまで伝わる形か
- catalog の ID が変わった / 消えたとき、古い snapshot の Result が UI に残るか (`doctorReuseFrom` は今の catalog にある ID
  だけを返すか。`start` の snapshot 復元 (TTL 5 分) は catalog を見ずに丸ごと出す → 消えた ID の行が出る)
- JSON の型が壊れているとき (`Total` が文字列 / `Results` が object): `json.Unmarshal` エラーで「無し」に倒れる (確認) が、
  **部分的に壊れている** (1 Result だけ Status が知らない文字列) と何が起きるか

### 体 6: CLI と UI の一致 / 見え方 / 説明可能性 / 入口 / 変更耐性

読むもの: `src/doctor/disk/report.go` と `src/glogx/doctor_view.go` の diskSection、`src/doctor/svc/report.go` と svcSection、
`src/doctor/cmd/*/main.go` (exit code)、`src/glogx/README.md` のキー表と `options.go` の `--help`、`src/glogx/CLAUDE.md`。

- **CLI と UI の一致**: 同じ `Scan` の Report を `disk.Format` と `diskSection` に通し、合計 / 件数 / 「診断できず」の有無 /
  partial の表示が食い違う入力を探す (fixture を作って両方に流す)。exit code (0 / 1 / 2) と画面の色分けの対応
- **見え方**: `NO_COLOR` / パイプ (色なし) でリスク記号だけで判別できるか。幅 60 / 80 / 120 で何が切れるか
  (`truncateDisp` は末尾を落とすので、右端の要約・行数・リスク記号が先に消える)。全角ラベルと半角マークの列
  (`no-mixed-width-columns-in-terminal-ui.md`: 幅計算でなく目で見る。`bin/glogx` を起動して `D` を押し、
  端末幅を変えて実物を見る。`tmp/` のサンプルは gitignore なので残っていないことがある)。
  🚨 以外に表示幅が揺れる記号 (✅ ⛔ ❓ ❔ ▌ ▶ ▼) が無いか、実端末で 1 分間見て右端が動かないか
- **説明可能性**: 各候補の「なぜ出たか」が、人がそのまま打てる裏取りコマンドになっているか。ディスクは `du -sk <path>` /
  `xcrun simctl list devices` / `brew info <formula>`、svc は `launchctl list | grep <label>` / `launchctl print gui/$(id -u)/<label>` /
  `ls -l <Program>`。**Y のコピー文に裏取りコマンドを載せる**と、別セッションの LLM が自分で確かめられる (提案として起票)
- **入口ドキュメント** (`new-tool-requires-entrypoint-docs.md`): glogx README のキー表に `D` / `Enter` / `y` / `Y` / `r` が載っているか、
  `glogx --help` に doctor が出るか、`bin/diskdoctor --help` / `bin/svcdoctor --help` の文言が現状 (削除なし / exit code の意味)
  と合っているか、`src/README.md` の一覧に doctor があるか。無ければ起票 (ux)
- **変更耐性**: カタログにエントリを 1 つ足す試行 (例: `~/Library/Caches/pip`。commit しない)。触る箇所が `catalog.go` 1 箇所で
  済むか、UI / snapshot / テスト (`TestCatalogRespectsExclusions`) / README のどこに波及するか。issue 148 が「将来の
  診断項目を同じ枠で足せる器か」を ③ の検証と位置づけているので、足してみるのが最も安い検証。波及が 3 箇所を超えるなら起票

## 前回 (2 回目) で「記録に留めた」もの — 再提出しない

issue 148「敵対的レビュー 2 回目」節の「記録に留めたもの」5 件と「壊せなかった攻め口」。特に: 2 プロセス同時の lost update /
cancel 後の有界な残り I/O / restart 直前の子 / 空白入りラベルの B 未評価 / ハードリンクの 2 件目 0 表示。

## 結果 (索引)

走行 1 回目: 2026-09-02 17:05〜17:17。6 体を並行起動したが **全体が session limit (429) で途中終了**した。
体 1 / 2 / 6 は報告を書き終えており、体 4 は実測ログを、体 5 は証跡を残していたので、そこから起票した。
**体 3 (並行・中断・状態機械) だけ成果ゼロ**なので、別枠で直列に走らせる (下記「未走行」)。
以降のサブエージェントは並行させず 1 体ずつ直列に動かす (ユーザー指示 2026-09-02)。

### 起票した issue

| # | カテゴリ | タイトル | 重要度 | 出典 |
|---|---|---|---|---|
| 167 | bug | `collectBundleIDs` の走査漏れでインストール済みアプリのコンテナを孤児にする | **P1** | 体 1 + 体 5 |
| 168 | bug | `simDeviceUDIDs` が XCTestDevices セットを見ない | P2 | 体 1 |
| 169 | bug | `xctest-logarchive` の glob が未実測で黙って 0 件になりうる | P2 | 体 1 |
| 170 | test | doctor のテスト 11 箇所が変異しても green | P2 | 体 1 + 体 2 |
| 171 | bug | `brew doctor` の警告本文が空行以降で切り捨てられる | P2 | 体 2 |
| 172 | bug | 再利用した計測値がトーストに乗る | P3 | 体 2 |
| 173 | bug | failed した完全な結果がキャッシュを潰しトーストが沈黙する | P3 | 体 2 |
| 174 | bug | `LastNotifiedAt` が未来だとトーストが永久に沈黙する | P3 | 体 2 |
| 175 | bug | `expand` が glob メタ文字 / 相対パスで無音の 0 件になる | P2 | 体 5 |
| 176 | bug | `/opt/homebrew` ハードコードで Intel Mac が候補 0 件に化ける | P2 | 体 5 |
| 177 | bug | CLI の exit code が「診断できず」を伝え落とす | P2 | 体 5 + 体 6 |
| 178 | bug | snapshot を信用する境界が閉じていない (④ の前提) | **P1** | 体 5 |
| 179 | ux | UI が svc の `com.apple.` 偽装 / brew 孤児の注記を落とす | P2 | 体 6 |
| 180 | ux | 「診断できず」の行が選べず末尾も切れる | P2 | 体 6 |
| 181 | ux | 入口ドキュメントが 4 箇所欠けている | P2 | 体 6 |
| 182 | ux | 狭い幅で意味が消える表示が 4 系統 | P3 | 体 6 |
| 183 | ux | `Y` のコピー文に裏取りコマンドが無い | P3 | 体 6 |

### 却下 (理由つき。同じ指摘の再生成を止めるため)

**体 4 (リーク / パフォーマンス) は全項目が実測で「問題なし」だったので 1 件も起票していない。** 実測値は下の表に残す。

- **却下: goroutine / fd のリーク** — doctor 画面の開閉 100 回 (mid 50 / full 50 / snapshot 復元→再スキャン 98) で
  goroutine 2→2、fd 6→6、settle 0 秒。`runner.Exec` の cancel 100 回でも goroutine 2→2 / fd 6→6、
  プロセスグループの生存は return 時点 0 件 / 100ms 後 0 件 (平均 51ms / 最大 76ms で消える)。Setpgid + グループ kill は効いている
- **却下: `lines()` の毎フレーム再構築が重い** — 実測 172µs/call (実機相当の行構成) / 100 行の合成でも 267µs。
  12.5fps で CPU 0.2%、60fps でも約 1%。`-cpuprofile` の top は grapheme 幅計算 (uax29 / displaywidth) で 13% 程度。
  メモ化 (`status_view.go` の `idxCache` 相当) を入れる価値は現時点で無い。**再検討の trigger**: 行数が 200 を超える設計変更が入ったとき
- **却下: `duSize` の 4 並行が SSD で最適でない** — 実測 (実機 63GB / 596k ファイル、順序を入れ替えて 2 周):
  conc=1 で 16.0s / 16.7s、conc=2 で 9.1s / 7.8s、conc=4 で 4.6s / 5.5s、conc=8 で 4.7s / 4.4s。
  4 で頭打ちになり 8 でも悪化しないので、既定 4 は妥当
- **却下: `brewledger.Installed` を disk と svc で 2 回起こしているのが遅い** — 実測 `brew info --json=v2 --installed` は
  0.84〜0.90 秒 (issue 163 本文の「1.5 秒」は過大)。2 本を並行させても wall 0.91 秒で、brew の内部 lock で直列化されていない。
  走査全体のパターン (doctor ∥ info ∥ cleanup --dry-run) は wall 2.71 秒で、律速は `brew cleanup --dry-run` (2.5〜2.75s) と
  `brew doctor` (2.23〜2.27s)。**1 回に寄せても体感は変わらない**
- **却下: 起動パスの `loadDoctorDiskCache` が起動時間を悪化させる** — 実測 26.5µs / 5.6KB / 65 allocs。
  `bin/glogx` の起動〜初回描画の before/after は**未計測**だが、この値では観測不能
- **却下: snapshot を `start()` で 2 回読むのが遅い** — 実測 `loadDoctorSnapshotAny` 168µs、2 回読む経路の合計 331µs。
  `saveCache` 187µs / `saveDoctorSnapshot` 204µs も同オーダー。Update の中で同期にやって問題ない
- **却下: `containerOwnedByInstalled` の接頭辞判定で偶然一致の偽陰性が出る** — 実機で孤児判定された 8 件のうち 7 件
  (Acrobat.Pro / LINE.TimelinePreviewService.0,1 / 1password.browser-support / google.one / pdfeditor / Telegram.TelegramShare) は
  本体不在か旧版の残骸で**正しい孤児**。逆向き (コンテナ id が app id の接頭辞) や偶然一致で現役を見逃す形は作れなかった
- **却下: `brewSharedVarDirs` の除外漏れ / `Parse` の `@` 落としで現役を孤児にする** — 実機の `/opt/homebrew/var` は
  cache / db / homebrew / log / mysql / postgresql@14 / run で、孤児判定は `postgresql@14` のみ (`brew list` に無いので正しい)。
  `brew info --json=v2` に oldnames / aliases が実在することも確認 (216 formulae)。
  `var/mongodb` と `mongodb-community@X` 型の不一致は**該当 formula が無く未確認**
- **却下: `canonicalPath` / `EvalSymlinks` で削除対象と比較対象がずれる** — `_zshrc` の規則 (anyenv 優先 → `~/.tool`、
  `<UPPER>_ROOT` を起動時 export) と一致。実機 `GOENV_ROOT=~/.anyenv/envs/goenv` で `~/.goenv` (244MB) が孤児 = 設計どおり。
  root 自体が symlink でも両側 `EvalSymlinks` で揃う
- **却下: svc の `keepAliveRestartsOnFailure` / `PathState` / 非数値 PID** — dict `{SuccessfulExit:false, Crashed:true}` は
  true を返す (正しい)。`PathState` は偽陰性側。実機 `launchctl list` に非数値 PID / 空白ラベル行は 0 件。
  svcdoctor の実行結果は 12 件走査 / Findings 0 / Undiagnosed 0 / StatusErr・BrewErr 空
- **却下: Xcode 無し (CLT のみ) / brew 無しで false green になる** — 偽環境で実測し、**4 ID すべて failed (診断できず) に倒れた**
  (simulator-runtimes / coresimulator-orphan / brew-orphan-state / brew-cleanup-residue)。rc=2、stderr 0 byte。
  `LANG=C LC_ALL=C` でも同じ。AppDirs が空 / 不在なら「bundle id を 1 つも取れなかった」で fail-closed。
  Previews セット不在は `os.Stat` で skip。**ただし svcdoctor の exit code だけは 0 に化けた** → issue 177 で起票
- **却下: HOME に空白 / 末尾スラッシュ / `*` で壊れる** — いずれも正常 (items=1)。壊れたのは `[` を含む形と相対 TMPDIR
  → issue 175 で起票
- **却下: TMPDIR が `/var/folders` 以外だと壊れる** — 実 `/var/folders` / scratchpad 配下はいずれも正常。
  symlink 経由の `~/tmp` は「経路の途中に symlink がある」で fail-closed (正しい)
- **却下: CI で doctor のテストが skip されて手元でだけ緑** — `t.Skip` は 3 箇所のみ。
  `TestStdPathMatchesPathsH` は paths.h が CLT SDK にも在るので CLT だけの環境でも PASS (実測)、
  `disk/scan_test.go` の 1 つは root のときだけ skip (runner は非 root)、もう 1 つは実 `du` を使う (macOS 標準)。
  glogx の doctor テストは `XDG_CACHE_HOME` を `t.TempDir()` に向けており実キャッシュを触らない。
  **ただし `make test` は `-v` 無しなので skip が出力に出ない** (今回は実害なし。記録に留める)
- **却下: 壊れた JSON で誤動作する** — `total` が文字列 / `results` が object / `elapsed` が文字列 / 末尾ゴミは
  いずれも load 失敗 → 走査に倒れる (正しい)。部分的に壊れている形 (未知 status / 負の size / 未来の measured_at) は
  実害があるので issue 178 で起票
- **却下: 変更耐性が低い (カタログに 1 つ足すと波及が広い)** — 実験 (`~/Library/Caches/pip` を追加) では
  `catalog.go` の 1 箇所だけで両 module の `go build` / `go test` が全 green。波及は issue 148 の表 (写し) を含めて 2 箇所

### 壊せなかった攻め口

- 体 1: `containerOwnedByInstalled` の偶然一致 / `brewSharedVarDirs` と `@` 落とし / `canonicalPath` の symlink /
  svc の KeepAlive dict・PathState・非数値 PID / xcrun 不在の fail-closed / Previews セット不在 / Containers の隠しファイル
- 体 2: 再利用の TTL 連鎖 (`Scan` は `*prev` を丸ごとコピーし `MeasuredAt` / `Elapsed` を更新しないので、元計測から 1 時間で必ず切れる) /
  `reusable` の ID 一致 (disk 側の `TestReuseSkipsScan` は変異で red = 効いている) / 軽いエントリの非再利用 /
  時計戻しの `loadDoctorSnapshot` と `doctorReuseFrom` (両方に `age<0` がある) / `doctorNothing` の実経路 /
  brew の同じ見出し 2 回 (key に index を含むので衝突しない)
- 体 4: 上の「却下」の実測値がそのまま「壊せなかった」の記録
- 体 6: 合計 (`SumDeletable` 単一) / 件数 / partial の有無 / 「OK かつ 0 件かつ Failures 無しなら隠す」条件は CLI・UI で一致。
  マーク列の開始位置は幅 60 / 80 / 120 で固定 (col 54)。全角ラベル列は左寄せ・半角サイズ列は右寄せに分かれており**同列混在は無い**。
  表示記号 ✅ ⛔ ❓ ❔ 🚨 はいずれも Emoji_Presentation (2 桁固定) で、🚨 の doctor 表示経路への残りは 0 件 (grep はコメント内のみ)。
  `▌ ▶ ─` は East Asian Ambiguous だが glogx 全体と同じ扱いで新規リスクではない。snapshot ヘッダ「N 分前の結果」は幅 60 でも残る

### この audit の決着 (2026-09-03)

**6 体のうち 5 体を消化し、指摘 17 件を起票した。未達の 3 点は独立した issue へ切り出したので、
索引としてのこの issue は役目を終えた。**

| 未達だったもの | 切り出し先 |
|---|---|
| 体 3 (並行・中断・状態機械) が未走行 | [205](205-audit-doctor-red-team-concurrency-state-machine.md) — 攻め口 6 つを移設。🚨 180/182 で選べる行と幅の扱いが変わったので、当時の攻め口はそのままでは成立しない旨も書いた |
| 体 4 の実機観察 (fd / プロセスグループ) と起動時間の before/after が未計測 | [206](206-perf-doctor-unmeasured-leak-and-startup-items.md) — 済んだ計測の実測値も表で持ち越した |
| 反証レビューを P1 2 件にしか通していない | [207](207-test-doctor-unrefuted-issues-from-163.md) — 着手済み 15 件は実装時に実コードで裏を取っているので、残るのは未着手の 183 / 169 だけ |

起票した 17 件の消化状況 (2026-09-03):

| 状態 | 件数 | 番号 |
|---|---|---|
| done | 15 | 167 / 168 / 170 / 171 / 172 / 173 / 174 / 175 / 176 / 177 / 178 / 179 / 180 / 181 / 182 |
| pending | 1 | 169 (glob の実名が未実測。`xcodebuild test` を回した後が trigger) |
| open | 1 | 183 (`Y` のコピー文に裏取りコマンドを載せる) |

<details><summary>当時の記録 (2026-09-02 時点の進捗表)</summary>

**今回やったのは 6 体のうち 5 体分で、しかもその 5 体も途中終了した残骸から拾ったもの。**

| 体 | 観点 | 状態 |
|---|---|---|
| 1 | 判定を壊す (disk / svc / brewledger) | 報告完了 → 起票済み |
| 2 | 素通り (fail-closed / キャッシュ / テスト) | 報告完了 → 起票済み |
| 3 | **並行・中断・状態機械 (doctor 画面)** | **未走行 (成果ゼロ)** |
| 4 | リソースリークとパフォーマンス | 実測ログのみ回収 → 全項目「問題なし」で却下 |
| 5 | 環境差とキャッシュを信用する境界 | 証跡のみ回収 → 起票済み |
| 6 | CLI・UI 一致 / 見え方 / 入口 / 変更耐性 | 報告完了 → 起票済み |

**未走行: 体 3 (並行・中断・状態機械)**。1 回目は session limit で成果ゼロ、2 回目は起動直後にトークン消費を抑えるため
中断した。攻め口は本 issue の「### 体 3」節がそのまま使える (rows とカーソル index のずれ / `expanded` キーのずれ /
nil channel の永久ブロック / Setpgid と SIGINT・`syscall.Exec` 再起動 / `TestExecKillsGrandchildOnCancel` の時間依存 /
`catalogN+1` の容量計算と `Reuse`)。

**回収した 5 体も「完全に走り切った」わけではない**。体 4 は計測項目のうち実機の `lsof` / `pgrep -g` による観察と
`bin/glogx` の起動時間 before/after が未計測 (却下節に明記)。体 5 は報告本文を書く前に落ちたので、証跡の表から
main agent が起票した (証跡に無い攻め口は当たっていない可能性がある)。

### 当時の「今後の予定」(すべて 205 / 206 / 207 へ移した)

- 次の枠で体 3 を直列で走らせる → **205**
- 体 4 の未計測項目を埋める → **206**
- 起票済み issue の反証レビュー → **207**

</details>

### 反証レビューの実施状況

- **167 / 178 (P1 2 件)**: 通した。結果は「事実誤認・重複なし」。ただし 167 の対象シンボルの所在に誤記があり訂正した
  (`collectBundleIDs` / `containerOwnedByInstalled` は `scan.go` ではなく `guard.go`)。
  実機 probe 由来の主張 (UUID コンテナの具体パス / 負サイズの表示) はコードからの静的検証止まりで、実行による裏取りはしていない
- **168-177 / 179-183**: **未反証**。着手時に個別に通す

## 受け入れ条件

- [x] 6 体の報告が、トピックごとの issue と、この issue の索引に分かれて残っている (体 4 は数字つき)
      — **5 体分。体 3 は 205 へ**
- [x] 起票した issue はそれぞれ反証レビューを通している (P1 は必須) — **条件は 207 へ委譲した**。
      P1 2 件 (167 / 178) は反証レビュー済み。他は着手時に実コードで再現を確かめる形にしたが、
      🚨 **167 / 168 は「被害が実機で成立するか」が未確認のまま**なので、183 / 169 と同じ
      未回収枠として 207 が持つ (2026-09-03 の敵対的レビューで検出。当初は「全件確認済み」と
      書いていた)
- [x] 却下した指摘は理由つきでこの issue に残っている (次の audit が同じ指摘を出さない)
- [x] 修正はこの issue の仕事ではない。起票した issue を個別に着手する
      — **17 件中 15 件が done。残りは 169 (pending) と 183 (open)**
