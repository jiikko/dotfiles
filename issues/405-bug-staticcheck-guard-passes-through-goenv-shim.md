# `staticcheck が無い` ガードが goenv の shim を掴んで素通りし、`make test` が毎回落ちる

起票日: 2026-09-20
カテゴリ: bug / priority: **medium**
対象: `scripts/check_unused_excluding_tests.sh` の存在ガード (`command -v staticcheck`)
出典: [issue 404](done/404-retro-av1ify-vfr-six-rounds-and-lockman-364-2026-09-19.md) の残課題
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

## 🚨 この issue の一般化 (同型が他にもあるはず)

**`command -v` は「PATH に名前が在る」しか見ない**。version manager の shim・ラッパー・
`alias` 越しでは「在るのに実行できない」が普通に起きる。実行可否を見たいなら
**実際に無害なサブコマンド (`--version`) を走らせて rc を見る**のが正しい。
[`verify-execution-not-just-exit-code.md`](../_claude/rules/verify-execution-not-just-exit-code.md)
の「exit code 0 は『失敗しなかった』であり『そもそも走らなかった』を含む」の存在検査版。
横展開の結果しだいでは、ルールへ 1 行足す価値がある。
