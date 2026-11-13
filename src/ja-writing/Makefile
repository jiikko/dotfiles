# ja-writing (Go) の静的解析とテスト。root Makefile の test-go-lint / test-go と CI
# (.github/workflows/src_ja-writing.yml) から `make -C src/ja-writing lint|test` として呼ばれる自己完結ターゲット。
# golangci-lint はインストール不要で、scripts/golangci_lint.sh が版を固定して一度だけビルドし起動する。
# バージョンは src/.golangci.yml (version: "2") と整合させること。
GOLANGCI_LINT_VERSION := v2.15.0

.PHONY: lint test golden pin-upstream

lint:
	../../scripts/go_module_graph.sh
	../../scripts/golangci_lint.sh $(GOLANGCI_LINT_VERSION) run ./...

# 記録との突き合わせの重い検査 (internal/pyre の全パターン・lint の CLI と検出の記録・diff の関数の記録) は -race を付けると時間の上限を超えるので
# build tag で race から外し、race なしで別に回す。どれも 1 つの goroutine で順に呼ぶだけで、race で見つけられるものが無い。
# 走った証拠として、それぞれの成功の行が出たことも見る (テストの名前を変えて 0 件実行になっても rc=0 になるため)。
NORACE_CHECKS := ./internal/pyre/:TestPatternsMatchPythonRecording ./lint/:TestCLIMatchesPythonRecording ./lint/:TestRawFindingsMatchPythonRecording ./diff/:TestBaseMatchesPythonRecording

test:
	go test -race ./...
	@for c in $(NORACE_CHECKS); do pkg=$${c%%:*}; name=$${c##*:}; \
	  out=$$(go test -count=1 -v -run "^$$name\$$" $$pkg 2>&1); rc=$$?; echo "$$out" | tail -n 3; \
	  if [ $$rc -ne 0 ] || ! echo "$$out" | grep -q -- "--- PASS: $$name "; then echo "$$name ($$pkg) が走っていないか落ちた (rc=$$rc)"; exit 1; fi; \
	done

# 本家 (Python) を環境を固定して動かし、testdata/golden/ の記録を作り直す。手で打つ (test には含めない。Python が要る)。
# 記録の Python / Unicode のバージョンか本家の commit が今と違うときは止まる (UPDATE=1 のときだけ上書き)。
golden:
	go run ./oracle/cmd/golden

# 本家のファイルを固定の commit から取り直し、oracle/upstream.sha256 を書く (commit の前に差分を読む)。
pin-upstream:
	go run ./oracle/cmd/golden -pin
