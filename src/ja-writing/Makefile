# ja-writing (Go) の静的解析とテスト。root Makefile の test-go-lint / test-go と CI
# (.github/workflows/src_ja-writing.yml) から `make -C src/ja-writing lint|test` として呼ばれる自己完結ターゲット。
# golangci-lint はインストール不要で、scripts/golangci_lint.sh が版を固定して一度だけビルドし起動する。
# バージョンは src/.golangci.yml (version: "2") と整合させること。
GOLANGCI_LINT_VERSION := v2.15.0

.PHONY: lint test golden pin-upstream

# deadcode_check.sh は、製品の package にどの main からも届かない関数 (テストからだけ呼ぶ export の入口など) が無いことを見る。
# golangci-lint の unused も repo の unused の検査も、export した名前を報告しない。
lint:
	../../scripts/go_module_graph.sh
	../../scripts/golangci_lint.sh $(GOLANGCI_LINT_VERSION) run ./...
	./deadcode_check.sh

# 記録との突き合わせの重い検査 (internal/pyre の全パターン・lint の CLI と検出の記録・diff の関数の記録と資料との照合の記録) は -race を付けると時間の上限を超えるので
# build tag で race から外し、race なしで別に回す。どれも 1 つの goroutine で順に呼ぶだけで、race で見つけられるものが無い
# (hook の締め切りの goroutine とタイマーは、記録でなく check/hook_test.go が race つきで見る)。
# race なしで回すテストは norace_tests.sh が tag のファイルから集める (手書きの一覧を置かない。一覧は tag のファイルにテストを足すたびに漏れる)。
test:
	go test -race ./...
	./norace_tests.sh

# 本家 (Python) を環境を固定して動かし、testdata/golden/ の記録を作り直す。手で打つ (test には含めない。Python が要る)。
# 記録の Python / Unicode のバージョンか本家の commit が今と違うときは止まる (UPDATE=1 のときだけ上書き)。
golden:
	go run ./oracle/cmd/golden

# 本家のファイルを固定の commit から取り直し、oracle/upstream.sha256 を書く (commit の前に差分を読む)。
pin-upstream:
	go run ./oracle/cmd/golden -pin
