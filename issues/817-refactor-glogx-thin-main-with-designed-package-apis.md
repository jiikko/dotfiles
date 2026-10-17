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

- [ ] A-1 0-1 の道具で doctorview の公開名を分類する (main の本体が使う / main のテストだけが使う / 外部テストだけが使う / 中だけ)
- [ ] A-2 main の本体が使う口を 0-2 の形の API にまとめ、それ以外を非公開に戻す
- [ ] A-3 main のテスト・外部テストが中身 (フィールド) を見ている箇所は、API 経由に書き換えるか、`package doctorview` の中のテストへ戻す
- [ ] A-4 doctortest の公開面も同じく絞る
- [ ] A-5 JSON に載る構造体 (doctor_cache.go / doctor_resume.go) のフィールド名を変えていないことを確かめる

### B. 土台 (term / app) の公開面

- [ ] B-1 term・app の公開名のうち、package の外から使われないもの (例: `term.HintfitItems` は外からはテストだけ。
      🚨 `term.BuildPanelBoxImpl` は zoom.go が本番で使うので、戻すなら呼び出し側の移設か代わりの API が要る) を非公開に戻す
- [ ] B-2 app の `ScreenMemory` などのフィールドを公開している理由を見直す

### C. issues 画面 (`internal/issuesview`)

- [ ] C-1 下準備 (742 で 2026-10-17 に数えた): url_picker は issues と一緒に、watch_chain は dirWatcher・newDirWatcher・drainWatchEvents と一緒に
      app へ (interface だけでは足りない)、editorCommand など 4 関数と runEditorCmd・editorClosedMsg・openURLMsg・openInBrowser・copyToClipboard は
      extcmd (新設) へ、notice の種類・firstLine・clampIdx・normalizeSpaceKey・clipboardFailText は app へ
- [ ] C-2 本体を移す。**機械的に先頭を大文字にせず**、main から使われる名前だけを 0-2 の形で公開する
- [ ] C-3 テストを置き分ける (中を見るテスト / 外部テスト / browseModel と組み合わせるテストは main)。共有のテスト部品は issuesviewtest へ
- [ ] C-4 移設が外す検査を洗う (742 の罠の一覧: 固定名で読むテスト・reflect・識別子名の照合・TestMain・実コマンドの監視の範囲・CI の paths)

### D. status 画面 (`internal/statusview`)

- [ ] D-1 status_view.go と worktree_status.go を移す (hintItem は term へ出し済み)。lineCache は diff / job の overlay とも共有するので app へ
- [ ] D-2 テストを置き分ける

### E. usage / ratelimit

- [ ] E-1 usage_overlay / usage_cache と ratelimit_dashboard / ratelimit_resume を移す (どちらも browseModel のメソッドを持たない。742 で数えた)
- [ ] E-2 テストを置き分ける

### F. filer / lan

- [ ] F-1 browseModel のメソッド (`quit` / `toggleUsage` / `deliverNotice` / `maybeTick` / `openFullScreen`) と tui.go の差し替え点
      (`currentDir` / `runEditorCmd`) への依存を、画面から main へ頼む口 (0-2) に置き換える
- [ ] F-2 移す・テストを置き分ける

### G. main に残すものを整理する

- [ ] G-1 git log の取得と CI の取得 (gitlog.go / github.go / cache.go) を `internal/gitdata` へ出すか決める (render.go のコミット行の描画が使う)
- [ ] G-2 overlay 類 (diff / job / pr_status / action_modal) の置き場所を決める
- [ ] G-3 main に残るのが「起動・配線・browseModel の状態機械」だけになったら、src/glogx/CLAUDE.md・README の構造の記述を直して閉じる

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
