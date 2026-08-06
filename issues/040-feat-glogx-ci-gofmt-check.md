# glogx: CI に gofmt チェックを追加する (golangci-lint v2 の formatters が未設定)

## 現状 (2026-08-06 確認)

glogx の CI (`_go-project.yml` → `make -C src/glogx lint`) は golangci-lint v2.5.0 を回しているが、v2 では gofmt / gofumpt は linter ではなく **formatters** として別枠になっており、`src/glogx/.golangci.yml` に `formatters:` セクションがないため **fmt チェックは CI で走っていない**。

現時点で `gofmt -l .` の差分はゼロ (整形自体は守られている) ので、これは「強制の欠落」のみの issue。

## 提案

`.golangci.yml` に formatters を追加する (最小):

```yaml
formatters:
  enable:
    - gofmt
```

golangci-lint v2 の `run` は formatters の差分を issue として報告するため、`make lint` / CI がそのまま fmt ゲートになる。

## 対象範囲

同じ `.golangci.yml` 構成を持つ src/ 配下の他 Go プロジェクト (parallel-each, disassemble_excel) も同様の欠落がないか着手時に確認し、あれば揃えて直す (同じ間違いの横断確認)。

## 注意

- codex レビューはユーザー方針 (codex 不使用) により省略
