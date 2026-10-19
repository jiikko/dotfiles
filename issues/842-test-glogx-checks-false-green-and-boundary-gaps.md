# 842 (test): glogx の構造検査の素通りと、テストが守っていない境界 (変異で確認)

> 🚨 **担当中: air5 (glogx)**（2026-10-19〜）

起票日: 2026-10-19

## 概要

2026-10-19 の glogx 全体監査 (844) で、(A) 検査・gate が名指しする退行を当てても緑のまま通る形 (false-green) と、
(B) 本番の境界を壊す変異が `go test ./...` 全体で緑のまま通る形を、`mutate-verify` で確かめたもの。テストは削除していない。

## 詳細

### A. 検査・gate の素通り (false-green)

1. **`TestOwnStringLiteralsHaveNoVS16` がエスケープ表記を見ない** (F5)。`vs16_literal_test.go` は `lit.Value` (ソースの字面) だけを見るので、`"x️"` のようなエスケープ表記と rune リテラル (`token.CHAR`) は掛からない
   - 変異: `render.go` に `const vs16EscapeProbe = "x️"` を足すと緑 (rc=6)。対照の生の U+FE0F は赤。走査 254 ファイル、今の違反は 0
   - 推奨: `strconv.Unquote` した値で判定し、CHAR リテラルも見る
2. **`TestSetNoticeKindsFollowWarningClassification` が手で並べた 3 ファイルしか読まない** (F5)。対象は `issues_view.go` / `issues_linkjump.go` / `status_view.go`
   - 変異: `internal/issuesview/issues_watch.go` にエラーを `NoticeRefused` で出す `setNotice` を足すと緑 (rc=6)。同じ行を `issues_view.go` に足すと赤。今の `setNotice` 39 か所はすべて 3 ファイルの中
   - 推奨: `setNotice` を定義する package の非テストファイルを全部走査する
3. **`make test-budget` がテストの改名で「no tests to run」の緑になる** (F4)。`src/glogx/Makefile` の `go test -count=1 -run '^TestFrameAllocBudget$' .`。確保回数の予算は -race の run で判定を飛ばすので、判定しているのは CI のこの 1 本だけ
   - 実測: 一致しない名前で同じコマンドを打つと `ok glogx [no tests to run]` rc=0
   - 推奨: `-v` の出力から `--- PASS: TestFrameAllocBudget` を grep し、無ければ落とす
   - 同じ step は CI で約 10.6 秒かかる (テスト本体は 0.168 秒。直前の `-race` と別の build cache になり非 race のビルドをやり直す。run 39187832311 の 02:04:22 → 02:04:32)。直すかは任意
4. **`clock_rollback_test.go` は名前が TTL / StaleAfter / Cooldown の定数と比べる関数しか見ない** (F5)。`usageview.UsageOverlay.Stale()` (`app.UsageRefreshInterval` と比較) は対象外のまま緑。ただし `Stale` は monotonic clock で比べるので本番の害は無い (840 の 9 で却下)。検査の範囲の狭さとしてだけ残す
5. **CI の部分成功のテストの fake が本物の gh と違う** (F5)。`internal/gitdata/github_test.go` の `TestFetchCIStatusesPartialErrors` の fake は err=nil で `"errors"` 入りの JSON を返すが、gh は `data` と `errors` を同時に返すとき rc≠0 になる (本番側は 840 の 6)。fake にも本物と同じ rc を返させる
6. **AST の走査の下限が実件数から遠い** (F5)。`cache_write_discipline_test.go` は下限 40 に実測 97、`details_flags_test.go` は 40 に 97、`internal/issuesview/issues_rows_setter_test.go` は 20 に 29。対象がかなり縮んでも緑。`clock_rollback_test.go` は 8 に 8 で、方針どおり (足場の共通化は 843 の 2)
7. **環境 skip が CI の出力から見えない** (F4。予防)。`t.Skip` が 25 行 (fsnotify を作れない 7・git が無い 4・symlink など)。手元では skip 0 件だが、CI は `-v` 無しなので skip に化けても見えない (087 の再発の形)。`go test -json` の skip 件数を照合するか、`t.Skip*` を許可リストで固定する

### B. 境界を壊しても `./...` が緑のまま (変異は比較演算子を 1 つ反転)

1. **行数の上限ちょうど** (P2): `internal/gitdata/gitlog.go` の `capGitBody` (`<=` → `<`。ちょうど 5000 行で、何も省略していないのに「省略した」が付く)、`LoadCommitDiff` (`>=` → `>`)、`internal/statusview/status_view.go` の未追跡ファイルのプレビュー (`>=` → `>`)、`worktree_status.go` の `diffLines` (`>=` → `>`)。`TestParseLogCapsHugeBody` は `MaxDiffLines+100` 行の入力だけ。各実装に「ちょうど上限 / 上限 + 1」の 2 ケースを足す
2. **status viewer のヘッダ** (P2): `headerLine` の `if conflicted > 0` を `if false` に、件数の `unstaged+untracked, staged` を入れ替えても緑。ヘッダの文言を assert するテストが 0 件。conflict あり / 無し / 混在の 3 ケースを足す
3. **`gitdata.FormatDuration` の 1 時間ちょうど** (P3): `d < time.Hour` → `<=` で緑 (1h が `60m00s` になる)
4. **`app.ClampIdx` の空** (P3): `total <= 0` → `< 0` で緑 (`ClampIdx(i, 0)` が -1)。`internal/app` には自前のテストが 1 本も無い
5. **`worktree_status.go` の末尾で切れた rename レコード** (P3): `if i+1 < len(fields) ...` → `<=` で緑。panic になる変異だが、元パスの無い R/C レコードが最後に来る入力のテストが無い (panic はコードから判断。実行していない)
6. **`issues/parse.go` の `isAllDigits`** (P3。根拠は弱い): `s[i] < '0'` → `<=` で緑。'0' を含む数字だけのトークンの入力が無い

### C. 重複・古い前提 (P3)

1. 画面の記憶の拒否規則 (期限切れ・未来・時刻なし・壊れた JSON) を 5 画面のテストが重複して検査し、規則を持つ `internal/app/screen_memory.go` の `ScreenMemory.Load` 自体のテストは無い。`|| at.After(now)` を消すと 5 本が同時に赤。規則のテストを `internal/app` に 1 本置き、各画面には固有の照合と `SavedAt` の配線の 1 ケースを残す (消すと配線の誤りが見えなくなるので、各画面の 1 ケースは残す)
2. `cli_health_test.go` の `TestCheckCLIHealthSignalExitWithOutputIsUnknown` は期限切れ ctx で呼ぶので、signal 終了の処理 (`commandExitCode` の `exitCode < 0`) に届かない。`if exitCode < 0` を `if false` にしても緑 (落ちたのは `TestCommandExitCodeSignalIsUnknown` だけ)。期限切れでない ctx で signal 終了 + 出力ありを渡す形に直す
3. issues viewer の見張りのテストの「CI (ubuntu-slim) では fsnotify が skip される」という注記が 4 か所残る (`issues_watch_seam_test.go` 2・`issues_watch_test.go` 2)。CI は `macos-15` (`.github/workflows/src_glogx.yml`) で実物版も走るので、seam 版と実物版が同じ不変条件を 2 本で検査している。注記を直し、seam 版を残す理由 (fs のタイミングに依存しない) を書くか一方を畳む
4. `issues/audit698_test.go` は監査番号で名付けた雑多なテストの置き場 (中身は parse / body のテスト)。`issues/parse_test.go` の `TestScanRealRepoIssuesDirIfPresent` は実 repo の issues/ に依存する smoke (10 件を割ると偽の赤。`issues/**` は CI の paths に無い)

## 裏取りの範囲

- 変異は約 150 本 (比較演算子の反転。internal の term / gitdata / statusview / usageview / ratelimitview / commitview / app と issues)。緑のまま通ったのは約 22 本で、等価変異と判断したもの (`OrderLess` の `>=`・`pruneToLimit` など) は除いた
- 約 20 本は途中で他のセッションが元の repo を変えて判定不能 (rc=9) のまま (`status_view.go` の後半・`worktree_status.go` の前半・`commitview`・issues の `discover`)。**未確認**

## 進捗

- [x] A1 〜 A7 (2026-10-19。commit「glogx: 全体監査 (844) の issue 834〜843 を直す」)
  - A1 VS16 の検査はリテラルの値 (`strconv.Unquote`) で見て、rune リテラルも対象 (変異: `"x\uFE0F"` を足すと red)
  - A2 通知の種類の検査は glogx の本番の .go を全部走査する。成功 (NoticeOK) もエラーの詳細を含めば誤分類
  - A3 `make test-budget` は `-v` の出力に `--- PASS: TestFrameAllocBudget ` が無ければ落ちる (改名で `[no tests to run]` の緑にならない。手で確かめた)
  - A4 記録だけ (Stale は monotonic の時計で比べるので本番の害は無い。issue の判断のまま)
  - A5 は 840 の 6 で済んでいた (fake は本物の gh と同じ rc≠0)
  - A6 AST の走査の下限を実件数のすぐ下へ (cache_write 85 / 実測 92・details_flags 85 / 92・rows の setter 25 / 29)。走査の足場は 843 の 2 で 1 つにした
  - A7 環境の前提が無いときの skip を `testenv.SkipEnv` に寄せ、CI (`CI` が立つ) では落とす。`t.Skip` を直接呼んでよいテストは `skipAllowlist` (ファイル:宣言の名前と理由。var の塊の中も 1 つずつの名前で引く) に固定 (`TestSkipsAreAllowlisted`)。幅を固定しているのに skip していた doctor のテストは Fatal にした。CI=1 で全体を回して落ちるものが無いことを確かめた
- [x] B1 〜 B6: 上限ちょうど / + 1 のテスト (`DiffDisplayLines`・`capGitBody`・未追跡ファイルのプレビュー)・1 分 / 1 時間ちょうど・空の `ClampIdx` (internal/app に初めてのテスト)・末尾で切れた rename (NUL も無い入力)・数字の端。B2 は 838 で済んでいた
- [x] C1 〜 C4
  - C1 `ScreenMemory.Load` の規則のテストを internal/app に置いた (TTL の手前・ちょうど・超過・未来・時刻なし・壊れた JSON)。各画面のテストは SavedAt の配線を兼ねるので残した (消すと配線の誤りが見えなくなる)
  - C2 signal 終了のテストは期限の切れていない ctx で呼ぶ (変異: `exitCode < 0` を外すと red)
  - C3 「CI (ubuntu-slim) では fsnotify が skip される」の注記 5 か所を、今の前提 (CI は macOS・seam 版はイベントの届き方に依存しない) に直した
  - C4 `issues/audit698_test.go` の 6 本を parse / body / banner のテストへ移した。実 repo の issues/ を読む smoke は残した (CI には issues/ が在る。切り出したときだけ skip)
- 変異: 13 本 + 当て直し 2 本がすべて red
- レビュー: codex の実装レビュー 3 周 + opus のサブエージェント 1 周 (codex の週の枠が切れた後)
