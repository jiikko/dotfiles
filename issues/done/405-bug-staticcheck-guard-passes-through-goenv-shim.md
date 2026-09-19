# `staticcheck が無い` ガードが goenv の shim を掴んで素通りし、`make test` が毎回落ちる

起票日: 2026-09-20
カテゴリ: bug / priority: **medium**
対象: `scripts/check_unused_excluding_tests.sh` の存在ガード (`command -v staticcheck`)
出典: [issue 404](404-retro-av1ify-vfr-six-rounds-and-lockman-364-2026-09-19.md) の残課題
反証レビュー: 未実施。**下記はすべて 2026-09-20 の実測**

## 症状

`make test` の `test-unused-excluding-tests` が落ち続ける。出力は

```
✗ ?? src/glogx: 解釈できない staticcheck の出力: goenv: 'staticcheck' command not found
✗ ?? src/glogx: 解釈できない staticcheck の出力: The 'staticcheck' command exists in these Go versions:
✗ ?? src/glogx: 解釈できない staticcheck の出力:   1.25.4
```

対象 module の数だけ同じ行が並ぶ (実測 6 行)。

## 原因: ガードが「shim の存在」しか見ていない

`check_unused_excluding_tests.sh` は実行前にこう守っている:

```sh
if ! command -v staticcheck >/dev/null 2>&1; then
  echo "✗ staticcheck が無い (go install honnef.co/go/tools/cmd/staticcheck@latest)" >&2
  exit 1
fi
```

goenv を使っていると **`command -v` は shim に当たって必ず成功する**:

```
$ command -v staticcheck
/Users/koji/.anyenv/envs/goenv/shims/staticcheck   # rc=0
$ staticcheck --version
rc=127 / stderr: goenv: 'staticcheck' command not found
```

shim は「どの Go 版にも無い」ときでも PATH 上に在るので、**ガードは原理的に発火しない**。
その結果、用意された親切なメッセージ (`go install …@latest`) は**一度も出ず**、
代わりに shim のエラー文が「解釈できない staticcheck の出力」として parser に流れ込む。

つまりこれは「staticcheck が入っていない」ではなく、**存在検査の手段が間違っている**話。

## 今この環境で起きている条件

- goenv の global が **1.26.0** (`/Users/koji/.anyenv/envs/goenv/version`)
- staticcheck が入っているのは **1.25.4 側だけ** (実際には 1.25.4 の bin にも `go` / `gofmt` しか
  無く、shim が「exists in these Go versions」と案内する先にも実体が無い可能性がある。要確認)
- CI は影響なし。`.github/workflows/unused.yml` は版を pin して `go install` してから
  `$(go env GOPATH)/bin/staticcheck` を**絶対パスで**叩くので、この経路を通らない

**いつから壊れたか**: `global_go_version` を上げた際の取り残しに見えるが、未確定 (要 `git log`)。

## 直し方 (案)

1. **ガードを「実行できるか」で書く**。`command -v` は shim を掴むので当てにならない:
   ```sh
   if ! staticcheck --version >/dev/null 2>&1; then
     echo "✗ staticcheck を実行できない (go install honnef.co/go/tools/cmd/staticcheck@latest)" >&2
     exit 1
   fi
   ```
   これだけで「親切なメッセージが出ない」は直る (根本の未導入は別途)
2. **CI と同じく版を pin して絶対パスで叩く**。`unused.yml` は既にそうしており、
   手元とやり方が違うのが乖離の温床 (`.github/workflows/unused.yml:48` の
   「手元の staticcheck を上げたらここも同じ版へ更新すること」という注記自体が、
   手元と CI で導入経路が二重管理になっている証拠)
3. **setup.sh で入れる**。現状 `setup.sh` / `Brewfile` のどちらにも staticcheck は無く、
   「各自が `go install` する」前提になっている。goenv の版を上げるたびに消えるので、
   **版を上げる導線 (`global_go_version` / setup.sh) と同じ場所で入れ直す**のが筋

## 受け入れ条件

- [ ] staticcheck が実行できない環境で、**用意されているメッセージが実際に出る**こと
      (shim を掴んで素通りしない)。`PATH` から外した状態と、shim だけ在る状態の**両方**で確認する
- [ ] `make test` が手元で緑に戻る
- [ ] 変異検証: ガードを元の `command -v` 形へ戻すと、上の確認が red になること
- [ ] 同じ形 (`command -v <goenv/rbenv/nodenv が shim を持つコマンド>`) が他の検査に無いか grep する
      (**あれば同じ穴**。shim を持つ言語は goenv だけではない)

## 進捗 (2026-09-20): 完了

### 根本は「staticcheck が無い」ではなく **版上げで go 製ツールが 17 本消えた**

goenv は **GOPATH を版ごとに分ける** (`~/go/<version>`)。実測:

| | |
|---|---|
| `~/go/1.25.4/bin` | 17 本 (staticcheck / golangci-lint / gopls / goimports / dlv / revive …) |
| `~/go/1.26.0/bin` | **空** |
| `~/.anyenv/envs/goenv/versions/*/bin` | どちらも `go` と `gofmt` だけ |

つまり 1.25.4 → 1.26.0 の版上げで **go 製ツールがまとめて PATH から消え、shim だけが残った**。
staticcheck はその 1 本にすぎない (issue 本文の「1.25.4 の bin にも実体が無い可能性」は、
**版ごとの GOPATH を見ていなかった**ため。実体は `~/go/1.25.4/bin` に在った)。

### 直したこと

1. **ガードを「実行できるか」で書いた** (`staticcheck --version` の rc)。案内にも
   「いま有効な Go 版へ入れること (GOPATH が版ごとなので版を上げると消える)」を足した
2. **同型を全数 grep した** (受け入れ条件 4)。`command -v` の対象のうち shim を持つのは
   **go / ruby / staticcheck の 3 つ**:

   | 箇所 | 判定 |
   |---|---|
   | `scripts/check_unused_excluding_tests.sh` (staticcheck) | **直した** (本 issue) |
   | `bin/lib/go_autobuild.zsh` (go) | **直した**。同じ害 (案内が素通りして exit 127 の英語エラーが点滅する)。判定はすぐ下で必要な `go env GOVERSION` に寄せ、呼び出しを増やしていない |
   | `Makefile:486` (ruby) / `Makefile:549` (go) | **直さない**。shim が通っても後続 (`ruby -c` / `go build`) が落ちて red になる = fail-closed。案内が消える害も無い |

3. **staticcheck を CI と同じ pin (v0.7.0) で現行版へ導入した** → `make test` が緑に戻った
   (`staticcheck 2026.1 (v0.7.0)`)

### 受け入れ条件

- [x] staticcheck が実行できない環境で、用意されているメッセージが実際に出る
      (`tests/scripts/test_staticcheck_guard_detects_shim.sh`。**A) PATH に無い** と
      **B) 偽 shim だけ在る** の両方を作る)
- [x] `make test` が手元で緑に戻る (rc=0)
- [x] 変異検証: ガードを `command -v` へ戻すと B が red。しかも **production の症状
      (「解釈できない staticcheck の出力」が 6 module ぶん並ぶ) をそのまま再現**した
- [x] 同型の grep (上表)

### 🚨 残る問題 (本 issue のスコープ外。別 issue 候補)

**版を上げるたびに go 製ツール 17 本が消える導線が、どこにも無い**。今回 staticcheck だけを
入れ直したので `make test` は緑になったが、`golangci-lint` は **各 module の Makefile が
`go run …@v2.5.0` で都度取る**ため影響を受けていないだけで、`gopls` / `dlv` / `goimports` などは
消えたまま (エディタ側で効く)。直すなら「版を上げる導線 (`global_go_version` / `setup.sh`) と
同じ場所で入れ直す」= issue 本文の案 3。**setup.sh / Brewfile のどちらにも staticcheck は無い**。

### ルールへの横展開 (本文の「一般化」に対する回答)

`verify-execution-not-just-exit-code.md` へ「**存在検査版**」を 1 行足す価値がある
(「`command -v` は PATH に名前が在るかしか見ない。version manager の shim・ラッパー・alias 越しでは
『在るのに実行できない』が起きるので、実行可否は無害なサブコマンドの rc で見る」)。
**切り出しの実行はユーザーの判断待ち**。

## 🚨 この issue の一般化 (同型が他にもあるはず)

**`command -v` は「PATH に名前が在る」しか見ない**。version manager の shim・ラッパー・
`alias` 越しでは「在るのに実行できない」が普通に起きる。実行可否を見たいなら
**実際に無害なサブコマンド (`--version`) を走らせて rc を見る**のが正しい。
[`verify-execution-not-just-exit-code.md`](../../_claude/rules/verify-execution-not-just-exit-code.md)
の「exit code 0 は『失敗しなかった』であり『そもそも走らなかった』を含む」の存在検査版。
横展開の結果しだいでは、ルールへ 1 行足す価値がある。
