# 199 test: glogx のテストで繰り返されているセットアップ 2 箇所をヘルパーへ抽出する

起票日: 2026-09-03
出典: `/audit` の test-helpers (direct、2026-09-03)
重要度: P3
関連: [`verify-design-intent-before-refactor.md`](../_claude/rules/verify-design-intent-before-refactor.md)
(複雑性が下がるかで判断する)

## 前提: この repo には既に共通ヘルパー基盤がある

`src/glogx/tui_helpers_test.go` (main 用 18 ヘルパー + `TestMain` のキャッシュ隔離)、
`issues/main_test.go`、`usage/main_test.go`。各ヘルパーには「なぜこの形か」「統合しては
いけない理由」がコメントで残っている。**下記はそこに載っていない残りの重複だけ**。

## 候補 1: `gitlog_watch_test.go` の 9 行プロローグ × 12 (抽出を推奨)

同一ファイル内で実測 (2026-09-03): `Options{MaxCount: 20, NoFrame: true}` が **12 回**、
`newBrowseModel(` が **12 回**、`newTempRepo(` が **14 回**。ブロックは毎回同一:

```go
dir := newTempRepo(t, []string{...})
opts := &Options{MaxCount: 20, NoFrame: true}
commits, err := LoadCommits(opts, false)
if err != nil { t.Fatal(err) }
m := newBrowseModel(commits, map[string]CIState{}, nil, Repo{}, false, opts, false, 80, 10)
t.Cleanup(m.cancel)
m.zoom.off = true
```

**抽出後の姿** (ファイルローカルに置く。`tui_helpers_test.go` へは上げない — 実 git repo に
依存するのはこのファイルだけで、共有語彙に見せると読み手を迷わせる):

```go
// realRepoBrowse は実 git repo (subjects のコミット付き) を作り、そこへ chdir した
// browseModel を返す。dir は追加コミット (commitLines) 用。
func realRepoBrowse(t *testing.T, subjects ...string) (m *browseModel, dir string, opts *Options)
```

9 行 → 1 行 × 12 で **約 96 行減** (756 → 約 660 行)。減るのは「repo を作って model を組む」
という前提の準備だけで、各テストの主張 (指紋・アンカー行・世代の弾き) は 1 行も動かない。

⚠️ **3 変数すべてを返すこと**。`dir` は `commitLines(t, dir, 9, "c5")`、`opts` は
`BuildFingerprintArgs(opts)` / `LoadCommits` で個別に使われている。1 つでも隠すと
呼び出し側が結局自前で組み直す。`newTempRepo` は `t.Chdir` の副作用を持つので**呼び出し順を変えない**。

## 候補 2: `doctor_view_test.go` の snapshot 書き込み × 8 (抽出を推奨)

`doctorSnapshotPath()` の出現が **8 回** (実測 2026-09-03)。うち 2 箇所は既にテスト内の
ローカルクロージャ (`write` / `writeSnapshot`) として自力で抽出されている = 書いた人自身が
重複に気づいてファイルスコープへ上げないまま留めた形跡。

```go
data, err := json.Marshal(sn)   // ← err を捨てる版と捨てない版が混在
path, _ := doctorSnapshotPath()
os.MkdirAll(filepath.Dir(path), 0o755)
os.WriteFile(path, data, 0o600)
```

**抽出後の姿**: `func writeDoctorSnapshot(t *testing.T, sn doctorSnapshot)`。
10 行 → 1 行 × 8 で **約 72 行減** (1,742 → 約 1,670 行)。既存の 2 クロージャもこれに置き換わる。

⚠️ `TestDoctorCacheCorruptAndAtomic` だけは**壊れた JSON** (`"{broken"`) を書くので
`doctorSnapshot` を受ける版では表現できない。対象外に残すか、`writeDoctorSnapshotRaw(t, []byte)`
を別に置いて上を委譲させる。同テストは `os.Chmod(dir, 0o500)` で書けないディレクトリを作る
前提も持つので、ヘルパー側の無条件 `MkdirAll` が邪魔にならないか確認する。

## 抽出しないと判断したもの (次の監査が同じ提案を再生成しないため)

- **`hasTerminalControl` の 3 コピー** (`status_view_test.go` / `issues/untrusted_test.go` /
  `termsafe/termsafe_test.go:hasControl`): **別パッケージ**なので `_test.go` のヘルパーは共有できず、
  共有するには `internal/testutil` の新設か termsafe への exported 関数追加が要る。しかしこの関数は
  **「termsafe の実装が正しいか」を termsafe と独立に検証する oracle** で、3 本ともコメントに
  「ESC と BEL だけ落とす実装が全 green で通ってしまう (敵対的レビュー 2026-08-05 が実際に突いた)」と
  書いてある。production と oracle を同じ出典にすると変異を検出できなくなる。**9 行の重複はこの
  独立性の対価として安い**
- **`t.Setenv("XDG_CACHE_HOME", t.TempDir())` × 25 / 9 ファイル**: `TestMain` のプロセス全体隔離と
  テストごと隔離の**二層設計**で、`tui_helpers_test.go` のコメントが明示的にそう設計している。
  1 行を 1 行 (`isolateCache(t)`) に置き換えても行数は減らず、「この env が何を隔離しているか」が
  ヘルパーを開かないと分からなくなる
- **PATH stub スクリプト** (`usage/codex_test.go:writeStub` と `usage_overlay_test.go` のローカル
  クロージャ `stub`): 別パッケージ跨ぎで、片方はローカルクロージャ 1 箇所だけ。5 行の重複のために
  パッケージを増やすのは複雑性が上がる方向
- **`newTestBrowse` / `benchBrowse` / `newFramedBrowse` の三つ子**: 「フレームを踏む/踏まない」
  「fast-path を通る/通らない」という測定意図の差がコメントで明示済み。統合すると片方の前提が黙って壊れる
- ⚠️ **`claude_version_test.go` が `writeVersionCache` (`tui_actions_test.go` に既存) を使っていない**
  点は寄せられそうに見えるが、前者は `path` 自体を `loadClaudeVersionCache(path, ...)` に渡すので
  path を返さない現行シグネチャでは寄せられない。シグネチャ変更の波及に対して利得が小さい

## ヘルパーの置き場所の規約

`tests/CLAUDE.md` の「ヘルパーは `lib/` か `test_` で始まらない名前に置く」は `make test` の
自動発見ルール (`tests/**/test_*.sh`) の裏返しで**シェル固有**。Go は発見規則が `func Test*` なので
同じ問題が起きず、規約の移植は不要。Go 側の実態は「main は `tui_helpers_test.go` に集約、
各サブパッケージは `main_test.go` に `TestMain` のみ、他はファイルローカル」で一貫している。

## 受け入れ条件

- [ ] 候補 1 / 2 を抽出し、各テストの主張 (assert) が 1 行も変わっていないことを diff で確認する
- [ ] 抽出後に `go test ./...` が green
- [ ] ⚠️ 印の注意点 (3 変数を返す / `t.Chdir` の順 / 壊れた JSON のケース) を踏んでいないか確認する

## 対応 (2026-09-03、後続セッション)

**候補 1 / 2 とも抽出した。production は 1 行も変えていない** (変更はテストファイル 2 本だけ)。

| | before | after | 備考 |
|---|---|---|---|
| `gitlog_watch_test.go` | 756 行 | **700 行** (-56) | `realRepoBrowse(t, height, subjects...)` を新設し **10 箇所**を 1 行へ |
| `doctor_view_test.go` | 1,779 行 | **1,753 行** (-26) | `writeDoctorSnapshot` / `writeDoctorSnapshotRaw` を新設し **7 箇所**を 1 行へ |

### 抽出でテストの主張が変わっていないことの確認

**「意味のある assert」の数が完全に不変**であることを機械的に確かめた
(`t.Fatal(err)` の定型だけがヘルパーへ移った):

| | 意味のある assert | 定型 `t.Fatal(err)` | テスト関数 |
|---|---|---|---|
| `gitlog_watch_test.go` | 101 → **101** | 14 → 5 | 24 → 24 |
| `doctor_view_test.go` | 195 → **195** | 41 → 30 | 33 → 33 |

### 想定と違った点

- **候補 1 の対象は 12 箇所ではなく 10 箇所**だった。`newBrowseModel` の呼び出し 12 回のうち
  1 つは高さが 24 (他は 10) で、残り 1 つはプロローグの形が違う。ヘルパーに `height` 引数を
  持たせて高さ違いも吸収し、形が違う 1 箇所はそのまま残した
- **`commits` を後で使うテストが 3 箇所あった** (`commits[2].SHA` / `m.diffOv.open(commits[0].SHA)`)。
  `m.commits` から同じものが取れる (他テストも既にその形) ので置き換えた。
  ヘルパーは `commits` を返さない — 返すと引数が 4 つになり、使う側が 3 箇所しか無い
- **候補 2 の 8 箇所のうち寄せられたのは 7 箇所**。残る 1 箇所 (`TestDoctorCacheCorruptAndAtomic` /
  `os.Remove` を使う箇所) は **path 自体が要る**ので `doctorSnapshotPath()` の直接呼び出しを残した
  (issue 本文の ⚠️ 注記どおり)。`writeDoctorSnapshotRaw` は壊れた JSON 用に用意したが、
  現状の呼び出し側は `writeDoctorSnapshot` 経由のみ
- **行数の削減は issue の見積もり (96 / 72 行) より小さい** (56 / 26 行)。ヘルパー本体
  (32 行 + 14 行) が同じファイルに入るため。**削減の主目的は行数ではなく、各テストの
  本題が先頭に来ること**なので、この差は問題としない

### 受け入れ条件

- [x] 候補 1 / 2 を抽出し、各テストの主張が変わっていないことを確認した (上表)
- [x] 抽出後に `go test ./...` green / `make -C src/glogx lint` 0 issues
- [x] ⚠️ 印の注意点を踏んでいない: 3 変数とも返す / `newTempRepo` の `t.Chdir` より後に
      `LoadCommits` を呼ぶ順序を維持 / 壊れた JSON のケースは寄せずに残した
