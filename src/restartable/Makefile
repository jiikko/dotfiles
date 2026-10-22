# restartable (Go) の静的解析とテスト。
GOLANGCI_LINT_VERSION := v2.15.0

.PHONY: lint test

lint:
	../../scripts/go_module_graph.sh
	../../scripts/golangci_lint.sh $(GOLANGCI_LINT_VERSION) run ./...

test:
	go test -race ./...
