# bug: 見張りの fd 上限がディレクトリ数を数えていて、実際の fd を bound していない

起票日: 2026-09-05
カテゴリ: bug
優先度: 中（上限が目的を果たしていない。実害が出るのは hard limit が低い環境か、
remote-tracking ref が大量の repo。実効上限の見積もりは下記で訂正済み）

## 何が起きているか

`gitlog_watch.go:gitLogWatchMaxDirs` のコメントはこう書いている:

> 見張るディレクトリ数の上限。kqueue (macOS) は **1 ディレクトリにつき fd を 1 本使う**ので、
> ref を大量に持つ repo で fd を食い潰さないよう頭を止める。

**この前提は不完全で、そこから導いた上限は fd を bound していない。**
kqueue についての記述としては正しい（EVFILT_VNODE は監視対象 vnode 1 つにつき fd 1 本）が、
抜けているのは「fsnotify が inotify を模して**エントリ 1 つにつきさらに 1 本**開く」こと。
fsnotify の kqueue backend は
`watchDirectoryFiles()` (`backend_kqueue.go`) がディレクトリ直下の**全ファイルを個別に
`unix.Open` して register する**ため、`Add(dir)` のコストは **1 + そのディレクトリ内のエントリ数**。

したがって「ディレクトリ数 64 まで」という上限は、**守りたい量（fd）を一切測っていない**。
1 ディレクトリの中身が何万件でも 1 としか数えない。

## 実測（独立に 3 回確認）

環境: darwin/arm64, fsnotify **v1.10.1**（`src/glogx/go.mod` の pin と同一）。
`lsof -p <自 pid>` の**差分**で数えた（ヘッダ行・`cwd`/`txt`/`mem` は前後で相殺する。
`/dev/fd` の列挙と並べても差分は完全一致することを敵対レビューが確認済み）。

### (1) `Add(dir)` 単体のコスト

| ディレクトリ内のファイル数 | `Add` 前後の open fd 差分 |
|---:|---:|
| 0 | +1 |
| 10 | +11 |
| 100 | +101 |

→ `1 + entries` で線形。

🚨 **これは単独ディレクトリの式で、合成では成り立たない**（過大側に外れる）。
`addWatch` は既に watch 済みなら `info.wd` を再利用するので、親を Add したときにエントリとして
開かれたサブディレクトリは、後で明示的に Add しても fd が増えない。実コストは
`|dirs ∪ entries(dirs)|`。検算: 下の合成 repo は Σ(1+entries) = 4032 に対し実測 **4022**、
差 10 は重複したサブディレクトリと一致する。予算に使うなら安全側。

### (2) 本物の `gitLogWatchDirs` + 本物の `newDirWatcher` で実測

敵対レビューが `src/` を複製して**本物の関数**で測り直し、下の数字が再現した。

| repo | 見張るディレクトリ数 | 開いた fd |
|---|---:|---:|
| この dotfiles repo | 13 / 64 | 49 |
| 合成 repo（`refs/remotes/origin/` に loose ref 2000 本） | **11 / 64** | **4022** |
| worktree（`common != gitDir` の分岐が効く経路） | 14 / 64 | 57 |

🚨 **上限 64 に一度も達していないのに fd は 4022 本**。ディレクトリ数の上限は
何も bound していない。

### (3) issues viewer 側

`issuesWatchDirs` は `done/` を名指ししていない。入るのは `filepath.Dir(iss.Path)` 経由。
本物の `scanIssues` + `issuesWatchDirs` + 実 watcher での実測（dotfiles, issue 総数 274）:

```
issues        entries=12
issues/next   entries=1
issues/done   entries=258
issues/pending entries=9
→ ディレクトリ 4 件, 開いた fd = 281
```

**viewer を開くだけで 281 fd**（うち `done/` 由来が 259）。

## 🚨 fd は「起動時の Add」では決まらない — イベント経路で後から増える

**これが本 issue の核心で、初版の推奨対応が無効だった理由。**

fsnotify は Add 済みディレクトリに**後から**エントリが増えたときも fd を開く:
`readEvents` → `dirChange` → `sendCreateIfNew` → `internalWatch` → `unix.Open`
（`backend_kqueue.go`）。

本物の `gitLogWatchDirs()` + 本物の `newDirWatcher()` で実測
（`refs/remotes/origin/main` 1 本だけの repo で見張りを開始 → その後 2000 本追加）:

```
見張り開始:                     dirs=11  fd=+24
★ fetch 相当 (2000 ref 追加) 後: dirs=11  fd=+4024   WatchList=11
```

隔離 probe でも同型（1 ファイルの dir を Add → 500 作成 → 500 削除）:

```
Add 直後        fd=+2    (files=1)
500 作成後      fd=+502  WatchList=1
500 削除後      fd=+2    WatchList=1
```

つまり守りたい量は **見張り中に開いている fd という動的な量**で、削除時には戻る
（`readEvents` の Rename/Remove 分岐 → `w.remove` → `unix.Close`）ので定常値は
「生存エントリ数」に張り付く。

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

- **remote-tracking ref が多い repo**。`gitLogWatchDirs` の `appendSubdirs` は
  `.git/refs/**` と `.git/logs/**` を必ず登録対象に入れる
- 🚨 **「fetch 直後の一時的な窓」ではない**（初版の誤り）。fd の約半分は **reflog 由来で恒久的**。
  同一の合成 repo に順に打った実測:

  | 状態 | refs loose | logs files | 実測 fd |
  |---|---:|---:|---:|
  | 直後 | 2001 | 2002 | **4022** |
  | `git pack-refs --all` 後 | 0 | 2002 | **2021** |
  | `git gc` 後 | 0 | 2002 | **2022** |
  | `git reflog expire --expire=now --all` 後 | 0 | 2002 | **2022** |

  reflog ファイルは ref が生きている限り残る（`expire` はエントリを消すだけでファイルを消さない）。
  **`pack-refs` / `gc` では半分しか減らない**ので、消費は remote-tracking ref の本数に比例して恒久的
- **issues viewer 側も同型**（上の (3)）
- **silent に壊れる**: `startGitLogWatch` も `startWatch` も `_ = w.Add(dir)` でエラーを捨てる設計
  （消えて戻ったディレクトリを取り戻すための意図的な冪等 `Add`）なので、EMFILE に当たっても
  watcher 側は無言でイベント経路が欠けるだけ = 設計どおりのポーリング縮退にしか見えない。
  実害は**別の fd 利用者**に出る（`runGitTimeout` の git fork / `writeAtomic` の `os.CreateTemp` /
  status プレビューの `os.Open`）
- テストも lint も止めない（`waitdelay_discipline_test.go` は `exec.CommandContext` の走査なので無関係、
  `.golangci.yml` に `check-blank` が無いので errcheck も `_ = w.Add(dir)` を指摘しない）

## 🚨 EMFILE の部分失敗は恒久で、glogx の唯一の回復機構が効かない

`addWatch` は `w.watches.updateDirFlags(name, flags)` を `watchDirectoryFiles(d)` の**前**に実行する。
EMFILE で `watchDirectoryFiles` が落ちても、ディレクトリ自身の watch と `dirFlags`（NOTE_WRITE 付き）は
残る。次の `Add` は `alreadyWatching && info.dirFlags&NOTE_WRITE != 0` → `watchDir=false` →
**`watchDirectoryFiles` が二度と呼ばれない**。

実測（200 ファイルの dir、`ulimit -n 160`、ダミー fd 60 本で逼迫を作り、失敗後に解放してから再 Add
= glogx が毎周期やる操作）:

```
1 回目 Add (逼迫中): err="…/f0092": too many open files
ダミー fd を解放
2 回目 Add: err=<nil>        ← 偽の成功
3 回目 Add: err=<nil>
f0000 の Write イベント: true
f0050 の Write イベント: true
f0100 の Write イベント: false   ← 失敗点 (f0092) 以降は恒久的に無 watch
f0150 の Write イベント: false
```
（上限なしの基準 run では 5 本とも `true`。）

これは `startGitLogWatch` / `startWatch` が根拠にしている

> 🚨「Add 済み」を自前で覚えて skip しないこと。… Add は冪等なので毎回無条件に呼び、
> 消えて戻った先を取り戻す

に対する**反例**。「ディレクトリが消えて戻った」ケースでは正しいが、
「**エントリ登録が部分失敗した**」ケースでは取り戻さない。

しかも 2 回目以降が `err=nil` なので、**「エラーを捨てるのをやめる」方向の修正でも検出できない**
（`AddWith` は失敗時 `addUserWatch` を呼ばないので `WatchList()` にも出ない）。

## 実害の見積もり（初版の数字は誤り）

初版は「この開発機の `ulimit -n` は 1,048,576 なので枯渇しない」と書いたが、**それは効いている
上限ではない**。実測:

```
sysctl kern.maxfiles: 491520 / kern.maxfilesperproc: 245760

Go の syscall.Getrlimit(RLIMIT_NOFILE):
  素で起動            → cur=245760
  ulimit -Sn 256 下   → cur=245760   ← Go runtime が soft→hard へ上げ、darwin では
  ulimit -Sn 1024 下  → cur=245760      maxfilesperproc へ clamp
  ulimit -n 160 (soft=hard) 下 → cur=160   ← hard を絞ったときだけ効く
```

訂正 3 点:

1. 実効上限は **245,760**（`kern.maxfilesperproc`）で 1,048,576 ではない
2. **shell の `ulimit -n`（soft）を下げても効かない**。悪化するのは **hard limit が低い**環境だけ。
   初版の「設定を絞ったマシン」は、実際には効かない条件を悪化条件として挙げていた
3. 「コンテナ」は射程外（`CLAUDE.md`「macOS のみ。Linux はサポート対象外」）。
   **CI（macOS runner）の hard limit は未測定**

したがって **今すぐ壊れる話ではない**。本 issue の中身は次の 2 つ:

1. **コメントの前提が不完全で、上限が目的を果たしていない**。次に上限を触る人は同じ計算をやり直す
2. **部分失敗が恒久的で回復しない**（上節）。こちらは fd が足りている限り顕在化しないが、
   一度当たると `r` で再起動するまで戻らない

## 推奨対応（初版の案は無効。書き直した）

🚨 **初版の「`os.ReadDir` の件数を足し込んで打ち切る」は採らない。** 2 つの理由で機能しない:

- **起動時の `Add` しか縛らない**。上の「イベント経路で後から増える」のとおり、
  fetch が開く fd は 1 本も止まらない（+24 → +4024 は全部イベント経路）
- **`issues` 側には当ててはいけない**。`issuesWatchDirs` は **watch 集合と指紋の入力を兼ねている**
  （`issues_view.go` の `issuesFingerprint(issuesWatchDirs(dirs, found), …)` と
  `issues_watch.go:watchTargets` が同じ関数を呼ぶ）。打ち切ると落ちた dir は
  ①指紋の dir 側 mtime から消え ②新規ファイルは `paths`（前回スキャン由来）にも無いので、
  **そのディレクトリへの新規 issue 出現がイベントでもポーリングでも永久に無音**になる。
  しかも `issues_watch.go` の🚨「スキャン側と観測側で必ず同じ集合を作ること」は守られたままなので
  既存テストは緑（silent 退行）

代わりに:

1. **`gitlog_watch.go` のコメントを実測に合わせて直す**（最小で最も価値がある）。
   「`Add(dir)` = 1 + entries」「イベント経路でも増える」「上限はディレクトリ数であって fd ではない」を
   fsnotify の `watchDirectoryFiles` / `dirChange` を名指しして書く
2. **fd を本当に bound したいなら、継続的な会計 + 超過時の `Remove`** が要る（動的な量なので）。
   ただし上の理由から **`issues` 側は対象外**（指紋を watch 集合から分離する設計変更が先）
3. **部分失敗の恒久化に対処する**なら、`Add` の戻りを見るだけでは足りない（2 回目以降 nil）。
   `WatchList()` と期待集合を突き合わせるか、周期的に watcher を作り直す
4. **`tui.go` のコメントが `fsnotify v1.9.0` と書いているが pin は v1.10.1**。挙動は同じだが、
   ここを触るなら同時に直す

### テストの書き方（初版の案には穴がある）

`newDirWatcher` は package var の seam で、`issues_watch_test.go` に前例がある。ただし初版が
書いた「fake に Add されたディレクトリを記録させ、見積もりが予算を超えないことを assert」には
2 つの穴がある:

- production の見積もり式もテストの期待値も同じ `os.ReadDir` の件数になり、
  `~/.claude/rules/mutation-verify-new-tests.md` の「期待値を production と同じ式から作っていないか」に該当
- **fake はイベント経路で開かれる fd を模さない**ので、守りたい量の主要成分（+24 → +4024）が
  射程外になる

## 反証の試み

`issues/` と `issues/done/` を `kqueue` で grep したところ、当たったのは issue 035（fsnotify 導入）と
`tui.go` のコメントだけ。後者は「kqueue fd **本体**が `O_CLOEXEC` を持たないので `restartSelf` で
継承される」という**別の話**で、これは `stopWatch` / `stopGitLogWatch` で正しく閉じられている
（`kqueue.Close()` が `listPaths(false)` の全パスを `unix.Close` する。v1.10.1 の #732 が明示的に
この穴を塞いでいる）。ディレクトリ内ファイル数に比例する fd の話は `docs/` を含めどこにも無い。

🚨 なお `system_darwin.go` の `openMode = unix.O_EVTONLY | unix.O_CLOEXEC` なので、
**監視対象の fd（今回の 4022 本）は exec で継承されない**。継承されるのは kq 本体だけ。

## 関連

- `issues/done/035-refactor-glogx-issues-watch-backoff.md`（fsnotify 導入）
- `issues_watch.go:watchTargets` / `issues_view.go` の `issuesFingerprint`（watch 集合と指紋の結合）
