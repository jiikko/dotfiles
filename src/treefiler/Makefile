# treefiler (Go) の静的解析とテスト。root Makefile の test-go-lint / test-go と CI
# (.github/workflows/src_treefiler.yml) から `make -C src/treefiler lint|test` として呼ばれる
# 自己完結ターゲット。golangci-lint はインストール不要で、scripts/golangci_lint.sh が版を固定して一度だけビルドし起動する。
# バージョンは src/.golangci.yml (version: "2") と整合させること。
GOLANGCI_LINT_VERSION := v2.15.0

.PHONY: lint test vet-gorules probe perf

lint: vet-gorules
	../../scripts/go_module_graph.sh
	../../scripts/golangci_lint.sh $(GOLANGCI_LINT_VERSION) run ./...

# gorules/ は build tag で通常ビルドから外れるので、ここで型検査する (src/glogx/Makefile と同じ理由)
vet-gorules:
	go vet -tags ruleguard ./gorules

test:
	go test -race ./...
	# 確保の回数の予算 (filer/alloc_budget_test.go) は -race の計装で揺れるので、-race なしでも回す。
	# 仮想化の乱数の操作列のテスト (filer/virt_test.go) は -race で 7 倍遅く (4 秒 → 28 秒)、描画の計算を比べるだけなので -race なしで回す
	go test -count=1 -run '^(TestFrameAllocBudget|TestVirtualizedRandomOpsMatchFull)$$' ./filer/

# 実物のディレクトリでヘッドレスに台本を回し、操作ごとの所要と資源の推移を測る (tools/tfprobe。読み方は CLAUDE.md の「実物で測る」)。
# 結果は本体の checkout の tmp/ へ (worktree の tmp は worktree と一緒に消える)。例: make probe ROOT=~/Downloads CYCLES=10 ARGS=-cpuprofile
ROOT ?= $(abspath ..)
CYCLES ?= 5
OUT ?= $(abspath $(shell git rev-parse --git-common-dir)/..)/tmp/tfprobe-$(shell date +%Y%m%d-%H%M%S)
probe:
	go run ./tools/tfprobe -root $(ROOT) -out $(OUT) -cycles $(CYCLES) $(ARGS)

# 実物の bin/treefiler を隔離した tmux で起こし、打鍵から端末への書き出しまでを測る (tools/tfperf。読み方は CLAUDE.md の「打鍵の所要を実物のバイナリで測る」)。
# 直す前後を比べるときは、直す前のバイナリを BIN= で渡す。例: make perf ROOT=~/Downloads KEYS='j*40 k*40'
KEYS ?= j*30 k*30
PERF_OUT ?= $(abspath $(shell git rev-parse --git-common-dir)/..)/tmp/tfperf-$(shell date +%Y%m%d-%H%M%S)
BIN ?= $(PERF_OUT)/treefiler
perf:
	mkdir -p $(PERF_OUT)
	[ "$(BIN)" != "$(PERF_OUT)/treefiler" ] || go build -o $(BIN) .
	go run ./tools/tfperf run -bin $(BIN) -root $(ROOT) -out $(PERF_OUT) -keys '$(KEYS)' $(ARGS)
