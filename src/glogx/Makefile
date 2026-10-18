# glog (Go) の静的解析とテスト。root Makefile の test-go-lint / test-go と CI
# (.github/workflows/src_glog.yml) から `make -C src/glog lint|test` として呼ばれる
# 自己完結ターゲット。golangci-lint はインストール不要で、scripts/golangci_lint.sh が版を固定して一度だけビルドし起動する。
# 設定はこのディレクトリの .golangci.yml を自動で読む。
# バージョンは .golangci.yml (version: "2") と整合させること。
GOLANGCI_LINT_VERSION := v2.5.0

.PHONY: lint test test-budget

lint: vet-gorules
	../../scripts/go_module_graph.sh
	../../scripts/golangci_lint.sh $(GOLANGCI_LINT_VERSION) run ./...

# gorules/ は build tag で通常ビルドから外れるので、golangci-lint も go build も型検査しない。
# ここで明示的に vet を回さないと、ルールに型エラーを書いても CI は緑のままになる。
# 🚨 gorules/ を glogx 直下へ戻すと package 名衝突でこの vet 自体が落ちる (それが元の状態だった)。
vet-gorules:
	go vet -tags ruleguard ./gorules

# test-budget: 1 フレームの確保の回数は -race の計装で揺れるので、その予算だけ -race なしで判定する (バイトは test の -race で判定)
# (frame_alloc_test.go の TestFrameAllocBudget の doc)。-race 付きの run は回数の判定を飛ばす。
# -race なしの run はパッケージ全体をもう一度コンパイルし、手元の GOCACHE を 1 行の変更ごとに約 24 MB 増やす (実測) ので、
# 手元の make test では回さず、CI (GitHub Actions が環境変数 CI を立てる) でだけ回す。描画を触ったら手元でも make test-budget を回す
test:
	go test -race ./...
ifneq ($(CI),)
	$(MAKE) test-budget
endif

test-budget:
	go test -count=1 -run '^TestFrameAllocBudget$$' .
