# bug: 見張りの fd 上限がディレクトリ数を数えていて、実際の fd を bound していない

起票日: 2026-09-05
カテゴリ: bug
優先度: 中（前提が誤っている。実害が出るのは fd 上限が低い環境か loose ref が大量の repo）

## 何が起きているか

`gitlog_watch.go:gitLogWatchMaxDirs` のコメントはこう書いている:

> 見張るディレクトリ数の上限。kqueue (macOS) は **1 ディレクトリにつき fd を 1 本使う**ので、
> ref を大量に持つ repo で fd を食い潰さないよう頭を止める。

**この前提は誤り。** fsnotify の kqueue backend は inotify を模すために
`watchDirectoryFiles()` (`backend_kqueue.go`) がディレクトリ直下の**全ファイルを個別に
`unix.Open` して register する**ため、`Add(dir)` のコストは **1 + そのディレクトリ内のエントリ数**。

したがって「ディレクトリ数 64 まで」という上限は、**守りたい量（fd）を一切測っていない**。
1 ディレクトリの中身が何万件でも 1 としか数えない。

## 実測（独立に 2 回確認）

環境: darwin/arm64, fsnotify **v1.10.1**（`src/glogx/go.mod:8` の pin と同一）。
`./tmp/fdprobe` の隔離 module で `lsof -p <自 pid>` を `Add` の前後で数えた
（repo の `go.mod` / `go.sum` には触れていない）。

### (1) `Add(dir)` 単体のコスト

| ディレクトリ内のファイル数 | `Add` 前後の open fd 差分 |
|---:|---:|
| 0 | +1 |
| 10 | +11 |
| 100 | +101 |

→ `1 + entries` で厳密に線形。

### (2) glogx の `gitLogWatchDirs` + `startGitLogWatch` を再現した実測

`gitLogWatchDirs` と同じ規則（`.git` + `refs/**` + `logs/**`、上限 64）でディレクトリを集め、
全部 `Add` したときの fd を数えた。

| repo | 見張るディレクトリ数 | 開いた fd |
|---|---:|---:|
| この dotfiles repo | 13 / 64 | 49 |
| 合成 repo（`refs/remotes/origin/` に loose ref 2000 本） | **11 / 64** | **4022** |

🚨 **上限 64 に一度も達していないのに fd は 4022 本**。ディレクトリ数の上限は
何も bound していないことが、これで示せる。

### 再現コード（`tmp/` は gitignore なので本文に残す）

`go.mod` は `module fdprobe` + `require github.com/fsnotify/fsnotify v1.10.1` だけ。
実行: `go run . <対象 repo>/.git`（引数なしの表 (1) は `Add` 1 回ぶんを測る版）。

```go
package main

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/fsnotify/fsnotify"
)

func openFDs() int {
	out, _ := exec.Command("lsof", "-p", strconv.Itoa(os.Getpid())).Output()
	return len(strings.Split(strings.TrimSpace(string(out)), "\n"))
}

// gitLogWatchDirs と同じ規則でディレクトリを集め、全部 Add したときの fd を数える。
func main() {
	gitDir := os.Args[1]
	dirs := []string{gitDir}
	for _, root := range []string{filepath.Join(gitDir, "refs"), filepath.Join(gitDir, "logs")} {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if !d.IsDir() {
				return nil
			}
			if len(dirs) >= 64 { // gitLogWatchMaxDirs
				return filepath.SkipAll
			}
			dirs = append(dirs, p)
			return nil
		})
	}
	w, _ := fsnotify.NewWatcher()
	before := openFDs()
	for _, d := range dirs {
		_ = w.Add(d)
	}
	after := openFDs()
	fmt.Printf("見張るディレクトリ数=%d (上限 64), Add 全部で開いた fd=%d\n", len(dirs), after-before)
	w.Close()
}
```

合成 repo（表 (2) の下段）の作り方:

```sh
git init -q . && git commit -q --allow-empty -m x
sha=$(git rev-parse HEAD)
for i in $(seq 1 2000); do echo "create refs/remotes/origin/feature-$i $sha"; done | git update-ref --stdin
```

## 発火条件

- 単一ディレクトリに loose ref が大量にある状態。典型は**大きな fetch の直後**
  （`git gc` / `pack-refs` が走る前）の `.git/refs/remotes/<remote>/` や `.git/refs/tags/`。
  `gitLogWatchDirs` の `appendSubdirs` はこれらを必ず登録対象に入れる
- **issues viewer 側も同型**: `issues_watch.go:startWatch` が `issues/done/` を `Add` する。
  この repo では現在 258 ファイルなので、viewer を開くだけで 259 fd を消費する
- **silent に壊れる**: `startGitLogWatch` も `startWatch` も `_ = w.Add(dir)` でエラーを
  捨てる設計（消えて戻ったディレクトリを取り戻すための意図的な冪等 `Add`）なので、
  EMFILE に当たっても watcher 側は無言でイベント経路が欠けるだけ = 設計どおりの
  ポーリング縮退にしか見えない。**実害は別の fd 利用者に出る**（`runGitTimeout` の
  git fork / `writeAtomic` の `os.CreateTemp` / status プレビューの `os.Open` が失敗し、
  原因から遠い場所でエラーになる）
- テストも lint も止めない（`waitdelay_discipline_test.go` / `.golangci.yml` のどのリンタも
  この形を見ない）

## 実害の見積もり（誇張しないための注記）

この開発機の `ulimit -n` は **1,048,576** なので、4022 fd でも枯渇しない。
**今すぐ壊れる話ではない**。問題は 2 つ:

1. **コメントが嘘をついている**。次に上限を触る人は同じ誤った計算をやり直す
2. **上限が目的を果たしていない**。「fd を食い潰さないよう頭を止める」と書いてあるのに、
   fd は止まっていない。`ulimit -n` が低い環境（コンテナ・CI・設定を絞ったマシン）や
   `refs` がさらに大きい repo では、この差がそのまま実害になる

## 推奨対応

1. **上限を「ディレクトリ数」から「確保される fd の見積もり」へ変える**。
   `appendSubdirs` / `watchDirs` でディレクトリを積むときに `os.ReadDir` の件数を足し込み、
   合算が予算を超えたら打ち切る。打ち切っても**保険のポーリングが受け持つ**という
   既存の縮退はそのまま成立する
2. **`gitlog_watch.go` のコメントを実測に合わせて直す**。根拠として
   「`Add(dir)` = 1 + entries」と fsnotify の `watchDirectoryFiles` を名指しで書くこと
   （誤った前提が残るのが本体の害）
3. **退行を止めるテストは `dirWatcher` の seam に噛ませられる**。fake watcher に `Add` された
   ディレクトリを記録させ、「N ファイルのディレクトリを含むターゲット集合で、`Add` の
   合計コスト見積もりが予算を超えない」を assert する（fake が件数を数える形にすれば
   実 fd を使わずに固定できる）
4. 🚨 この上限を新設・変更したら、**予算を外す変異を当てて red を見る**まで確認すること
   （`~/.claude/rules/mutation-verify-new-tests.md`）。「N 段構え」を宣言するなら段ごとに

## 反証の試み

`issues/` と `issues/done/` を `kqueue` で grep したところ、当たったのは issue 035
（fsnotify 導入）と `tui.go` のコメントだけ。後者は「kqueue fd **本体**が `O_CLOEXEC` を
持たないので `restartSelf` で継承される」という**別の話**で、これは `stopWatch` /
`stopGitLogWatch` で正しく閉じられている。ディレクトリ内ファイル数に比例する fd の話は
`docs/` を含めどこにも無く、`gitlog_watch.go` の当該コメントが唯一の記述で、
それが今回の実測と矛盾している。

## 関連

- `issues/done/035-*`（fsnotify 導入）
- `issues_watch.go:startWatch`（同型。`issues/done/` を Add する）
