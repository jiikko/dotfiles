# 852 (bug): treefiler と glogx の TestFrameAllocBudget が、テストの dir が git repo の中にあると確保の上限を超えて落ちる

起票日: 2026-10-21

## 概要

`src/treefiler/filer` の `TestFrameAllocBudget` (`filer/alloc_budget_test.go`) は、止まった画面 1 フレームの確保の回数を上限
`frameAllocBudget` (258) と比べる。テストが作る木 (`t.TempDir()`) が**どこかの git repo の中**にあると、git の状態の印を描くぶん確保が増えて落ちる。
`TMPDIR` を repo の中 (dotfiles の `tmp/` など) に向けて `go test` を回すと再現する。既定の `TMPDIR` (`/var/folders/…`) と CI では repo の外なので通る。

issue 850 の確認で、使い捨ての `TMPDIR` を `~/dotfiles/tmp/…` に置いたときに見つけた。

## 実測 (2026-10-21、go1.26.0 darwin/arm64、`go test -count=1 -v -run '^TestFrameAllocBudget$' ./filer/`)

| `TMPDIR` | 1 フレームの確保 |
|---|---|
| 既定 (`/var/folders/…/T/`) | 256 |
| `/tmp/a` | 256 |
| `mktemp -d` (repo の外) | 256 |
| `~/dotfiles/tmp/…` (dotfiles の repo の中・`tmp/` は ignore) | 262 |
| 同上で、深さ・長さを変えた 2 通り | 262 |
| `git init` した dir の下 (ignore なし = untracked) | 312 |
| `git init` した dir の下の ignore された dir | 262 |

パスの長さ・段数では変わらず、git repo の中か、その中でどの状態に見えるかで変わる (`/tmp/a` は symlink の下だが 256 なので、`/var` と `/private/var` の違いでもない)。

glogx の `TestFrameAllocBudget` の filer の行 (`src/glogx/frame_alloc_test.go` の `budgetFilerModel`、上限 222) も同じ前提に依る
(`-run '^TestFrameAllocBudget$' .`):

| `TMPDIR` | filer の行の確保 |
|---|---|
| 既定 | 209 (緑) |
| `git init` した dir の下 (untracked) | 245 (赤) |

`alloc_budget_test.go` の `frameAllocBudget` のコメントは、glogx のこの行を退行の主な検出役として挙げている。treefiler だけを直すと glogx に偽の赤が残る。

## 原因

- treefiler は開いた dir から上へ `.git` を探し (`filer/git.go` の `findRepoTop`)、見つかれば git の状態の印・芽の色・ブランチを描く
- テストは自分の木が repo の外にあることを前提にしているが、それを固定していない。前提を決めているのは `TMPDIR` の場所 (テストの外)
- 🚨 `findRepoTop` は git を起こさず自分でディレクトリを上へ辿るので、`GIT_CEILING_DIRECTORIES` では隔離できない (glogx の `gitdata` のテストが使う手は効かない)

## 影響

- 手元で `TMPDIR` を repo の中に向けると偽の赤になる。CLAUDE.md の「一時ファイルの配置」は作業ファイルを本体 checkout の `tmp/` に置くよう求めているので、
  その流れで `TMPDIR` を `tmp/` の下に向けると踏む (issue 850 で踏んだ)
- 逆向き: repo の中で回すと、テストは「git の印を描く 1 フレーム」を測ることになり、上限 258 が何を守っているかが場所で変わる
- 同じ前提に依るテスト: treefiler の `filer` 全体では `TestFrameAllocBudget` の 1 本だけが赤になった (repo の中の ignore / untracked の両方で)。
  glogx では `TestFrameAllocBudget` の filer の行が赤になる (上の表)。glogx の他のテストと、他の module は未確認
- 未実測の推定: dotfiles の中で回すと、`fetchRepo` (`git.go`) は dotfiles 全体に `git status --ignored` を打つ。上限は `subproc.GitOpTimeout`。
  repo が重いと上限切れで印が出ず 256 に戻りうるので、repo の中の回数は場所だけでなく repo の重さでも揺れうる

## 対応方針 (案)

1. **テストが git の状態を自分で決める**: 木を作った dir で `git init` して全部を commit し、印の無い clean な repo として測る。glogx の `budgetFilerModel` も同じにする。
   副作用と、決めておくこと:
   - 上限は測り直し、新しい値の出典 (go の版・`-race` なし) をコメントに書き直す。ブランチ名を描くぶん、今の値から変わりうる
   - 初期ブランチ名は `initRepo` の `-b` で固定する (`gitIn` は global 設定を `/dev/null` にするので `init.defaultBranch` は効かず、git の版で名前が変わる)
   - git を要するテストになる。`needGit` は CI 以外では Skip するので、git の無い手元では予算の検査が丸ごと走らなくなる
   - `fetchRepo` は `GIT_CONFIG_GLOBAL` を落とさずに `git status` を起こすので、利用者の global 設定 (`status.*`・excludesfile) は測る状態に入り続ける。
     clean な repo ならほぼ効かないはずだが、「どの `TMPDIR` でも同じ状態」は見積もり
2. **repo の外であることを確かめて、そうでなければ skip / fail する**: `findRepoTop(dir) != ""` なら理由を出して止める。測る対象は今と同じだが、`TMPDIR` を repo の中に向けた run では測れない
3. **git を見ない設定を Model に足す**: production に testing のための口を足すことになるので、1・2 で足りるなら採らない

1 は場所に依らず同じものを測れるので第一候補。2 は変更が小さいが、`TMPDIR` を `tmp/` に向ける運用と両立しない。

## 確かめ方

- `TMPDIR` を repo の外・repo の中 (ignore / untracked) の 3 通りにして `-run '^TestFrameAllocBudget$'` を回し、確保の回数が同じになること
- 変異: 対応を外すと、repo の中の `TMPDIR` で赤 (または回数が変わる) に戻ること

## 関連ファイル

- `src/treefiler/filer/alloc_budget_test.go` (`TestFrameAllocBudget` / `frameAllocBudget`)
- `src/treefiler/filer/git.go` (`findRepoTop`)
- `src/treefiler/filer/helpers_test.go` (`newAt` / `isolateConfig`)
- `src/glogx/frame_alloc_test.go` (`TestFrameAllocBudget` の filer の行 / `budgetFilerModel`)

## 進捗

- [x] 原因の切り分けと実測 (2026-10-21)
- [x] 反証レビュー (サブエージェント 1 体、read-only): P1 = glogx の filer の行も同じ前提に依る (再現して本文へ反映) /
  P2 = 方針 1 の副作用 (反映) / P3 = repo の重さで揺れる推定 (未実測として反映)。反証できなかった主張: 256 / 312 の実測、
  `findRepoTop` が git を起こさず `GIT_CEILING_DIRECTORIES` が効かないこと、symlink 説の否定、CI の workflow が `TMPDIR` を設定していないこと
- [x] 直す (方針 1) — commit「treefiler・glogx: 確保の予算の検査を、全部を commit した repo の木で測る」
  - treefiler: `cleanRepo` (`alloc_budget_test.go`) で木を `main` の repo にして全部を commit。`GIT_CONFIG_GLOBAL=/dev/null`・`GIT_CONFIG_NOSYSTEM=1` を `t.Setenv` し、
    裏の `git status` にも利用者の設定ファイルを読ませない (既定の excludesfile `~/.config/git/ignore` は読まれる。コメントに明記)
  - glogx: `budgetFilerModel` で同じことをする (`gitIn` の引数を `testing.TB` に広げて流用)
  - 下界の検査に「`⎇ main` を描いたか」を足した (反証レビュー 2 回目の P2: git の取り込みが黙って失敗すると、印の無い軽い画面を測って緑になる)
- 実測 (go1.26.0 darwin/arm64、`TMPDIR` = 既定 / repo の中の untracked / repo の中の ignore の 3 通り):
  treefiler は 3 通りとも 258 回 (上限を 258 → 260 に上げた。増えた 2 回は木を repo にしてブランチ名を描く分。repo の外で測っていた修正前は 256)。
  glogx の filer の行は 3 通りとも 211 回 / 50,214〜50,223 B (`-race`)。上限 222 / 51,500 B は据え置き
- 変異 (mutate-verify、`TMPDIR` を repo の中の untracked に向けて): `cleanRepo(t, dir)` の呼び出しを消すと treefiler が 312 / 260 で red。
  glogx の repo 化の 3 行を消すと 245 / 222 で red。`branchFor` が常に "" を返す形にすると、treefiler と glogx の両方が「ブランチを描いていない」で red
- [x] 他のテストに同じ前提が無いかを見る: treefiler の `filer` 全体では `TestFrameAllocBudget` だけ (本文の「影響」)。glogx はこの 1 行を直した。
  glogx の他のテストと他の module は見ていない (repo の中の `TMPDIR` で `make test` を回すのは、この issue の範囲外)
- 反証レビュー 2 回目 (素通りの観点、read-only 1 体): P1 なし。P2 (ブランチの前提) は上で対応。P3 のうち「global の設定も切る」の言い過ぎはコメントを直した。
  残した P3: `needGit` は CI の外では Skip するので、git の無い手元では treefiler の予算の検査が走らない (CI では Fatal なので CI 側は弱まっていない。glogx は git が無いと Fatal で、
  2 つの module で扱いが違う)。glogx の case の注記「止まった画面で 220 / 50799 B」は今の実測 211 / 50,2xx B と合わないが、修正前からのずれで上限も据え置いたので触らない
