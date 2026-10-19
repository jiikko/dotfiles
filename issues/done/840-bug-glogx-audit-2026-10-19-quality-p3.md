# 840 (bug): glogx 全体監査の品質系 P3 (gh -F・autobuild の panic・claim 外しの部分失敗・画面の記憶の共有など)

起票日: 2026-10-19

## 概要

2026-10-19 の glogx 全体監査 (844) の品質系 (security / resource-leaks / broken-code / error-handling / concurrency / performance) のうち、
単独の issue にするほど重くない P3 を集める。1 項目ずつ独立に直せる。

## 詳細

1. **gh api graphql に owner / name を型変換つきの `-F` で渡す** (broken-code)
   - 該当: `src/glogx/internal/gitdata/github.go` の CI の GraphQL を投げる 2 か所 (`"-F", "owner="+repo.Owner, "-F", "name="+repo.Name`)
   - `gh api --help` (v2.92.0) によると `-F` は整数・`true` / `false` / `null` を JSON の型に変え、`@` で始まる値をファイルとして読む。repo 名が `2048` のような数字だと `$name: String!` に整数が渡り、CI が取れないはず。**API の応答は未実測** (help からの推論)
   - 推奨: `-f` (raw-field) で渡す
2. **`autobuildReplaceDirs` が go.mod の `=>` で終わる行で panic する** (broken-code)
   - 該当: `src/glogx/autobuild.go` の `strings.Fields(target + " ")[0]`。起動経路 (`autobuildStaleBinary`) で呼ばれる
   - 監査の再現で `index out of range [0] with length 0` を確認。コメントアウトした `// replace x => ../y` も走査する木として拾う
   - 推奨: コメント行を飛ばし、Fields の要素数を見てから添字を使う
3. **`issues.MoveToSubdir` の claim 外しの部分失敗** (error-handling)
   - 該当: `src/glogx/issues/move.go` の `subdir == "" && iss.NextLink != ""` の枝。先にバナーを外し、その後 `removeNextLink` が失敗するとバナーを戻さず `(iss.Path, err)` を返す。doc の「失敗は何も変わっていない」と、二重の失敗では "" を返す約束の両方に反する。結果は目印が残りバナーだけが消えた claim (CI のバナー検査が落ちる)
   - 発火条件: 走査の後に `next/<base>` が別の形へ差し替わる (git pull) か、Remove の権限エラー。読んで確認した
4. **`cmd/issue-claim` が issue のパスとエラーを無害化せずに出す** (security)
   - 該当: `src/glogx/cmd/issue-claim/main.go` の `run`。`iss.Rel` とパスを含む err を stdout / stderr へそのまま出す。`src/glogx/CLAUDE.md` の「stdout へ直接書く CLI こそ termsafe の関門が要る」に反する。ファイル名の ESC が端末に出る (実機では**未確認**)
   - 推奨: **制御文字を含むパスは変更の前に拒否する** (stdout を表示用に無害化する案は採らない: `scripts/issue_claim.sh` が issue-claim の stdout を実パスとして `git add` に渡すので、書き換えると実体との対応が壊れる。codex の反証レビューの指摘)。stderr の表示は無害化してよい
5. **`issues.LoadMeta` / `ReadBody` が開くときに symlink と非通常ファイルを見直さない** (security / 未確認リスク)
   - 該当: `src/glogx/issues/parse.go`。走査では `isIssueFile` で symlink と FIFO を弾くが、開く `os.Open` / `os.ReadFile` は symlink を辿り通常ファイルかを見ない。走査から開くまでに差し替わると (viewer を開いたまま git checkout し、見張りが反映する前に Enter)、リンク先を表示する。FIFO なら戻らない。窓が狭い
   - 推奨: `O_NOFOLLOW|O_NONBLOCK` で開いて `f.Stat()` で通常ファイルか確かめる
6. **CI の GraphQL の部分成功が、本番では全部 `?` に落ちる** (error-handling)
   - 該当: `src/glogx/internal/gitdata/github.go` の `FetchCIChunk` (err があると stdout を捨てて `EmptyBatch()`)、`parseStatusResponse` の部分成功の枝
   - 監査で gh 2.92.0 に `data` と `errors` を同時に返す GraphQL (存在しない repo を alias で混ぜる) を投げ、stdout に data 付きの JSON が出ることを確認した (記録の stderr は `gh: Could not resolve ...`。rc=1 は報告者の申告で、記録ファイルに rc は残っていない)。rc≠0 なら 1 つの alias のエラーでそのチャンクの SHA が全部 `?` になり、30 秒の負キャッシュにも載る。害は `?` に落ちる向き
   - 実験は本番と違う形のクエリ (repository の alias) で、本番の `object(oid:)` の alias で errors が付く条件は確かめていない
   - テスト側 (fake が rc=0 を返す) は 842 の A5。`fetchAssociatedPRs` / `FetchPRStatus` が同じ形かは**未確認**
   - 推奨: err があっても stdout を読むが、`errors[].path` と alias を対応付け、エラーの alias は「取得失敗」にする。`parseStatusResponse` は alias の `null` を `StateNone` (Check なし) にするので、単に stdout を通すとエラーの alias を「Check なし」と誤表示する (codex の反証レビューの指摘)
7. **却下: 別の glogx の終了が全画面の記憶を消す** (concurrency K4 として起票したが、明記された設計)
   - 挙動は再現する: repo X の glogx を issues viewer のまま C-g で閉じる → repo Y の glogx を一覧から q で終える → X の記憶が消える
   - ただし `src/glogx/internal/issuesview/issues_state.go` の冒頭のコメントが、repo ごとに持たない理由 (起動時に repo root の解決の fork を増やさない) と代償「repo A → repo B と開くと A の記憶が消える」を明記している。`internal/filerview/filer_resume.go` も同じ場面を代償として書いている。未認識の不具合ではない
   - 起票時の「全体で 1 ファイル」も誤り: ファイルは画面の種類ごとに 1 本。doctor・R・L は Root を持たない (場所に依存しない) ので、「Root が自分の repo のときだけ Remove」は共通の直し方にならない。662 は treefiler の設定の上書きで別の仕組み (codex の反証レビューの指摘)
   - 再提起の条件: 複数の repo で glogx を並べて使い、記憶が消えることを実害として観測したとき (起動のコストと比べて判断し直す)
8. **`usageview.loadUsageShape` がキャッシュの codex の `Label` を無害化しない** (security / 未確認)
   - 一般ユーザー権限で書き換えられるキャッシュの `Label` を termsafe に通さず、取得中の場所取り行で `usage.RenderTableGroups` がそのまま描く。`loadUsageCache` は通している。色付きの描画は SGR を残すので、描画層が全部落とすとは言えない (codex の反証レビュー)。実害の有無は**未確認**
9. **却下: `UsageOverlay.Stale()` に時計の巻き戻しのガードが無い**
   - `fetchedAt` は本番では `app.TimeNow()` (= `time.Now`) で設定され、`Stale` の `Sub` は両方の monotonic clock を使うので、壁時計が戻っても発火しない。監査の再現は fixture で壁時計だけの値を置いたもので、本番の根拠にならない (codex の反証レビューの指摘)。842 の A4 (clock_rollback_test の対象外) も同じ理由で実害の根拠にはならない
   - 再提起の条件: `fetchedAt` を永続化した値 (キャッシュの時刻など) から設定する経路ができたとき
10. **job のログ全体をメモリに溜めてから末尾 50 行を取る** (performance。見積もり・**未実測**)
    - `gitdata.fetchJobLogTail` / `ExecRunner` がログ全体を読んで `bytes.Split` で全行に分ける。走るのは job の詳細を開いたときの先読みで、`internal/commitview/job_detail_overlay.go` の `StartOpen` はキャッシュに当たれば取り直さない (起票時の「開くたび」は誤り)
    - `bytes.Split` は本文を行ごとにコピーしないので、増えるのはログ全体の buffer と行数分の slice header。何倍になるかは行数・平均の行長・buffer の容量しだいで、見積もっていない

## 関連ファイル

項目ごとに上に書いた。

## 進捗

- [x] 1 〜 6・8・10 (2026-10-19。commit「glogx: 全体監査 (844) の issue 834〜843 を直す」)
  - 1 GraphQL の owner / name を `-f` (文字列) で渡す
  - 2 `autobuildReplaceDirs`: コメントを外し、空のフィールドで panic しない
  - 3 claim の解除: 目印 → バナーの順で外し、バナーを外せなければ目印を戻す。目印が元から無かったときは「戻した」と言わない。done への移動は目印を外せなければ動かさない (`TestMoveToDoneRefusesBeforeMovingWhenLinkCannotBeRemoved`)。バナーの書き換えは `openIssueFile` で読む (FIFO に差し替わっていても止まらない)
  - 4 `cmd/issue-claim`: 制御文字を含むパスは claim しない・stderr は無害化 (無いディレクトリの名前も)
  - 5 `openIssueFile` (O_NOFOLLOW|O_NONBLOCK・開いた fd が通常のファイルか)
  - 6 CI の部分成功: 失敗しても stdout を読み、`errors[].path` の alias だけを捨てる。fake は本物の gh と同じ rc≠0
  - 8 usage のキャッシュの無害化を `readUsageCache` へ
  - 10 `logTail` を後ろから走査 (実測: 2.06 ms / 4.8 MB → 約 22 µs / 16.9 KB)
- 変異: 23 本 + 指摘対応の分がすべて red
- レビュー: Claude の 2 体のレビュー・codex の実装レビュー 2 周
- 記録だけ (再現していない・脅威として薄い。理由と再提起の条件はコードの直近): `removeNextLink` の取り直しから削除までの窓・`openIssueFile` は親のディレクトリの symlink を見ない・2 つのプロセスが同じ issue の解除と claim を同時に行う形 (CI のバナーの検査が落とす)
- [x] 7・9 は反証レビューで却下 (理由と再提起の条件は各項目)
