# schedkeys (Go) の静的解析とテスト。root Makefile の test-go-lint / test-go と CI
# (.github/workflows/src_schedkeys.yml) から `make -C src/schedkeys lint|test` として呼ばれる
# 自己完結ターゲット。golangci-lint はインストール不要で、scripts/golangci_lint.sh が版を固定して一度だけビルドし起動する。
GOLANGCI_LINT_VERSION := v2.5.0

.PHONY: lint test

lint:
	../../scripts/golangci_lint.sh $(GOLANGCI_LINT_VERSION) run ./...

test:
	go test -race ./...
