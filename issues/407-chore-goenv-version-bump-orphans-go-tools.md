# goenv の版を上げると go 製ツールがまとめて消えるのに、入れ直す導線が無い

起票日: 2026-09-20
カテゴリ: chore / priority: low〜medium
対象: `setup.sh` / `Brewfile` / `global_go_version` の導線
出典: [405](done/405-bug-staticcheck-guard-passes-through-goenv-shim.md) の残る問題

## 事実 (2026-09-20 の実測)

goenv は **GOPATH を版ごとに分ける** (`~/go/<version>`)。1.25.4 → 1.26.0 の版上げで:

| | |
|---|---|
| `~/go/1.25.4/bin` | **17 本** (staticcheck / golangci-lint / gopls / goimports / dlv / revive / errcheck / deadcode / benchstat / godef / gotags / impl / iferr / motion / gomodifytags / fillstruct / asmfmt) |
| `~/go/1.26.0/bin` | **空** (405 で staticcheck を入れるまで) |

shim は残るので `command -v` は成功し続ける。405 はそのせいで**ガードが素通りする**バグを直したが、
**「版を上げるたびに 17 本が消える」導線そのものは手つかず**。

## 何が困るか

- `make test` は staticcheck を入れ直したので緑に戻ったが、**`gopls` / `dlv` / `goimports` は
  消えたまま** (エディタ側で効く。気づくのは補完・定義ジャンプが死んだとき)
- `golangci-lint` は各 module の Makefile が `go run …@v2.5.0` で都度取るので影響を受けていない
  = **同じツール群の中で導入経路が 3 通りに割れている** (go install / go run の pin / 未管理)
- `setup.sh` / `Brewfile` のどちらにも staticcheck は無く、「各自が `go install` する」前提

## 対応の候補 (未決)

1. **版を上げる導線と同じ場所で入れ直す**。`global_go_version` を上げる手順 / `setup.sh` に
   「現行版へ go install する一覧」を置く。CI と同じ**版 pin** を持たせる (`unused.yml` は
   `v0.7.0` を pin している)
2. **`go run …@<pin>` へ寄せる** (golangci-lint と同じ形)。都度取るので版上げの影響を受けないが、
   毎回のダウンロード / 起動が遅くなるものはエディタ用途に向かない
3. **エディタ用 (gopls / dlv) と検査用 (staticcheck 等) で分ける**。前者は版に追従させ、
   後者は pin して `go run` へ寄せる

## 受け入れ条件

- [ ] 版を上げた直後に「消えたツール」が分かる、または自動で入り直す
- [ ] 入れ直す版は CI と同じ pin を使う (手元と CI で版が割れない)
- [ ] 導入経路が何通りあるかを 1 箇所に書く (いまは go install / go run pin / 未管理の 3 通り)
