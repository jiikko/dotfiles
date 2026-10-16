# mac-lan-manager (Go) の静的解析とテスト。root Makefile の test-go-lint / test-go と CI
# (.github/workflows/src_mac-lan-manager.yml) から `make -C src/mac-lan-manager lint|test` として呼ばれる
# 自己完結ターゲット。golangci-lint は scripts/golangci_lint.sh が版を固定して起動する (.golangci.yml の version と整合させる)。
GOLANGCI_LINT_VERSION := v2.5.0

.PHONY: lint test

lint:
	../../scripts/go_module_graph.sh
	../../scripts/golangci_lint.sh $(GOLANGCI_LINT_VERSION) run ./...

test:
	go test -race ./...
