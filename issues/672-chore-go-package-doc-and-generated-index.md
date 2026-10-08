# 672 (chore): Go の package に doc コメントを必須にし、「再利用できる package の一覧」を生成する

起票日: 2026-10-08

## 概要

`src/README.md` の一覧は module 単位の手書きで、module の中の package (`src/tuikit/termwidth` など) の索引が無い。
そのため、既にある package に気づけず、同じ処理を別の言語や別の場所で書き直す (実例: `bin/tmux-toast` が表示セル幅を
python3 で数えていて、`termwidth` を使っていない。issue 670)。

package の doc コメント (`// Package termwidth は …`) を正本にして一覧を**生成**し、doc コメントの無い package を検査で止める。
手書きの表は実体とずれるので、package の粒度では手で持たない。

## 現状 (2026-10-08)

- 17 module / 76 package (`src/*/` の各 module で `go list ./...`)
- doc コメントが無い package は 5 個:
  `src/doctor/cmd/diskdoctor` / `src/doctor/cmd/svcdoctor` / `src/restartable` / `src/restartable/internal/runner` / `src/restartable/internal/ui`
- 一覧を作る道具は無い

## 対応方針

- 一覧の生成: 各 module で `go list -f '{{.ImportPath}}\t{{.Dir}}\t{{.Doc}}' ./...` を回して並べる (`.Doc` は doc コメントの最初の 1 文)。
  `bin/` か `scripts/` に置き、出力先を決める:
  - 案 1: `src/PACKAGES.md` に書き出して commit し、「生成し直すと差分が出ない」ことを検査する (ずれは CI が止める)
  - 案 2: commit せず、必要なときに道具を叩いて見る (ずれは原理的に起きないが、入口が道具の存在を知っている人に限られる)
- 検査が素通りする形を設計で潰す (2026-10-08 のレビュー):
  - `go list` が module 単位で失敗すると出力が 0 行になり、「doc 無し 0 件」で緑になる → rc を見て、見た package の総数を出し、下限 (76) と突き合わせる
  - `.Doc` が空白だけのもの / `// Package <名前>` で始まらない doc コメントを通さない
  - build tag や `GOOS` で除外されるファイルは `./...` に出ない (例: `glogx/gorules/rules.go` / `ratelimit/gorules/rules.go` /
    `chromecookie/keychain_other.go` / `restartable/internal/runner/process_unix.go`)。そのファイルにしか doc が無い・そのファイルだけの package が母集合から黙って外れる
  - `go list` は `GOWORK=off` で回す (issue 671 で go.work を置いても結果が変わらないように)
  - 一覧に `.Dir` を出すなら repo 相対にする (絶対パスは commit できない)
- 検査: doc コメントの無い package を落とす。`main` package (`cmd/*`) も対象に含めるかを決める (含めるなら「何のコマンドか」を 1 行書く)
- `internal/` も一覧に載せるかを決める (import できる範囲は module の中に限られる。載せるなら印を付ける)
- 入口: `src/README.md` からこの一覧 (か道具) へ 1 行で案内する (`new-tool-requires-entrypoint-docs.md`)

## 受け入れ条件

- [ ] 上の 5 個に doc コメントを書く
- [ ] 一覧を生成する道具を置き、出力の形 (案 1 / 案 2) を決めて理由を本 issue に書く
- [ ] doc コメントの無い package を落とす検査を `make test-lint` (か Go の lane) に入れ、変異 (doc コメントを 1 つ消す) で red になることを確かめる
- [ ] 検査が CI で走っていることを、ログの検査名で確かめる
- [ ] `src/README.md` から一覧へ案内する

## 関連ファイル

- `src/README.md` / `src/*/go.mod`
- issue 670 (Go への移植。移植した道具もこの一覧に載る) / issue 671 (go.work。エディタからの発見性。こちらは文書からの発見性)

## 進捗

- 2026-10-08: 起票
- 2026-10-08: 反証レビューを反映 (検査が素通りする形: go list の失敗で 0 件の緑・空白の Doc・build tag で除外される package・GOWORK・`.Dir` の絶対パス)。反証できなかった主張: 76 package / doc の無い 5 個
