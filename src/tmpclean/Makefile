# tmpclean (一時領域を数え、承認された古いものをゴミ箱へ移す CLI) の静的解析とテスト。root Makefile の test-go-lint / test-go と CI
# (.github/workflows/src_tmpclean.yml) から `make -C src/tmpclean lint|test` として呼ばれる自己完結ターゲット。
# golangci-lint はインストール不要で、scripts/golangci_lint.sh が版を固定して一度だけビルドし起動する。
# バージョンは src/.golangci.yml (version: "2") と整合させること。
GOLANGCI_LINT_VERSION := v2.15.0

.PHONY: lint test

lint:
	../../scripts/go_module_graph.sh
	../../scripts/golangci_lint.sh $(GOLANGCI_LINT_VERSION) run ./...

test:
	go test -race ./...
