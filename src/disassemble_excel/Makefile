# disassemble_excel (Go) の静的解析とテスト。root Makefile の test-go-lint / test-go と
# CI (.github/workflows/src_disassemble_excel.yml) から `make -C src/disassemble_excel lint|test`
# として呼ばれる自己完結ターゲット。golangci-lint はインストール不要で go run 経由・バージョン固定。
# .golangci.yml は置いておらず既定 linter で運用 (parallel-each と同じ。カスタム lint が
# 必要になったら glogx の .golangci.yml を参照)。
GOLANGCI_LINT_VERSION := v2.5.0

.PHONY: lint test

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run ./...

test:
	go test ./...
