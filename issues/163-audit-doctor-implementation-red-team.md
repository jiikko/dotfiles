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

read-only のサブエージェント 3 体を並行で。**「レビューして」ではなく「壊す手順を見つけろ。壊せなければ壊せなかったと明記しろ」**
で投げる (`_claude/rules/issue-creation-codex-review.md` の反証の作法)。走行中は対象ファイルを編集しない
(`parallel-write-agents-need-worktree-isolation.md`)。5h 枠の残量が少ないと途中で落ちるので (2026-09-02 に 3 体とも
session limit で死んだ実例)、**枠が開いた直後に起動する**。報告は各 2000 字以内を指定する。

出力は `./tmp` に置かず、結論・採用・却下・壊せなかった攻め口をこの issue の「結果」節へ直接書く
(`move-report-conclusions-to-issues.md`)。

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

## 前回 (2 回目) で「記録に留めた」もの — 再提出しない

issue 148「敵対的レビュー 2 回目」節の「記録に留めたもの」5 件と「壊せなかった攻め口」。特に: 2 プロセス同時の lost update /
cancel 後の有界な残り I/O / restart 直前の子 / 空白入りラベルの B 未評価 / ハードリンクの 2 件目 0 表示。

## 結果 (走らせた後にここへ書く)

- 採用して直した:
- 記録に留めた:
- 壊せなかった攻め口:
- 各修正の変異検証:

## 受け入れ条件

- [ ] 3 体の報告がこの issue の「結果」節に転記されている (`./tmp` に残さない)
- [ ] P1 は全部、再現手順を実コードで裏取りしてから直すか却下する (裏取りせず直さない)
- [ ] 直した分には変異検証 (red) を付ける
- [ ] 直した差分に対して、もう 1 周 (節 7) を回すか、回さない理由を書く
