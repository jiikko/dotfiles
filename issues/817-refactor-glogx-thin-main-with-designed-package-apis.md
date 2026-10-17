# 817 (refactor): glogx の main を薄くする (API を絞った package の境界)

> 🚨 **担当中: M4Pro-MacBook-Pro (glogx)**（2026-10-17〜）

起票日: 2026-10-17

## 概要

glogx は `package main` に約 6 万行 (非テストの 62 ファイル・テストの 94 ファイル。issue 742 の 2026-10-17 の数え直し) が
並んでいた。Go では main を起動と配線だけの薄いものにし、本体を `internal/` などの package に置くのが一般的なので、
**設計の理由で** main を薄くする。issue 742 は「ビルドキャッシュを減らす」を動機に画面ごとの分割を始めたが、実測で
キャッシュへの効き目は小さく (下)、キャッシュの問題は issue 816 (-trimpath) で扱う。この issue は分割を設計として続ける。

## 742 で済んでいること

- `internal/term` (描画の土台)・`internal/app` (画面が共有する状態と道具)・`internal/doctorview` (doctor 画面) を出した
- doctor のテストは 3 か所に置き分けた: 画面の中を見るテスト (`package doctorview`)・公開面だけで書けるテスト
  (`*_ext_test.go` = `package doctorview_test`)・browseModel と組み合わせるテスト (main。約 20 本)。共有のテスト部品は
  `internal/doctorview/doctortest` (本番からの import は depguard が止める)
- 移設で外れかけた検査の直し方と罠は issue 742 の進捗と retro 815 にある

## 742 のやり方から変えること

- **公開名を機械的に増やさない**。742 は改名を「先頭の大文字化」だけにして等価を機械で確かめられる形にしたが、
  その結果 doctorview は 201 個 (段 3。改名した数)・term 48 個 (改名した数)・app は公開宣言 26 個 (型 3・関数とメソッド 11・定数と変数 10・
  フィールド 2。2026-10-17 の codex の数え直し。改名の表の 45 はジェネリックのインスタンスを重複して数えていた) を公開した (中身のフィールドまで)。Go は package の公開面を小さく
  保つので、画面ごとに「main が画面に頼むこと」(開く・閉じる・キーを渡す・描く・通知を受け取る) を API として設計し、
  それ以外は非公開に戻す
  - 既に出した doctorview の公開面も絞り直す (main と外部テストから実際に使われている名前を数えるところから)
  - 公開名を増やすと lint の検査範囲が狭まる (unparam の定数引数・unused は公開名を見ない。742 の記録) ので、絞ることは検査の面でも効く
- テストは検査する package の隣に置く。browseModel が要るテスト (キーの振り分け・トースト・終了経路などの配線) だけを main に残す

## 進め方 (セクションごとのステップ)

大きいので、セクションごとに独立した commit / push の単位で進める。各ステップで「テスト緑・テスト名の一致・lint」と、判断が
入るステップでは codex の敵対的レビューを通す (742 の手順。移設が外す検査の形は retro 815)。

### 0. 共通の準備

- [x] 0-1 公開面の棚卸しの道具: package ごとに、公開名と「どこから使われているか」(main の本体 / main のテスト / 外部テスト /
      package の中だけ) を出す。2026-10-17 の概数: main の本体が使う doctorview の名前 48 種、main のテストを含めると 76 種
      (grep で `doctorview.X` を数えた。メソッド・フィールドは数えていない)
- [x] 0-2 「main が画面に頼むこと」の型を決める: 開く / 閉じる / キーを渡す / 描く (lines・hint) / 非同期の結果を渡す (receive) /
      画面からの合図を受け取る (通知・横断・終了・コピー)。画面ごとに名前と戻り値の語彙がばらばらなので (742 の fullscreen_id.go の
      注記: interface のレジストリは 1 フレームの確保が増えるので採らなかった)、interface にするかは確保の予算 (TestFrameAllocBudget) を見て決める
- [x] 0-3 公開面を守る検査を置くか決める (package ごとの公開名の一覧を golden にする・増えたら落とす、など)。置かないなら理由を書く

### H. 再発防止 (main が太るのを機械で止める。ユーザー要望 2026-10-17。0 と並べて最初にやる)

golangci には「main を薄く」をそのまま表す規則が無いので、この repo の他の検査と同じく、テストで構造を検査し、例外は許可リストに
理由つきで書かせる形にする。どれも入れるときに変異で red を確かめる (`~/.claude/rules/mutation-verify-new-tests.md`)。
入口は src/glogx/CLAUDE.md の「構造の判断」に 1 行足す。

- [x] H-1 main の非テストのファイルの許可リスト (ラチェット): `src/glogx` 直下の非テストの .go の集合を一覧と突き合わせ、一覧に無い
      ファイルが増えたら落とす。足すには一覧に理由を書く (レビューで見える)。移して減ったら一覧からも消す (消し忘れも落とす)。
      2026-10-17 時点で 45 ファイル
- [x] H-2 browseModel のメソッドを持つ非テストのファイルの許可リスト: 2026-10-17 時点で 9 ファイル (tui.go・gitlog_watch.go・
      fullscreen.go・hint_surfaces.go・open_workspace.go・filer_view.go・filer_resume.go・lan_view.go・lan_resume.go)。新しい画面が
      browseModel にメソッドを生やす形 (filer / lan が辿った形) を止める。F を済ませたら filer / lan を一覧から消す
- [x] H-3 internal の package の公開面の golden (0-3 と同じもの): package ごとの公開名の一覧を置き、増えたら落とす。742 で doctorview が
      機械的に 201 個を公開した形を止める。画面ごとに公開面を絞ったステップ (A-2・C-2 など) で、その package の一覧を足す
- [x] H-4 main の非テストの総行数の上限 (下げる方向だけ動かすラチェット) を採るか決める。H-1 / H-2 は既存のファイル (tui.go など) が
      太るのを止めないので、それを止める唯一の形。ただし正当な機能追加も止め、「行数や責務の見た目で割らない」(src/glogx/CLAUDE.md) と
      緊張する。採らないなら、止めない形として H-1 の検査のコメントに書く
- 止めない形 (検査の冒頭に書く): 既存ファイルの中での肥大 (H-4 を採らない場合)・一覧を更新して通す変更 (レビューの責務)・
  internal の package どうしの依存の向き (Go のコンパイラは main の import だけを禁じる。必要になったら depguard で足す)

### A. doctorview の公開面を絞る (済んだ画面で型を固める)

- [x] A-1 0-1 の道具で doctorview の公開名を分類する (main の本体が使う / main のテストだけが使う / 外部テストだけが使う / 中だけ)
- [x] A-2 main の本体が使う口を 0-2 の形の API にまとめ、それ以外を非公開に戻す
- [x] A-3 main のテスト・外部テストが中身 (フィールド) を見ている箇所は、API 経由に書き換えるか、`package doctorview` の中のテストへ戻す
- [x] A-4 doctortest の公開面も同じく絞る
- [x] A-5 JSON に載る構造体 (doctor_cache.go / doctor_resume.go) のフィールド名を変えていないことを確かめる

### B. 土台 (term / app) の公開面

- [x] B-1 term・app の公開名のうち、package の外から使われないもの (例: `term.HintfitItems` は外からはテストだけ。
      🚨 `term.BuildPanelBoxImpl` は zoom.go が本番で使うので、戻すなら呼び出し側の移設か代わりの API が要る) を非公開に戻す
- [x] B-2 app の `ScreenMemory` などのフィールドを公開している理由を見直す

### C. issues 画面 (`internal/issuesview`)

- [x] C-1 下準備 (742 で 2026-10-17 に数えた): url_picker は issues と一緒に、watch_chain は dirWatcher・newDirWatcher・drainWatchEvents と一緒に
      app へ (interface だけでは足りない)、editorCommand など 4 関数と runEditorCmd・editorClosedMsg・openURLMsg・openInBrowser・copyToClipboard は
      extcmd (新設) へ、notice の種類・firstLine・clampIdx・normalizeSpaceKey・clipboardFailText は app へ
- [x] C-2 本体を移す。**機械的に先頭を大文字にせず**、main から使われる名前だけを 0-2 の形で公開する
- [x] C-3 テストを置き分ける (中を見るテスト / 外部テスト / browseModel と組み合わせるテストは main)。共有のテスト部品は issuesviewtest へ
- [x] C-4 移設が外す検査を洗う (742 の罠の一覧: 固定名で読むテスト・reflect・識別子名の照合・TestMain・実コマンドの監視の範囲・CI の paths)

### D. status 画面 (`internal/statusview`)

- [x] D-1 status_view.go と worktree_status.go を移す (hintItem は term へ出し済み)。lineCache は diff / job の overlay とも共有するので app へ
- [x] D-2 テストを置き分ける

### E. usage / ratelimit

- [x] E-1 usage_overlay / usage_cache と ratelimit_dashboard / ratelimit_resume を移す (どちらも browseModel のメソッドを持たない。742 で数えた)
- [x] E-2 テストを置き分ける

### F. filer / lan

- [x] F-1 browseModel のメソッド (`quit` / `toggleUsage` / `deliverNotice` / `maybeTick` / `openFullScreen`) と tui.go の差し替え点
      (`currentDir` / `runEditorCmd`) への依存を、画面から main へ頼む口 (0-2) に置き換える
- [x] F-2 移す・テストを置き分ける

### G. main に残すものを整理する

- [x] G-1 git log の取得と CI の取得 (gitlog.go / github.go / cache.go) を `internal/gitdata` へ出すか決める (render.go のコミット行の描画が使う)
- [x] G-2 overlay 類 (diff / job / pr_status / action_modal) の置き場所を決める
- [x] G-3 main に残るのが「起動・配線・browseModel の状態機械」だけになったら、src/glogx/CLAUDE.md・README の構造の記述を直して閉じる

## 参考: キャッシュへの効き目 (742 の実測。動機ではない)

1 行足したときの GOCACHE の増分 (`./...` を空の GOCACHE で): doctor のテストを移した後 (3656c979) で tui.go 27.5 → 22.5 MB (−18%)・doctor 27.5 → 31.1 MB (+13%)。
土台 (term) は段 3 の直後 (6205cb18) の測定で 27.5 → 30.8 MB (doctor のテストを移した後は未測定)。直近 3 か月の直す頻度で重み付けすると正味 −1 割程度 (直す回数の 8 割が main)。

## 関連

- issue 742 — 段 1〜3 と doctor のテストの置き分けの経緯・実測・罠 (この issue が続きを持つ)
- issue 816 — キャッシュの問題は -trimpath で扱う
- retro 815 — 移設が「名前・パッケージ・可視性」に掛かった検査を外す形

## 反証レビュー (codex 1 本。2026-10-17)

P2 2 件・P3 2 件を採用して直した: B-1 の BuildPanelBoxImpl は zoom.go が本番で使う・C-1 の下準備の漏れ (editorClosedMsg・openInBrowser・copyToClipboard・clipboardFailText・newDirWatcher・drainWatchEvents)・app の公開名の数 (26)・キャッシュの測定段階の混在。分割済みの構成・テストの置き分け・depguard・lint の説明・742 / 815 への参照は反証できなかった。

## 進捗

- [x] 2026-10-17 H と 0 (glogx: main を薄く保つ構造の検査を足す (issue 817 の H)):
  - H-1 / H-2 / H-3 は `src/glogx/structure_ratchet_test.go` の 3 本 (`TestMainSourceFilesAllowlist` / `TestBrowseModelMethodFilesAllowlist` /
    `TestInternalAPIGolden`)。一覧は「移設待ち (issue 817 の C)」のように行き先を理由に書いたので、この issue の進み具合の表にもなる。
    golden は `src/glogx/testdata/api/<package>.txt` (公開の func / type / const / var・メソッド (受け手が非公開でも)・公開のフィールド)。
    internal の下に golden の無いパッケージができても落ちる (「画面ごとに足す」ではなく全パッケージに要求した)
  - 変異で red を確認: main の直下に .go を足す・一覧の行を消す・一覧に実在しないファイルを足す・一覧に無いファイルに
    browseModel のメソッドを足す・internal/app に公開の関数を足す (5 本とも red、戻して緑)
  - H-4 (総行数の上限) は採らなかった: 正当な機能追加も止め、通すために行数のためだけの移設を誘う (CLAUDE.md「行数や責務の見た目で割らない」と逆向き)。
    止めない形として検査の冒頭に書いた
  - 0-1: 棚卸しは golang.org/x/tools/go/packages で型を解いて「公開名 × 使う側 (main の本体 / main のテスト / 外部テスト / 中だけ /
    別の internal)」を出す使い捨ての道具で行った (repo に置くと module が 1 つ増え、CI のレーンの検査 (check_go_project_lanes) と
    workflow が要るので置かない。golden が恒常の検査を担う)。2026-10-17 の doctorview: 公開名 287 (フィールド・メソッド込み) のうち
    main の本体が使う 59・main のテストだけ 89・外部テストだけ 79・doctortest だけ 56・中だけ 4
  - 0-2: interface にはしない。画面ごとの具体型に共通の動詞のメソッドを持たせる (Visible / Toggle / Close / OwnsKeys / HandleKey /
    Lines / Hint / Receive… / TakeWantCross / TakeWantQuit / TakeNotice / Animating / Busy)。理由: 引数の型 (描画の opts・ページ幅)
    と戻り値 (doctor は結果の enum、issues は tea.Cmd) が画面ごとに違い、共通の interface に寄せると変換の層が要る。
    interface のレジストリは 1 フレームの確保が増える (742 の fullscreen_id.go の注記)。動詞は main の issuesView / statusView /
    filerView / lanView と doctorview に既にある名前を採った
  - 0-3: H-3 と同じもの (golden)
  - H の codex の敵対的レビュー (1 本): P2 2 件・P3 1 件を採用 (型の別名 `type bm = browseModel` と括弧の受け手のメソッドを
    H-2 が数えない・公開の変数の無名構造体のフィールドと括弧で包んだ型のフィールドを H-3 が数えない)。再現手順 4 つで red を確認。
    ビルドタグ・cgo・ファイル名の大小文字・canary の空振り・recvTypeName の変更は壊せなかった
- [x] 2026-10-17 A (glogx: doctor のテストを doctorview の中へ戻し、main へ頼む口を整える / doctorview の公開名を main へ頼む口だけに絞る):
  - 外部テスト (`*_ext_test.go`) を package doctorview の中へ統合した。doctortest は doctorview を import しない fake の置き場にし
    (import の循環を断つ)、doctorview の型を使う部品は doctorview の `testhelpers_test.go` へ。差し替え口は `Deps` 1 つ
    (doctortest.Deps は同じ形の構造体で、変換して渡す。形がずれるとコンパイルで落ちる)
  - main へ頼む口: `Receive` (非同期の結果 6 種を 1 つで受ける。tui.go の型の switch 6 本が消えた)・`StartupToast`・
    `RestoreScreen` / `RememberScreen`・`InterruptDelete`。main の配線テストが要る状態は `Fixture()` の口だけ
    (Show / Delete / Reports / Rows / CursorIndex など)。本番のコードからの `.Fixture` と `Fixture{}` の直接構築は
    `TestNoFixtureCallsInProduction` が止める。**この形 (画面ごとの Fixture) を他の画面にも使う** (0-2 の決定に足す)
  - doctor だけで閉じる main のテスト (untrusted_display の doctor の 10 本・clock_rollback の小テスト・RememberScreen・latch の型の pin)
    を doctorview へ移した。テスト名 1,244 → 1,246 (増えたのは分割した 1 本と新しい検査 1 本)
  - 公開名 163 個を gopls rename で非公開に戻した (先頭の小文字化だけ = 742 の前の名前)。golden の doctorview は 231 行 → 95 行
    (うち 21 行は JSON に載る非公開の型のフィールド)。A-5: JSON に載る 5 つの構造体のフィールド名とタグが前後で同じことを比べた
  - A-4: doctortest の公開名 15 個はどれも doctortest の外 (main か doctorview の中のテスト) から使われている
  - 外れかけた検査: src/doctor の derived_fields_bypass_test が glogx のフィールド名を覚えている (`DiskRep` / `R` / `Rep` を
    小文字に戻した)。`Deps.Disk` (関数の型) が snapshot の `Disk` と名前で衝突して同じ検査が落ちたので、Deps のフィールドは元の
    差し替え口の名前 (DiskOpts など) にした。非公開に戻した名前は staticcheck の U1000 (-tests=false) と unparam の検査に戻る
  - 変異で red を確認: SSD の世代の照合を外す・武装を飛ばす・cancelAll の Stop を外す・寄せのトーストを外す・Update の Receive を外す
  - codex の敵対的レビュー (1 本): 本番の挙動の不変性は壊せなかった。P2 (Fixture.Delete が進捗を「既知」で置いて、進捗の届く前の
    削除中を見落とす) と P3 (`Fixture{v}` の直接構築が検査を通る) を直し、変異で red を確認
  - 踏んだ罠 (道具): 改名の位置を先に全部取ってから当てると、名前の長さが変わる改名の後の位置がずれて別の識別子を書き換える。
    gopls rename は改名ごとに位置を取り直す。改名の長さが変わらない (大文字・小文字だけ) なら位置はずれない
- [x] 2026-10-17 B (glogx: term.HintItem を hintfit.Item の別名にし、テストだけが使う変換を消す): term の公開名のうち外からテストだけが
  使っていたのは HintfitItems だけで、HintItem を hintfit.Item の別名にして変換ごと消した。app.ScreenMemory のフィールドは各画面が
  パッケージ変数として宣言する形なので公開のまま (理由と再評価の条件をコードに書いた)
- [x] 2026-10-17 C-1 (glogx: issues 画面を出す下準備として、共有の道具を app と extcmd へ出す): issues が main から借りていた 24 個を
  app (通知の種類・小さな道具・WatchChain と watcher の seam) と extcmd (新設。エディタ・ブラウザ・クリップボードの口と結果の Msg) へ。
  notice_kind_test (識別子の名前で照合)・waitdelay_discipline_test (Cmd を作るファイルの許可表) を直した
- [x] 2026-10-17 C-2〜C-4 (glogx: issuesview の公開名を main へ頼む口だけに絞る / 本番のコードから Fixture… のパッケージの関数を呼ぶ形も止める ほか):
  画面を internal/issuesview へ移し、main のテストは Fixture の口 (fixture.go。`//go:build !nofixture`) へ書き換えた。
  本番のコードから Fixture を呼ぶ形は TestNoFixtureCallsInProduction、各テストパッケージの TestMain が testenv.Main を呼ぶことは
  TestEveryTestPackageUsesTestenv が止める。到達不能の検査は -tags=nofixture で回す。opus の反証レビューを 2 体 (挙動 / 検査の素通り) 通した
- [x] 2026-10-18 D (glogx: status 画面を internal/statusview へ移し、公開名を main へ頼む口だけに絞る): golden は statusview 39 行
- [x] 2026-10-18 E (glogx: usage と ratelimit の画面を internal/usageview / internal/ratelimitview へ移す)
- [x] 2026-10-18 F (glogx: treefiler (F) と mac-lan-manager (L) の画面を internal/filerview / internal/lanview へ移す):
  前景実行は画面の TakeExecCmd / ExecReturned、終了時の記憶は RememberScreen に寄せ、tui.go の ExecPending の手組みを消した。
  テスト名 1252 件は前後で一致。変異: lanview.TakeExecCmd から execPending = true を外すと TestLanBindsBeatCrossKeys が red

## 引き継ぎ (2026-10-18。別マシンで続ける)

残り (上から順に):
- [x] D / E / F の反証レビュー (2026-10-18。codex の 5h 枠が尽きていたので opus 2 体、観点は ①挙動 ②検査の素通り):
  ① 壊せなかった (前景実行の判定順 filer → lan・RememberScreen の順と中身・記憶の JSON 名・ChangedMsg.Open・Viewport を旧コードと並べて比べた)。
  ② P1 なし。P2「TestMain の前処理の中身は検査されていない」は変異で裏を取った: main の lan の前処理を外すと Lan のテスト 8 本が red
  (守られている)。lanview の中の前処理を外しても緑で本物のコマンドも起きない (中のテストは L を開かない。今は効いていない保険として残す)。
  なお test_no_real_commands_in_tests.sh は subset の 358 件しか回さず、lan の前処理を外した変異でも緑だった (lan のテストが subset の外)。
  P3「build tag の無い fixture.go が検査から丸ごと外れる」は直した: TestNoFixtureCallsInProduction が fixture.go に
  //go:build !nofixture を要求する (lanview の fixture.go から tag を消すと red)
  C までは opus 2 体で通した。D〜F はまだ誰も攻めていない
- [x] ratelimitview の SaveRatelimitScreen / RemoveRatelimitScreen を Fixture の関数 (FixtureSaveScreen / FixtureRemoveScreen) へ寄せて非公開にした (2026-10-18)
- [x] G-1 / G-2 / G-3 (上の節。2026-10-18 に済ませた)
  2026-10-18 に G-1 / G-2 の判断までは済ませた (下の「G の判断」節)。実装は未着手

### G の判断 (2026-10-18。xref で数えた。移設の実装は未着手)

- **G-1: gitlog.go / github.go / cache.go は `internal/gitdata` へ出す**。3 つとも browseModel のメソッドを 0 個しか持たず、
  main のほかの部分への依存は 7 個 (options.go の Options とそのフィールド Revs / Paths / MaxCount / Patch / Stat、render.go の StateLoading) だけ。
  逆に main からは 77 個使われる (Commit / CIState / PRStatus などのデータ型と取得の関数。ほとんどは今も大文字)。
  先に設計が要るもの 2 つ:
  1. ✅ 2026-10-18 済み (glogx: git log の取得の関数が CLI の Options でなく LogQuery を受け取る)。`LoadCommits` などが CLI の `Options` を丸ごと受け取っている → gitdata 側に取得の条件だけを持つ型 (仮名 LogQuery) を置き、
     main が Options から組んで渡す。StateLoading (表示専用の擬似状態) は render.go に残すか、CIState の定数として gitdata へ移す
  2. 一部済み 2026-10-18 (glogx: 一括取得の「丸めてから割る」を planCIChunks 1 つに寄せる): tui.go と FetchCIStatuses は planCIChunks だけを通す。
     チャンクごとの Cmd は残す (結果をチャンク単位で先に描くための意図的な形)。残り: fillUnknownFetched / formatDuration / lastLine の口を決めて gitdata へ移す。
     元の記述: CI の分割取得の中身 (capFetchSHAs / chunkSHAs / fetchCIChunk / fetchMaxSHAs / fillUnknownFetched / formatDuration / lastLine) を
     tui.go と external_commands.go が直接組んでいる → 「SHA の列を渡すと分割して取得し、取れなかったものを埋める」1 つの口 (仮名 FetchCIBatch) へ寄せてから移す。
     機械的に大文字にしない (C 以降と同じ作法)
- **G-2: diff / job_detail / pr_status の overlay は G-1 の後に移せる** (依存は gitdata の型・tui.go の Msg (diffMsg / jobDetailMsg) と
  render.go の decorateDetailLine / prStateColor だけ)。**action_modal.go は main に残す**: pull / push / claude・codex の update を起こし、
  その結果の Msg を受ける配線そのもので、external_commands.go と claude_version.go / codex_version.go に 14 個頼る (移すと main への頼みが公開名に化けるだけ)
- [ ] 全部済んだら `scripts/issue_done.sh 817` と retro

検証の手順 (各 commit の前): `src/glogx` で `go test -race ./...` / `make lint` / repo root で `scripts/check_unused_excluding_tests.sh` /
`bash tests/glogx/test_no_real_commands_in_tests.sh`、テスト名の集合 (`go test -list '.*' ./...`) を移設の前後で比べる。
公開名の golden は `GLOGX_API_UPDATE=1 go test -run TestInternalAPIGolden .` で作り直し、増えた名前が main から頼む口だけかを見てから commit する。
罠: gopls rename は長さが変わる名前を 1 つずつ (位置がずれる)。外のパッケージが使う名前は gopls が拒むので、main 側を先に書き換えてから非公開にする。
JSON のタグ付きのフィールドは小文字にしない。unparam (check-exported) は他パッケージの利用を数えないので `//nolint:unparam // 理由` で受ける。
- [x] 2026-10-18 G-1 (glogx: git log と CI の取得を internal/gitdata へ移す): 準備 2 つ (LogQuery / planCIChunks) の後に gitlog.go / github.go / cache.go と
  中だけで閉じるテスト 69 宣言を移した。main のテストだけが使っていた 7 個 (chunkSHAs / runGit / fingerprintFormat / buildLogArgs / parseLog /
  parseGitHubURL / buildDisplayArgs) は非公開に戻した (main のテストは PlanCIChunks・extcmd.RunGitTimeout を使う)。golden の gitdata は 114 行
  (データ型とフィールドが大半)。EmptyBatch は main のテストだけが使うが、空の map を持つ CIBatch を作る口として残した。
  Options → LogQuery の写しは main の TestOptionsLogQueryCarriesFetchFields が守る (テスト名は移設前 +1 の 1253)
- [x] 2026-10-18 G-2 の 1 段目 (glogx: diff / job 詳細 / PR 状態の overlay を internal/commitview へ移す): 3 つともコミット 1 件の詳細を重ねる
  overlay なので 1 パッケージにした。render.go と共有する decorateDetailLine / prStateColor と、tui.go の diffMsg / jobDetailMsg も一緒に移した。
  テスト名は前後で一致 (1253)。**公開名はまだ絞っていない** (golden 53 行。フィールド Cache / Pager / Sha / Open / Busy と Msg のフィールドが公開)。
  main のテストがフィールドを 100 か所以上、tui.go が 20 か所ほど直接触るので、C と同じく Fixture の口へ書き換えてから非公開に戻すのが 2 段目 (未着手)。
  action_modal.go は main に残す (上の「G の判断」)
- [x] 2026-10-18 G-2 の 2 段目 (glogx: commitview の公開名を main へ頼む口だけに絞る): overlay と Msg のフィールド 15 個を非公開にした。
  本番の口は SHA() / NewDiffMsg / NewJobDetailMsg / JobDetailMsg の Key()・Lines()・Err()。main のテストは overlay ごとの Fixture
  (DiffFixture / JobDetailFixture / PrStatusFixture。フィールドへのポインタを返す) を使う。golden は 61 行 (フィールドは 0)。
  テスト名は前後で一致 (1253)。変異: JobDetailMsg.Key が "" を返すと TestBrowseCopyJobContext… の 2 本が red
- [x] 2026-10-18 G-3 (glogx: main に残すものを確定させ、CLAUDE.md・README・許可表の理由を直す): main の非テストのファイルは 19 本で、
  どれも起動・配線・browseModel の状態機械 (tui.go / fullscreen.go / hint_surfaces.go / gitlog_watch.go / open_workspace.go) か、
  起動時の配線 (probe / autobuild / ime / cli_health / claude_version / codex_version / external_commands / action_modal / zoom / render)。
  structure_ratchet_test.go の一覧に残っていた「C-1 で extcmd へ」「G-2 で置き場所を決める」の仮の理由を確定の理由に直した
- [x] 2026-10-18 G-2 の 2 段目の漏れ (glogx: commitview の fixture.go を入れ忘れていたので足す): 新規の fixture.go を git add せずに
  pathspec で commit し、master の main のテストが 1 commit の間ビルドできなかった。G-3 の作業で新しい worktree を切って気づいた

