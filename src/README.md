# src/ — 自作ツールのソースコード

各サブディレクトリが 1 つの独立したプロジェクト（現在はすべて Go）。使い方・設計は各プロジェクトの README を参照。シェルからの入口は `bin/` のラッパが担う。

## 新規プロジェクトのガイドライン

プロジェクトを追加するときは、以下の **3 点セット**を必ず揃える。どれか欠けると lint / test がローカルまたは CI から漏れる（disassemble_excel はこれが無かったためテスト 6 ファイルが死蔵していた実例あり）。

1. **プロジェクト直下に Makefile（`lint` / `test` ターゲット必須）**
   root の `make test-go-lint` / `make test-go` と CI が `make -C src/<name> lint|test` として呼ぶ契約。実装言語が Go 以外になっても、この 2 ターゲットの契約だけは維持する
2. **root [Makefile](../Makefile) の `GO_PROJECT_DIRS` に登録**
   ローカルの `make test`（コミット前検証）に組み込まれる
3. **`.github/workflows/src_<name>.yml` を作成**
   paths filter 付きの専用 workflow で lint / test を回す（プロジェクトに触れた push だけで起動）。
   ⚠️ paths filter 付き check を branch protection の **required check に登録しないこと**（非接触 PR では run が生成されず、check が永遠に pending になる）

補足:

- `.golangci.yml` は任意（無ければ既定 linter で運用。カスタム lint の実例は glogx を参照）
- golangci-lint はインストール不要（Makefile が `go run` 経由でバージョン固定実行）
- テストが「重い / 環境依存」に思えても、CI から除外する前に**実測**すること（parallel-each は「TUI 依存で重い」とされていたが実測 8.7s で CI 投入できた）

## Template

下のテンプレは実物（[glogx/Makefile](glogx/Makefile)・[src_glogx.yml](../.github/workflows/src_glogx.yml) 等）の写し。乖離していたら実物を正としてこちらを直す。

### src/&lt;name&gt;/Makefile

```make
# <name> (Go) の静的解析とテスト。root Makefile の test-go-lint / test-go と CI
# (.github/workflows/src_<name>.yml) から `make -C src/<name> lint|test` として呼ばれる
# 自己完結ターゲット。golangci-lint はインストール不要で go run 経由・バージョン固定。
GOLANGCI_LINT_VERSION := v2.5.0

.PHONY: lint test

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run ./...

test:
	go test ./...
```

### .github/workflows/src_&lt;name&gt;.yml

```yaml
---
name: src/<name>

on:
  push:
    branches: [master]
    paths:
      - 'src/<name>/**'
      - .github/workflows/src_<name>.yml
  pull_request:
    paths:
      - 'src/<name>/**'
      - .github/workflows/src_<name>.yml
  workflow_dispatch:  # GitHub UI からの手動再実行用

permissions:
  contents: read

# 同一 ref の連続 push では古い run を打ち切る (旧コミットの結果に価値が無いため)
concurrency:
  group: ${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: true

jobs:
  # lint と test は独立 job: 並列実行 + 片方だけの再実行が可能
  lint:
    runs-on: ubuntu-slim
    # 初回は golangci-lint を go run でソースからビルドするため余裕を持たせる (以降は cache)
    timeout-minutes: 15
    steps:
      - name: Silence git init.defaultBranch hint
        run: git config --global init.defaultBranch main

      - uses: actions/checkout@v7

      - name: Set up Go
        uses: actions/setup-go@v6
        with:
          go-version-file: src/<name>/go.mod
          cache-dependency-path: src/<name>/go.sum

      - name: Lint
        run: make -C src/<name> lint

  test:
    runs-on: ubuntu-slim
    timeout-minutes: 10
    steps:
      - name: Silence git init.defaultBranch hint
        run: git config --global init.defaultBranch main

      - uses: actions/checkout@v7

      - name: Set up Go
        uses: actions/setup-go@v6
        with:
          go-version-file: src/<name>/go.mod
          cache-dependency-path: src/<name>/go.sum

      - name: Test
        run: make -C src/<name> test
```
