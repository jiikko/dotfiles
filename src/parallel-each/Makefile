# parallel-each (Go) の静的解析。root Makefile の test-go-lint と CI
# (.github/workflows/lint.yml の parallel-each-go-lint ジョブ) から
# `make -C src/parallel-each lint` として呼ばれる自己完結ターゲット。
# golangci-lint はインストール不要で go run 経由・バージョン固定。
# 設定はこのディレクトリの .golangci.yml を自動で読む。
# バージョンは .golangci.yml (version: "2") と整合させること。
GOLANGCI_LINT_VERSION := v2.5.0

.PHONY: lint test

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run ./...

test:
	go test ./...
