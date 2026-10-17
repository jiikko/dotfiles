# 742 (refactor): glogx を画面ごとのパッケージに割る

> 🚨 **担当中: M4Pro-MacBook-Pro (glogx)**（2026-10-17〜）

起票日: 2026-10-12

> **2026-10-17 にユーザー判断で open に戻した (着手はユーザーの手が空いたとき)**。理由: 開発中にビルドキャッシュが増えやすいのがペイン。
> glogx は 1 パッケージなので、1 行直すたびにパッケージ全体 (テストの -race 版で 1 つ 26 MB 級) を作り直し、新しい項目として GOCACHE に溜まる
> (下の「ビルドキャッシュの増え方」)。分割すると、直したパッケージとそれを import する側だけを作り直す。
>
> 2026-10-12 の保留の理由 (記録として残す): 画面どうしは今も直接参照せず `browseModel` が仲介する規律が守られているので、分割で新しく
> 得るのは「その規律の機械での強制」まで。いちばん複雑な `tui.go` は main に残るので分割では軽くならない。テストを main に残すとテストから
> 触る名前が公開名になり境界が弱まる。対して費用 (約 6 万行・テスト関数 1,100 本の移動、CI / Makefile / lint のパスの張り替え、
> `clock_rollback_test.go` のように移設で黙って外れる検査) が大きい。当時の着手条件は「doctor 画面へまとまった機能を足すとき、doctor だけを
> 先に切り出す」で、doctor を最初に切り出す順はそのまま使える。ビルドキャッシュの観点はこの時は検討に入っていなかった

## 概要

`src/glogx` は `package main` の 1 パッケージで、非テストの 62 ファイル・テストの 94 ファイル (計 60,963 行、テスト関数 1,111 本。2026-10-17 に数え直した) が
平場に並んでいる (main に残っている部分の数字。描画の部品・doctor の判定・ratelimit・treefiler などは既に別 module
`src/tuikit` / `src/doctor` / `src/ratelimit` / `src/treefiler` … に出ている)。ユーザー依頼 (2026-10-12「glogx も同じように平場に並んでいるのね。パッケージごとに分割して欲しいところ」)。
zundamon-kaisetsu を同じ形で割った (issue 741) のに続けて、画面ごとに `internal/` のパッケージへ割る。

目的は行数の分散ではない。`~/.claude/rules/verify-design-intent-before-refactor.md` の 3 問への見込み:

- 参照範囲が局所に閉じるか: 画面の型 (`issuesView` など) は `browseModel` を参照しない (下の実測)。画面を別パッケージにすれば、
  1 つの画面を直すときに読む範囲がそのパッケージと土台の層に閉じる
- 責務を分離できるか: 画面どうしは互いを直接参照せず、`wantCross` / `wantQuit` の信号を `browseModel` が仲介する形なので、変更の波及の
  経路が import の向きとして固定される
- 探すコストが下がるか: 平場の 156 ファイル (2026-10-17) から、画面ごとのディレクトリになる。ただしこれ単独では分割の理由にしない (CLAUDE.md の
  「行数や責務の見た目で割らない」)

## 既存の判断との関係

- `src/glogx/CLAUDE.md`「構造の判断」と README「glog との共通コード分離について」は、flat な `package main` を**意図的**としている。
  サブパッケージを切る基準は「実在する第二消費者」か「**明示的な分離要望**」(issues/ の前例)。今回はユーザーの明示的な分離要望なので、
  この基準の内側にある (基準を覆すのではない)。着手したら CLAUDE.md と README の「flat を維持する」の記述を新しい構成に合わせて直す
- 「行数や責務の見た目で割らない」(CLAUDE.md) は守る。割る単位は、下の実測で依存の向きが一方向にできる所に限る

## 現状 (2026-10-12 に実測。数字は 2026-10-17 に数え直した)

- 大きいファイル: `tui.go` 4,272 行 / `issues_view.go` 2,879 / `doctor_view.go` 1,700 / `doctor_delete.go` 1,550 / `status_view.go` 1,379 / `github.go` 927
- 状態機械 `browseModel` にメソッドを持つ非テストのファイルは 9 個 (`tui.go` に 124、`gitlog_watch.go` に 12、`fullscreen.go` に 3、`filer_view.go` / `filer_resume.go` / `lan_view.go` / `lan_resume.go` / `hint_surfaces.go` に 2、`open_workspace.go` に 1。2026-10-17)。
  issues viewer (`issuesView`)・doctor 画面・status viewer は **`browseModel` とは別の型**で、切り出しの候補になる
- go/ast でファイル間のトップレベルの宣言の参照を数えた (使い捨ての道具。issue 741 で使ったもの)。各画面が本体から使っているのは:
  - 描画の土台: `render.go` (`paint` / `ansi*` / `clipToWidth`)・`width.go` (`dispWidth` / `padSpaces` …)・`box.go` (`cursorGutter*` / `overlayCenteredBox` …)・`terminal.go` (`sanitize*Line`)
  - `tui.go` の小さな道具: `notice*` (結果の通知の種類)・`timeNow`・`clampIdx`・`normalizeSpaceKey` など
  - 全画面の ID: `fullscreen.go` の `fullScreenID` / `crossTarget` (画面から別の画面へ移る)
  - 最下行の案内: `status_view.go` の `fitHintItems` / `hintItem` (issues・doctor も使う。status の画面に置かれている)
  - 外部コマンド: `external_commands.go` (`copyToClipboard` / `runGit*` …)・`scroll_glide.go`・`line_cache.go`
- 依存の多い側: `tui.go` は 36 ファイルの宣言を、`issues_view.go` は 18、`doctor_view.go` は 16、`status_view.go` は 13 を参照する
- 画面どうしは直接参照しない (`wantCross` / `wantQuit` の信号を `browseModel` が仲介。`tui.go` は 3 画面の型を 14 回参照する)
- 例外: `filer_view.go` (2)・`filer_resume.go` (2)・`lan_view.go` (2)・`lan_resume.go` (2)・`hint_surfaces.go` (2)・`open_workspace.go` (1)・`fullscreen.go` (3) は
  `browseModel` のメソッドを持つので、filer と LAN (mac-lan-manager。2026-10-12 の後に足された) の全画面は型を分けてからでないと切り出せない (usage / ratelimit の全画面のファイルは `browseModel` のメソッドを持たない。2026-10-12 に数えた)

## ビルドキャッシュの増え方 (2026-10-17 に実測)

使い捨ての worktree (origin/master) と空の GOCACHE で、`src/glogx` の `go test -race -count=1 -run '^$' .` を回し、各ファイルの先頭に
1 行足してビルドし直したときの GOCACHE の増分を測った (Go 1.26.0、14 コア):

| 変更 | GOCACHE の増分 |
|---|---|
| 空の GOCACHE から 1 回目 (std と依存を含む) | 228 MB |
| そのまま 2 回目 | +0.4 MB |
| `tui.go` に 1 行 | +27.5 MB |
| `doctor_view.go` に 1 行 | +27.5 MB |
| `issues_view.go` に 1 行 | +27.5 MB |

- どのファイルを直しても同じ量になる (1 パッケージなので、どこを直してもパッケージ全体を作り直す)
- 母集合: `-race` のテストのビルドだけ。`src/glogx/Makefile` の 2 本目 (-race なしの予算検査)・`go vet`・golangci-lint の分は数えていない
  (-race なしの版は同じだけ別に増えるはず。未実測)。GOCACHE は 5 日使われなかった項目を消すので、増えた分は 5 日で消える
- 手元の `~/Library/Caches/go-build` は 2026-10-17 に 54 GB (今日 15 GB・昨日 23 GB の項目)。最大級の項目は glogx のテスト版の
  コンパイル結果 (1 つ 26 MB 前後。worktree のパスが中に埋まっている)。並行セッションが worktree ごとに glogx を直すと、この単位で溜まる
- **分割で減る量は未実測**。画面のパッケージだけを直したときに main (`tui.go` など) を作り直すかは、依存先の export data が変わるかで
  決まる、と書いていたが、下の「実測 2」で依存先が変われば main は必ず作り直しになると分かった (2026-10-17)。**着手時に、土台と doctor を出した段で同じ計測を回し、
  `doctor_view.go` に 1 行足したときの増分を before (27.5 MB) と比べる**。`tui.go` を直したときは main を作り直すので、main に残る量が増分の下限になる
- 計測の手順 (着手時に使い回す): 使い捨ての worktree と `GOCACHE=$(mktemp -d)` で `go test -race -count=1 -run '^$' .` を 2 回回してから、
  対象のファイルの先頭に `// cache probe` を 1 行足して 3 回目を回し、`du -sk "$GOCACHE"` の差を見る
- 27.5 MB の内訳: 増えた項目の最大のもの (27,549,862 B) を含む上位 3 つは `!<arch>` のアーカイブ (パッケージのコンパイル結果) で、
  リンクの結果は GOCACHE に入っていない (2026-10-17 の 2 回目の反証レビューが `go test -race -run '^$' -x .` で確かめた)。増分の実体は
  コンパイル結果なので、作り直すパッケージの大きさで決まる

## 試算と設計 (2026-10-17。目的: 開発中のビルドキャッシュの増え方を減らす)

### 実測 1: `make test` が glogx で作る版ごとの増分

使い捨ての worktree と空の GOCACHE で、`src/glogx/Makefile` の `test:` と同じ 3 種を回し、`doctor_view.go` の先頭に 1 行足して回し直した:

| 版 | 空の GOCACHE から | 1 行の変更で |
|---|---|---|
| `go test -race` (Makefile の 1 本目) | 228 MB | +27.5 MB |
| `go test` (-race なし。2 本目の `TestFrameAllocBudget` の 1 本のため) | 207 MB | +24.2 MB |
| `go vet` | 2.4 MB | +0.0 MB |
| **1 行の変更ごとの計** | | **+51.7 MB** |

- 2 本目は `-run '^TestFrameAllocBudget$$'` の 1 本のために、パッケージ全体を -race なしでもう一度コンパイルしている。1 行の変更ごとの増分の
  **47% がこの 1 本の分**。予算の検査は「1 フレームの確保の回数」が -race の計装で揺れるので -race なしで判定している (Makefile のコメント)。
  1 フレームの**バイト数**の予算は 1 本目 (-race) の側で判定していて、2 本目が見るのは**回数**だけ (`frame_alloc_test.go` の TestFrameAllocBudget)
- CI も同じ Makefile を通す (`.github/workflows/_go-project.yml` の `make -C ${{ inputs.dir }} test`)。2 本目を外すと CI からも回数の判定が消える
- 測り方は上の節と同じ (`tmp/742-cache2-2026-10-17/measure.sh` で回し、結果をここに移して消した)

### 実測 2: Go は依存先が変わると、import する側を必ず作り直す

小さな module (パッケージ `a` と、それを import する main) で、`a` に 1 行足す・インライン化されない関数 (`//go:noinline`) の本体の
文字列だけを変える、のどちらでも `go build -race -x` の compile は 2 回 (a と main) だった。つまり**分割しても main は毎回作り直しになり、
減るのは「直していないパッケージ」の分だけ**。import の連鎖の上にいるパッケージほど、作り直しの範囲が広い

### 実測 3: 束ごとの大きさと依存 (go/types で非テストのファイル間の参照を数えた。使い捨ての道具)

| 束 (ファイル) | 非テスト | テスト | 計 (全体比) |
|---|---|---|---|
| main (`tui.go`・`gitlog*`・`github.go`・`fullscreen.go`・overlay 類・`main.go` など) | 9,962 | 16,918 | 26,880 (44%) |
| doctor (`doctor_*.go`) | 5,152 | 7,076 | 12,228 (20%) |
| issues (`issues_*.go`) | 3,959 | 7,219 | 11,178 (18%) |
| status (`status_view.go`・`worktree_status.go`) | 1,712 | 1,985 | 3,697 (6%) |
| 描画の部品 (`render`・`width`・`box`・`terminal`・`scroll_glide`・`line_cache`) | 898 | 1,444 | 2,342 (4%) |
| usage (`usage_*.go`) | 594 | 1,185 | 1,779 (3%) |
| ratelimit / filer / lan / `external_commands.go` | 241 / 313 / 315 / 421 | 675 / 469 / 425 / 0 | 各 1〜2% |

- 画面 → main の参照は、ほとんどが少数の共通の部品: `timeNow`・通知の種類 (`notice*`)・全画面の ID (`fullScreenID` / `crossTarget`)・
  再開の記憶 (`screenMemory`)・後片付けの待ち (`cleanupLatch`)・キャッシュの書き込み (`writeAtomic` / `cacheBaseDir`)・小さな道具
  (`firstLine` / `clampIdx` / `normalizeSpaceKey`)・`watchChain`・`urlPicker`、と最下行の案内 (`status_view.go` の `hintItem` / `fitHintItems`)
- 🚨 **`render.go` は純粋な描画の部品ではない**: git log の行の描画のために `gitlog.go` / `github.go` の型とそのフィールド・定数 (`Commit` / `CIState` / `State*` /
  `DecorColors` など。フィールドまで数えて 29 種、render.go と土台だけでコンパイルしたときの未定義の名前は 12 種・34 回) を参照する。土台へ出すには、ANSI・`paint`・幅の部品と、log の行の描画を割る必要がある
- filer と lan は `browseModel` のメソッド (`quit` / `toggleUsage` / `deliverNotice` / `maybeTick` / `openFullScreen`) と、`tui.go` の関数・差し替え点
  (`currentDir` / `runEditorCmd`) を直接呼ぶので、画面から main へ頼む口 (interface) を作るまで切り出せない
- **画面のテストの大半は `browseModel` を作る**: doctor 7,076 行のうち 5,543 行、issues 7,219 行のうち 4,915 行、status 1,985 行のうち 1,512 行
  (`worktree_status` のテストは作らない)、usage 1,185 行のうち 984 行、ratelimit・filer・lan は全部 (ファイル単位で `browseModel` / `newTestBrowse` を含むかを数えた。
  ファイルの一部だけが作る場合も全部を数えているので、上限の見積もり)。これらは画面のパッケージへ移せない (main の型を使う) ので、
  テストを各パッケージへ移すなら「画面だけのテスト」と「main と組み合わせるテスト」に書き分ける必要がある

### 試算: 1 行の変更ごとの増分 (-race 版。行数に比例すると仮定した見積もりで、実測ではない)

今は 60,963 行で +27.5 MB (1 行あたり約 0.45 KB)。依存先が変わると main が作り直しになる (実測 2) ので、作り直すものの行数の和で
見積もる。直した画面は「本体の版」(main が import する) と、テストを持つなら「テスト込みの版」の 2 回コンパイルされる。main はテスト込みの 1 回:

| 案 | doctor を直したとき | `tui.go` を直したとき |
|---|---|---|
| 今 | 27.5 MB | 27.5 MB |
| A: 画面を分け、テストは main に残す (issue 741 と同じ) | 約 24 MB (−13%。doctor 5,152 + main 47,986 行) | 約 22 MB (−21%。main 47,986 行) |
| B: A に加えて、`browseModel` を作らない画面のテスト (約 4,500 行) を各パッケージへ移す | 約 25 MB (−9%。doctor が 2 回コンパイルされ、A より悪い) | 約 20 MB (−29%) |
| C: 画面のテストを全部、画面のパッケージへ移す (main と組み合わせるテストは書き分ける) | 約 21 MB (−25%。doctor 5,152 + 12,228 + main 28,402 行) | 約 13 MB (−53%。main 28,402 行) |
| D: 2 本目の -race なしの run を毎回の `make test` から外す (分割と独立) | 1 行ごとの計 51.7 MB → 27.5 MB (−47%) | 同じ |

- main の行数の内訳: A の 47,986 = main の非テスト 9,962 + 後回しの filer 313・lan 315 + テスト全部 37,396。C の 28,402 = 9,962 + 313 + 315 +
  main のテスト 16,918 + filer・lan のテスト 894
- 🚨 C は楽観側: 画面のテストのうち `browseModel` と組み合わせる部分 (doctor 5,543 行・issues 4,915 行など) は書き分けても main に残るものがあり、
  main は 28,402 行より大きくなる。どれだけ画面だけのテストに書き直せるかは、着手時に数える
- A〜C は -race 版だけの数字。-race なしの版 (2 本目) が残る限り、同じ割合でもう 1 つ乗る
- **費用に対する効き目は D が最大で、分割 (A) 単独の効き目は小さい**。分割でキャッシュを大きく減らすには、テストを画面ごとに書き分ける (C) まで要る。
  中途半端にテストを移す (B) と、移した画面を直したときはかえって増える (その画面を 2 回コンパイルするのに、main 側はあまり減らない)
- D で素通りになるもの: 1 フレームの確保の**回数**の上限だけ (バイト数の上限は -race の 1 本目に残る)。回数の余裕は 3 回前後と小さく
  (`fullscreen.go` の issues-40 は 2 回)、小さな確保が増える退行は、バイト数の余裕 (数%) に吸収されて回数の側でしか見えないことがある。
  CI も同じ Makefile を通すので、外すと CI からも消える。外すなら回数の判定を別の口 (例: 新設の `make test-budget` を CI のジョブにだけ置く) へ移す。
  `make test-budget` は今は無い。issue 323 の変異 (doctor の `lines()` に行ごとの確保を足す) が、外した後もバイト数の判定だけで red になるかは未確認。
  検査を外す判断なので、採るかはユーザーが決める (`~/.claude/rules/list-masked-failure-modes-before-removing-guard.md`)
- 見積もりの前提 (行数に比例) は確かめていない。着手したら、段ごとに上の「計測の手順」で実測して置き換える

### 設計 (案)

| 層 | パッケージ (案) | 持つもの | 出すときの注意 |
|---|---|---|---|
| 0 | `internal/term` | ANSI と `paint` / `clipToWidth` / `stripANSI` / `wrapToWidth` (`render.go` の純粋な部分)・`width.go`・`box.go`・`terminal.go`・`scroll_glide.go`・`line_cache.go`・最下行の案内 (`hintItem` / `fitHintItems`) | `render.go` の log の行の描画は main に残す。depguard の render-pure (`**/render.go`) の掛かり方を確かめる |
| 0 | `internal/gitdata` | `gitlog.go` / `github.go` の型 (`Commit` / `CIState` / `PRRef` …) と git・gh の実行 (`runGitTimeout` / `CommandRunner`) | そのままでは main 抜きで成り立たない: `Options` (`options.go`)・`sanitize*` / `dropEmojiVS16` (`terminal.go` → term へ出す)・`StateLoading` を参照する。`Options` を渡す形にするか、型だけを先に出す |
| 0 | `internal/app` (名前は要検討) | `timeNow`・通知の種類・全画面の ID (ID だけ。開く処理 `openFullScreen` は main)・`screenMemory`・`cleanupLatch` (この 2 つは単独でコンパイルが通る)・キャッシュの書き込み・`firstLine` などの小さな道具・`watchChain` (先に `dirWatcher` の interface を `issues_watch.go` から出す) | forbidigo の `time.Now` の許可 (`path-except: tui\.go`) を `timeNow` の行き先に合わせる。`urlPicker` は `issuesRenderOpts` を使うので app ではなく issues へ |
| 0 | `internal/extcmd` | `external_commands.go` と `codex_version.go` (`fetchInstalledCodexVersion` を使われる) | `cleanupLatch` (app)・`firstLine` / `lastLine`・`Repo` / `CommandRunner` (gitdata) を参照するので app と gitdata の後。テストの差し替え点 (パッケージ変数) をどちらに置くか |
| 1 | `internal/doctor` → `internal/issues` → `internal/status` → `internal/usage` / `internal/ratelimit` | 各画面 | 大きい順 (=効き目の順) で、doctor は依存も最も少ない (main へは共通の部品だけ) |
| — | 後回し | filer / lan | `browseModel` へ頼む口 (interface) を作ってから |
| — | main | `tui.go` (`browseModel`)・git log の取得と監視・overlay 類・`fullscreen.go`・`main.go` / `options.go` | |

- 段ごとの費用の目安 (main から呼ばれる名前 = 公開名にする数): 描画の部品 67 種・doctor 59・issues 49・status 35・ratelimit 26・
  usage 21・extcmd 11 (filer 24・lan 23)。逆向き (画面から共通の部品へ) は doctor 31 種・issues 65 種ほど
- 順番: 土台 (term → gitdata → app / extcmd) → doctor → issues → status → usage / ratelimit。1 段ごとに「計測の手順」で増分を測り、
  見積もりと比べる。効き目が見積もりより小さければ、その段で止める判断もありうる
- 下の「対応方針」「着手前に潰す罠」は 2026-10-12 の案。上の設計とは、`internal/ui` を term / app に分けた点だけが違う (土台を先に出す順・`hintItem` を先に出す・
  JSON の罠・CI と lint の配線はそのまま使う)
- 公開名の数 (67 / 59 / 49 …) は、main から各束の宣言を参照している名前 (フィールド・メソッドを含む) の種類の数で、検証は着手時に gopls rename の結果で行う

## 対応方針 (2026-10-12 の案。着手時に上の設計と合わせて設計のレビューを通して決める)

| 層 | パッケージ (案) | 持つもの |
|---|---|---|
| 0 | `internal/ui` (名前は要検討) | 描画の土台 (render / width / box / terminal)・通知の種類・最下行の案内 (`hintItem`)・全画面の ID・スクロールの滑走・行のキャッシュ |
| 0 | `internal/extcmd` | `external_commands.go` (git / tmux / claude / ブラウザ / クリップボード。テストの差し替え点) |
| 1 | `internal/issuesview` / `internal/doctorview` / `internal/statusview` など | 各画面 (`issues_*.go` / `doctor_*.go` / `status_view.go` + `worktree_status.go`)。usage・ratelimit の全画面も候補。filer は `browseModel` のメソッドを分けてから |
| — | `main` | `tui.go` (`browseModel`)・git log の取得と監視・CI の取得とキャッシュ・main / options |

- 挙動を変えない commit として単独で入れる。等価の証拠は issue 741 と同じ 3 つ (テストを変えずに緑・テストの名前の集合が前後で同じ・
  トップレベルの宣言のトークン列を改名と修飾を戻して比べる)
- 段階に分けてよい (土台の層を出す → 画面を 1 つずつ出す)。1 段ごとに上の 3 つで確かめる
- 🚨 順序: `hintItem` / `fitHintItems` (今は `status_view.go`) を土台へ出してから画面を出す。先に status を出すと issues / doctor が
  status を import する逆向きになる。main は下位から import できない (CLAUDE.md の「構造の判断」) ので、画面が main から使っている宣言
  (`notice*` / `timeNow` / `fullScreenID` …) は全部、画面より先に土台へ出す
- import パスは `glogx/internal/...` (go.mod の `module glogx`。既存の `glogx/issues` と同じ流儀)

## 着手前に潰す罠 (issue 741 で踏んだもの + glogx 固有)

- 🚨 **非公開のフィールドを公開名にすると encoding/json に出るようになる**。issue 741 では golden のテストが混入を捕まえた。
  glogx で JSON を書くのは `cache.go` / `claude_version.go` / `doctor_cache.go` / `usage_cache.go` と、全画面の再開の記憶を 1 か所で書く
  `screen_memory.go` (2026-10-17 に数え直した。`doctor_resume.go` / `filer_resume.go` / `issues_state.go` / `ratelimit_resume.go` / `lan_resume.go` は
  `screen_memory.go` を通して書く)。公開名にした
  フィールドが永続化の形を変えないかを、書く構造体ごとに確かめる (`json:"-"` を付けるか、型を分ける)。読む側 (`cli_health.go` /
  `github.go` の `json:` タグ付きの構造体) も、公開名にしたフィールドが余計に埋まらないかを見る
- **パスで掛かっている CI・lint の配線が移設で外れる** (`~/.claude/rules/list-masked-failure-modes-before-removing-guard.md` の「コードを別の
  module / ディレクトリへ移すとき」):
  - `.github/workflows/doctor.yml` の paths filter (`src/glogx/doctor_*.go` / `src/glogx/tui.go`) と `go test -run 'TestDoctor' -v .`
    (カレント = main だけ)。doctor を移すと filter が掛からず、`.` の TestDoctor が減る (本数の下限の検査があるので赤にはなる)
  - `src/glogx/Makefile` の `go test -run '^TestFrameAllocBudget$$' .` と `tests/glogx/bench_budgets.ci` のコメント (`frame_alloc_test.go` が main にある前提)
  - `src/glogx/.golangci.yml` (repo の root には無い): depguard の render-pure は `**/render.go` なので名前を変えずに移せば掛かり続ける。forbidigo の `time.Now` の禁止は
    `path-except: tui\.go` で許可しているので、`timeNow` を土台へ移すならこのパスも直す。`path: main\.go` / `_test\.go` の除外も見直す
  - `gorules/` (ruleguard) はパス指定なし
- **ソースを読む検査のテストの内訳** (反証レビューで数え直した):
  - `WalkDir(".")` で再帰する 5 本 (`cache_write_discipline_test.go` / `vs16_literal_test.go` / `issues_rows_setter_test.go` /
    `details_flags_test.go` / `waitdelay_discipline_test.go`) は main に残せば `internal/` も見る。件数の下限もある
  - 🚨 `clock_rollback_test.go` は `parser.ParseDir(".")` で**再帰しない**。移したファイルを黙って見なくなり、下限も無い
    (`len(pkgs)==0` だけ)。移す前に走査を再帰にし、件数の下限を足す (`vs16_literal_test.go` の冒頭に同じ罠の記録がある。issue 283)
  - 固定名で parse / 読む 4 本 (`hint_registry_gate_test.go` / `fullscreen_test.go` の `tui.go`、`notice_kind_test.go` の
    `issues_view.go` / `issues_linkjump.go` / `status_view.go`、`doctor_cleanup_test.go` の `main.go` と、同じテストの `tui_helpers_test.go` /
    `gitlog_watch_test.go`。後の 2 つは 2026-10-17 の反証レビューで見つけた)。移すと読めずに赤になるので見落としは
    起きにくいが、パスを直す
  - `own_sources_test.go` の `ownSourceRoots` (`.` と `../tuikit` などを走査する検査が使う) は `.` から再帰するので `internal/` も見る
- テストを main に残すか各パッケージへ移すか: issue 741 は cwd 相対の testdata とテストのバイナリの起こし直しのため main に残した。
  glogx でも同じ前提が無いかを先に数える (残すとテストのためだけの公開名が増える)。画面のテストは `tui_helpers_test.go` の共有ヘルパー
  (`installInertDoctor(t, *doctorView)` / `newTestBrowse` → `*browseModel`) を使うので、移すならヘルパーが両側から要る
- 識別子の改名を gopls rename で当てるとき、定義の行を正規表現で探すと別の識別子に当たる (issue 741 で 2 件)。改名の後に語単位の差分で
  「旧名 → 新名」の組を全部数え、想定の組だけであることを確かめる
- テストが書き換えるパッケージ変数 (時計・外部コマンドの差し替え点など。非テストの `^var ` で始まる行が 51。うち `var (` のブロックが 2 個で、中の変数の数は数えていない。2026-10-17) は、テストと同じパッケージに
  置くか公開変数にする。どれをテストが書き換えるかの表は着手時に作る
- ビルドタグ付きの `ime_tis_darwin.go` (`darwin && cgo`) / `ime_tis_stub.go` は main に残す前提 (画面の切り出しとは別系統)。
  `init()` と `go:embed` は非テストに無い (2026-10-12 に grep で 0 件)

## 反証レビュー (sonnet 1 本。2026-10-12)

数値と列挙の大半 (ファイル数・行数・テスト関数・`browseModel` のメソッド・JSON を書く 8 本) は反証できなかった。採用して直した: CI の配線
(doctor.yml / Makefile) の漏れ・ソースを読むテストの内訳 (10 本が走査、は誤り。再帰 5 + `ParseDir` 1 + 固定名 4)・`hintItem` を先に出す順序・
テストの共有ヘルパー・`timeNow` の lint のパス・3 問への答え。却下: 「ratelimit の全画面も `browseModel` のメソッドを持つ」(数え直すと 0 個)

## 関連

- issue 741 — zundamon-kaisetsu の同じ分割 (使った道具・確かめ方・踏んだ罠)
- `src/glogx/CLAUDE.md` の「構造の判断」・README の「glog との共通コード分離について」

## 進捗

- [x] 2026-10-17: open に戻し、現状の数字を数え直し、ビルドキャッシュの増え方を測った (上の節)
- [x] 2026-10-17: 案 D を採った (ユーザー判断)。確保の回数の予算 (-race なしの `TestFrameAllocBudget`) を手元の `make test` から外し、
  `make test-budget` に分けて CI (環境変数 `CI`) でだけ回す (glogx: 確保の回数の予算 (-race なしの TestFrameAllocBudget) を手元の make test から外し、CI でだけ回す (issue 742 の案 D))。
  変異 (一覧の回数の上限 130 → 10) で `make test-budget` が red になることを確かめた。分割 (A → C) は、トークンに余裕があるときにユーザーが着手する
- [x] 2026-10-17: 設計は上の「設計 (案)」の層の順をそのまま採った (2026-10-17 の「試算と設計」の反証レビュー済み)。土台は term (描画) と
  app (画面が共有する状態) の 2 つにし、gitdata / extcmd は使う画面を出す段で要る分だけ出す。改名は先頭の大文字化だけにして、
  等価の確認 (トークン列の比較) で機械的に戻せる形にした
- [x] 2026-10-17: 土台の層を出す
  - 段 1 (glogx: 描画の土台を internal/term へ出す (issue 742 の段 1)): width / box / terminal / scroll_glide と render.go の純粋な部分・
    hintItem / fitHintItems を `internal/term` へ。コミット行の描画は main の render.go に残した
  - 段 2 (glogx: 画面が共有する状態と道具を internal/app へ出す (issue 742 の段 2)): timeNow / 全画面の ID と crossTarget /
    screenMemory / cleanupLatch / cacheBaseDir と writeAtomic を `internal/app` へ
  - 等価の確認は各段で 3 つ (go test -race 緑・テスト名 1,241 本の一致・トークン列を改名と修飾を戻して比べ、差は意図した検査の直しだけ)
  - 段 1 の反証レビュー (sonnet 1 本): P1 / P2 なし。P3 (古い名前・パスのコメント 7 か所) は段 2 で直した。
    却下なし。記録: hint の gate は「`term` という名前のローカル変数が package を隠す」形を数え違える (現実に起きにくいので受容)、
    t.Run の副テスト名が表のラベル (`term.AnsiRed` など) の書き換えで変わった (`-run` で副テストを名指す箇所は repo に 0 件)
  - 移設で踏んだ罠 (次の段でも同じ形で出る):
    - cwd 相対でファイルを読むテストは、移すと **Skip で緑**になる (`box_test.go` の theme/colors.yml。パスを直して PASS 行を確かめた)
    - ソースを読む検査が**移す前の名前**で照合している: `hint_registry_gate_test.go` (fitHintItems の素の識別子)・
      `cache_write_discipline_test.go` (cacheBaseDir)・`clock_rollback_test.go` (`.After(timeNow())` の字句と、再帰しない ParseDir)。
      どれも名前の照合を新しい形に直し、変異で red を確かめた
    - ジェネリック型 (ScreenMemory) のメソッド・フィールドは、使う側ではインスタンスの別オブジェクトになるので、go/types の
      Uses から改名すると定義側が漏れる (build が落ちるので気づける)
    - 別パッケージの構造体を位置指定のリテラルで書くと vet (composites) が落ちる (HintItem で 138 か所。キーを付けた)
    - nolint が置き場所の変更で unused になる (`app.TimeNow` の forbidigo。nolintlint が落とす)
  - 外れた防御 (記録): `scripts/check_unused_excluding_tests.sh` (staticcheck U1000 -tests=false) は exported を見ないので、
    internal へ出して公開名にした宣言が「テストからしか使われない」になっても検出されない。2026-10-17 時点で該当は 0 個
    (term・app の公開名を数えた)。**trigger**: 画面のパッケージを出した段で同じ数え方をし、該当が出たら internal の公開名を対象にした検査を足すかを決める
- [ ] 画面を 1 つずつ出す
- [ ] CLAUDE.md / README の構造の記述を直す

## 反証レビュー (sonnet 1 本。2026-10-17 の改訂)

数え直した数字 (ファイル数・行数・テスト関数・`browseModel` のメソッドの内訳・JSON を書くファイル・CI と lint の配線) は反証できなかった。
採用して直した: 概要の「151 ファイル」の直し損ね・`var` の数え方の明記・`src/glogx/.golangci.yml` のパス・キャッシュの増分の内訳 (コンパイルか
リンクか) を着手時に切り分けること・`doctor_cleanup_test.go` が固定名で読むテストのファイル。
却下: 「`wantQuit` は存在しない」(`issues_view.go` の `issuesView.wantQuit` に在る) と「`tui.go` の 3 画面の型の参照は 12 回」
(数え直すと doctorView 4・issuesView 5・statusView 5 の 14 回で本文どおり)

## 反証レビュー (sonnet 1 本。2026-10-17 の「試算と設計」)

束の行数・`browseModel` を作るテストの行数・D の 47%・A と C の算術・「画面は本体とテスト込みの 2 回、main はテスト込みの 1 回」の前提は
反証できなかった。採用して直した: `urlPicker` (issues の型を使う) と `watchChain` (`dirWatcher` が要る) は app に置けない・gitdata と extcmd の
未定義 (`Options` / `codex_version.go` ほか)・C は楽観側 (組み合わせのテストは main に残る)・A と C の main の行数の内訳・D は CI からも外れ、
`make test-budget` は今は無い、素通りになるのは回数の上限だけ・status と usage のテストの数え直し・render.go の数え方・前の節の
「main は作り直さない見込み」の否定・27.5 MB の内訳 (コンパイル結果。リンクは入らない)。
却下: 「A の main は 47,358 行」(後回しの filer・lan の 628 行を main に数えるかの違い。本文に内訳を書いた)・「`runEditorCmd` は関数」は
差し替え点のパッケージ変数 (`var runEditorCmd = func…`、tui.go) で、メソッドではない点は採った
