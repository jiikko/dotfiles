# glog (Go) の静的解析とテスト。root Makefile の test-go-lint / test-go と CI
# (.github/workflows/src_glog.yml) から `make -C src/glog lint|test` として呼ばれる
# 自己完結ターゲット。golangci-lint はインストール不要で go run 経由・バージョン固定。
# 設定はこのディレクトリの .golangci.yml を自動で読む。
# バージョンは .golangci.yml (version: "2") と整合させること。
GOLANGCI_LINT_VERSION := v2.5.0

.PHONY: lint test

lint: vet-gorules
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run ./...

# gorules/ は build tag で通常ビルドから外れるので、golangci-lint も go build も型検査しない。
# ここで明示的に vet を回さないと、ルールに型エラーを書いても CI は緑のままになる (issue 202)。
# ⚠️ gorules/ を glogx 直下へ戻すと package 名衝突でこの vet 自体が落ちる (それが元の状態だった)。
vet-gorules:
	go vet -tags ruleguard ./gorules

test:
	go test -race ./...
