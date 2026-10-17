# 742 (refactor): glogx を画面ごとのパッケージに割る

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
- 探すコストが下がるか: 平場の 151 ファイルから、画面ごとのディレクトリになる。ただしこれ単独では分割の理由にしない (CLAUDE.md の
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
  決まる (行がずれるだけで export data が変わらなければ、main は作り直さない見込み)。**着手時に、土台と doctor を出した段で同じ計測を回し、
  `doctor_view.go` に 1 行足したときの増分を before (27.5 MB) と比べる**。`tui.go` を直したときは main を作り直すので、main に残る量が増分の下限になる
- 計測の手順 (着手時に使い回す): 使い捨ての worktree と `GOCACHE=$(mktemp -d)` で `go test -race -count=1 -run '^$' .` を 2 回回してから、
  対象のファイルの先頭に `// cache probe` を 1 行足して 3 回目を回し、`du -sk "$GOCACHE"` の差を見る

## 対応方針 (案。着手時に設計のレビューを通して決める)

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
  - `.golangci.yml`: depguard の render-pure は `**/render.go` なので名前を変えずに移せば掛かり続ける。forbidigo の `time.Now` の禁止は
    `path-except: tui\.go` で許可しているので、`timeNow` を土台へ移すならこのパスも直す。`path: main\.go` / `_test\.go` の除外も見直す
  - `gorules/` (ruleguard) はパス指定なし
- **ソースを読む検査のテストの内訳** (反証レビューで数え直した):
  - `WalkDir(".")` で再帰する 5 本 (`cache_write_discipline_test.go` / `vs16_literal_test.go` / `issues_rows_setter_test.go` /
    `details_flags_test.go` / `waitdelay_discipline_test.go`) は main に残せば `internal/` も見る。件数の下限もある
  - 🚨 `clock_rollback_test.go` は `parser.ParseDir(".")` で**再帰しない**。移したファイルを黙って見なくなり、下限も無い
    (`len(pkgs)==0` だけ)。移す前に走査を再帰にし、件数の下限を足す (`vs16_literal_test.go` の冒頭に同じ罠の記録がある。issue 283)
  - 固定名で parse / 読む 4 本 (`hint_registry_gate_test.go` / `fullscreen_test.go` の `tui.go`、`notice_kind_test.go` の
    `issues_view.go` / `issues_linkjump.go` / `status_view.go`、`doctor_cleanup_test.go` の `main.go`)。移すと読めずに赤になるので見落としは
    起きにくいが、パスを直す
  - `own_sources_test.go` の `ownSourceRoots` (`.` と `../tuikit` などを走査する検査が使う) は `.` から再帰するので `internal/` も見る
- テストを main に残すか各パッケージへ移すか: issue 741 は cwd 相対の testdata とテストのバイナリの起こし直しのため main に残した。
  glogx でも同じ前提が無いかを先に数える (残すとテストのためだけの公開名が増える)。画面のテストは `tui_helpers_test.go` の共有ヘルパー
  (`installInertDoctor(t, *doctorView)` / `newTestBrowse` → `*browseModel`) を使うので、移すならヘルパーが両側から要る
- 識別子の改名を gopls rename で当てるとき、定義の行を正規表現で探すと別の識別子に当たる (issue 741 で 2 件)。改名の後に語単位の差分で
  「旧名 → 新名」の組を全部数え、想定の組だけであることを確かめる
- テストが書き換えるパッケージ変数 (時計・外部コマンドの差し替え点など。非テストのトップレベルの `var` が 51 行。2026-10-17) は、テストと同じパッケージに
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
- [ ] 設計 (層と置き場所) を決めて反証レビューを通す
- [ ] 土台の層を出す
- [ ] 画面を 1 つずつ出す
- [ ] CLAUDE.md / README の構造の記述を直す
