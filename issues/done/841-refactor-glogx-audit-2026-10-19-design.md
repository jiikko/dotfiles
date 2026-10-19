# 841 (refactor): glogx 全体監査の設計系の発見 (WatchChain の世代・diff 整形の 2 重・gitdata の依存方向など)

起票日: 2026-10-19

## 概要

2026-10-19 の glogx 全体監査 (844) の設計系 7 タイプの発見のうち、今は正しく動くが「新しい経路・種別が増えたら黙って壊れる」構造。
今起きている誤表示 (doctor の `y`) は 839 に分けた。各項目は 1 つの型に 1 回だけ帰属させた。どれも silent に壊れる (compile error では止まらない)。

## 詳細

1. **`app.WatchChain` の札と世代を呼び出し側が手で書く** (encapsulation / E2)
   - 該当: `src/glogx/internal/app/watch_chain.go` の `WatchChain` (`W` / `Gen` / `EvArmed` / `PollArmed` を公開)、`gitlog_watch.go` の `stopGitLogWatch` / `handleGitLogProbe`、`internal/issuesview/issues_watch.go` の `StopWatch`
   - 「閉じて世代を進める」を `app.WatchChain{Gen: x.Gen + 1}` で 2 か所が手組みし、git log の見張りは死んだ watcher のとき `Gen++` → `PollArmed = false` をフィールドへ直接書く
   - 守っている不変条件「`PollArmed` を降ろすのは世代を進めたときだけ」(進めずに降ろすと旧い tick と新しい tick が同じ世代で届き、ポーリングが二重になる) を型が持っていない。doc のメソッド名も公開前の小文字のまま
   - 発火条件: 3 つ目の見張りが git log 側の分岐を写して `Gen++` を落とす
   - 推奨: `Reset()` (閉じる + 世代 + 札) と `DropDeadWatcher(advanceGen bool)` を置き、フィールドを非公開にする。issues viewer が世代を進めないのは意図した設計 (`HandleWatch` のコメント)
2. **diff 本文の整形 (行数の上限 + 無害化 + 色) が 2 か所** (duplication)
   - 該当: `src/glogx/internal/gitdata/gitlog.go` の `LoadCommitDiff` のループと `internal/statusview/worktree_status.go` の `diffLines`。`diffLines` の doc は「LoadCommitDiff と同じ整形なので揃える」とコメントで同期を取っている。省略の文言は既に違う (`truncatedNotice(sha)` と `"... (これ以降は省略)"`)
   - 境界 (ちょうど上限) が両方とも未検査なことは 842 に書いた
   - 推奨: `gitdata` に `DiffDisplayLines(out, colored, notice)` を置いて status も呼ぶ
3. **データ層の `internal/gitdata` が UI 側の package を経由して bubbletea と fsnotify に依存する** (leaky-abstraction / L1)
   - `gitdata` → `extcmd` (使うのは `RunGitCmd` / `RunGitTimeout` / `MaxDiffLines` だけ)、`gitdata` → `app` (使うのは `CacheBaseDir` / `WriteAtomic` / `FirstLine` だけ)。`extcmd` は `tea.Exec` と `tuikit/foreground` を、`app` は `WatchChain` のために fsnotify と bubbletea を import している。`go list -deps ./internal/gitdata` に `charm.land/bubbletea/v2`・`github.com/fsnotify/fsnotify`・`tuikit/foreground` が入ることを確認した
   - `MaxDiffLines` が外部コマンドの口の `extcmd` にあるのも置き場所の誤り (使うのは gitdata と statusview)
   - 発火条件: `watch_chain.go` や `extcmd.go` の 1 行の変更で gitdata とその上が全部作り直される / gitdata を UI 抜きで使う呼び出し元を作ると UI の依存を背負う。view どうしの import は `TestNoViewImportsAnotherView` が止めるが、データ層 → UI の道具の向きは誰も検査していない
   - 推奨: `extcmd/git.go` を末端の package へ出し、`MaxDiffLines` は gitdata へ。`app` の 3 関数も tea / fsnotify に依存しない package へ
4. **起動時の画面の復元で「Cmd が nil でない」を「開いた」の意味に使い、コメントも実装と食い違う** (leaky-abstraction / L5)
   - 該当: `src/glogx/tui.go` の `browseModel.Init` の `rlRestore` / `doctorRestore` / `filerRestore` / `lanRestore` の連鎖。`DoctorView.RestoreScreen` の bool は `doctorRestore, _ =` で捨てる。優先順位は `restore == nil && rlRestore == nil && ...` と条件を増やして書く
   - doctor の注記「issues の復元とは排他にしない…issues は下に残り、閉じれば出てくる」は誤り: `issuesRestoreMsg` のガードは `activeFullScreen()` が別の全画面なら復元を捨てる
   - issues の記憶があるだけで (repo の照合の前に) F と L の復元を譲るので、照合が外れると何も復元されない
   - 発火条件: 開いたのに nil の Cmd を返す画面が増える (全画面が 2 枚同時に出る) / 異常終了で記憶が 2 つ残る。今は開くと必ず非 nil が返るので発火しない
   - 推奨: 各段の後で `m.fullScreenActive()` を見る。doctor の注記を直す
5. **`EditorClosedMsg` が起動元を持たない** (encapsulation / E2)
   - 前景でプロセスを起こす箇所は 7 か所 (`RunEditorCmd(` を grep)。戻ったときは filer と lan の `execPending` を順に問い、どちらでもなければ「エディタ」として `issuesOv.ReloadAfterEdit()` と「エディタが異常終了しました」へ流す
   - 発火条件: 新しい画面が前景で起動すると、その画面の読み直しは走らず issues viewer が読み直される。前景は同時に 1 つなので今は正しい
   - 推奨: 起動側が自分の Msg で包むか、`EditorClosedMsg` に Origin を持たせて main が switch する
6. **画面から main への知らせの重さ (成功 / 断り / エラー) の決め方が 3 通り** (duplication)
   - issues / status / filer / lan は `app.NoticeKind` 付きで `deliverNotice` へ (663 の仕組み)、doctor は `TakeToast() string` で main が断りに固定、filer / lan の開いたときの失敗は `TakeOpenErr()` で main がエラーに固定
   - 発火条件: doctor が成功やエラーを出すようになると、エラーが `w` でコピーできないか成功が断りの色で出る。今の doctor の `pendingToast` 13 か所はすべて断り
   - 推奨: doctor も `TakeNotice() (string, app.NoticeKind)` に揃え、`TakeOpenErr` は `DrainNotices` に吸収する
7. **「codex が入っているか」の判定が usageview に 2 つ** (duplication。小さい)
   - `internal/usageview/usage_overlay.go` の `fetchCmdWith` (「判定は loadUsageCache と同じ」とコメントで同期) と `usage_cache.go` の `extcmd.LookPath("codex")`。`cli_health.go` の `checkCodexHealth` にも同じ問い合わせ
   - 推奨: usageview に `codexInstalled()` を 1 つ置く

## 既出 (再提出しない)

071 (pager 3 面の描画・actionModal の 7 本の bool・全画面のスライドの状態機械 2 コピー。未解消のまま)、224、237 C12、666 D / J、823、320。

## 進捗

- [x] 1 〜 7 (2026-10-19。commit「glogx: 全体監査 (844) の issue 834〜843 を直す」)
  - 1 `fswatch.Chain` (旧 `app.WatchChain`。下の 3 で移した) に `Advance()` (閉じて世代を進めた空の札) と `DropDeadWatcher(advanceGen)` (世代を進めるときは札も降ろす) を置き、git log と issues の見張りの手組みを置き換えた。フィールドは別 package のテストが書くので公開のまま、本番のコードが札と世代を書く形 (代入・++・`fswatch.Chain{...}`・Chain を埋め込む型を Chain なしで組み立てる) を `TestWatchChainStateWrittenOnlyByItsMethods` が落とす
  - 2 `MaxDiffLines` を gitdata へ移し、`gitdata.DiffDisplayLines` をコミットの diff と status viewer のプレビューが共有する
  - 3 git の実行を末端の `internal/gitexec` へ、見張りを `internal/fswatch` へ出した。`go list -deps ./internal/gitdata` から bubbletea・fsnotify・foreground が消えた。depguard の `data-layer-no-ui` (gitdata・gitexec・app・term の直接の import に bubbletea・fsnotify・foreground・extcmd・fswatch を禁じる) で固定。直接の import しか見ないので、gitdata が module 内の別の package を新しく import したらそこも足す (規則の注記)
  - 4 起動時の画面の復元 (`restoreScreensCmd`) は同期で開ける画面を優先順に試し、`fullScreenActive()` で開いたところで止める。issues は repo の照合が非同期なので最後に回し、記憶があるだけで F・L が譲らない。照合が着く前にキーが押されていたら復元を捨てる (`keyed`)。doctor の誤った注記は消した
  - 5 `EditorClosedMsg` に起動元 (`extcmd.ExecOrigin`: エディタ / F / L) を載せ、main が起動元で振り分ける。filer と lan の `execPending` は消した
  - 6 doctor の知らせに重さを持たせ (`refuse` / `TakeNotice`)、main は `deliverNotice` へ流す。filer・lan の開けなかった理由は `DrainNotices` がエラーとして出す (`TakeOpenErr` を消した)
  - 7 usageview の `codexInstalled()` を 1 つにした
- 変異: 札と世代の外し方・構造検査・depguard・復元の優先順位と割り込み・起動元の振り分け・知らせの重さなど 20 本超がすべて red
- レビュー: codex の実装レビュー 3 周 + opus のサブエージェント 1 周 (codex の週の枠が切れた後)
