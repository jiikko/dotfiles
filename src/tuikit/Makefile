# tuikit (Go) の静的解析とテスト。root Makefile の test-go-lint / test-go と CI
# (.github/workflows/src_tuikit.yml) から `make -C src/tuikit lint|test` として呼ばれる
# 自己完結ターゲット。golangci-lint はインストール不要で go run 経由・バージョン固定。
# バージョンは .golangci.yml (version: "2") と整合させること。
GOLANGCI_LINT_VERSION := v2.5.0

.PHONY: lint test demo

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run ./...

test:
	go test -race ./...

# 一覧 → 詳細の遷移を gif に撮る (vhs が要る。手順と前提は README の「デモ」)
demo:
	vhs examples/listdetail/demo.tape
