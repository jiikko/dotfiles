# parallel-each (Go) の静的解析とテスト。root Makefile の test-go-lint / test-go と CI
# (.github/workflows/src_parallel-each.yml) から `make -C src/parallel-each lint|test` として
# 呼ばれる自己完結ターゲット。golangci-lint はインストール不要で go run 経由・バージョン固定。
# .golangci.yml は置いておらず既定 linter で運用 (カスタム lint が必要になったら glog を参照)。
GOLANGCI_LINT_VERSION := v2.5.0

.PHONY: lint test

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run ./...

test:
	go test ./...
