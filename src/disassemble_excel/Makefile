# disassemble_excel (Go) の静的解析とテスト。root Makefile の test-go-lint / test-go と
# CI (.github/workflows/src_disassemble_excel.yml) から `make -C src/disassemble_excel lint|test`
# として呼ばれる自己完結ターゲット。golangci-lint はインストール不要で、scripts/golangci_lint.sh が版を固定して一度だけビルドし起動する。
# .golangci.yml は最小構成 (既定 linter + gofmt ゲートのみ。カスタム lint が
# 必要になったら glogx の .golangci.yml を参照)。
GOLANGCI_LINT_VERSION := v2.5.0

.PHONY: lint test

lint:
	../../scripts/golangci_lint.sh $(GOLANGCI_LINT_VERSION) run ./...

test:
	go test -race ./...
