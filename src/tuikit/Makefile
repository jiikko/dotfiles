# tuikit (Go) の静的解析とテスト。root Makefile の test-go-lint / test-go と CI
# (.github/workflows/src_tuikit.yml) から `make -C src/tuikit lint|test` として呼ばれる
# 自己完結ターゲット。golangci-lint はインストール不要で、scripts/golangci_lint.sh が版を固定して一度だけビルドし起動する。
# バージョンは .golangci.yml (version: "2") と整合させること。
GOLANGCI_LINT_VERSION := v2.5.0

.PHONY: lint test demo

lint:
	../../scripts/golangci_lint.sh $(GOLANGCI_LINT_VERSION) run ./...

test:
	go test -race ./...

# デモの gif を撮る (一覧 → 詳細の遷移と、右下の通知。vhs が要る。手順と前提は README の「デモ」)
demo:
	vhs examples/listdetail/demo.tape
	vhs examples/toast/demo.tape
