# ratelimit (Go) の静的解析とテスト。root Makefile の test-go-lint / test-go と CI
# (.github/workflows/src_ratelimit.yml) から `make -C src/ratelimit lint|test` として呼ばれる
# 自己完結ターゲット。golangci-lint はインストール不要で、scripts/golangci_lint.sh が版を固定して一度だけビルドし起動する。
# バージョンは .golangci.yml (version: "2") と整合させること。
GOLANGCI_LINT_VERSION := v2.5.0

.PHONY: lint test vet-gorules

lint: vet-gorules
	../../scripts/golangci_lint.sh $(GOLANGCI_LINT_VERSION) run ./...

# gorules/ は build tag で通常ビルドから外れるので、ここで型検査する (src/glogx/Makefile と同じ理由)
vet-gorules:
	go vet -tags ruleguard ./gorules

test:
	go test -race ./...
