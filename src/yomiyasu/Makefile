# yomiyasu (Go) の静的解析とテスト。root Makefile の test-go-lint / test-go と CI
# (.github/workflows/src_yomiyasu.yml) から `make -C src/yomiyasu lint|test` として呼ばれる自己完結ターゲット。
# golangci-lint はインストール不要で、scripts/golangci_lint.sh が版を固定して一度だけビルドし起動する。
# バージョンは src/.golangci.yml (version: "2") と整合させること。
GOLANGCI_LINT_VERSION := v2.15.0

.PHONY: lint test golden pin-upstream

lint:
	../../scripts/go_module_graph.sh
	../../scripts/golangci_lint.sh $(GOLANGCI_LINT_VERSION) run ./...

# 全パターンの記録との突き合わせ (internal/pyre の patterns_test.go) は -race を付けると時間の上限を超えるので build tag で race から外し、race なしで別に回す。
# 走った証拠として成功の行が出たことも見る (テストの名前を変えて 0 件実行になっても rc=0 になるため)。
test:
	go test -race ./...
	@out=$$(go test -count=1 -v -run '^TestPatternsMatchPythonRecording$$' ./internal/pyre/ 2>&1); rc=$$?; echo "$$out" | tail -n 4; \
	  if [ $$rc -ne 0 ] || ! echo "$$out" | grep -q 'Python と一致'; then echo "パターンの記録との突き合わせが走っていないか落ちた (rc=$$rc)"; exit 1; fi

# 本家 (Python) を環境を固定して動かし、testdata/golden/ の記録を作り直す。手で打つ (test には含めない。Python が要る)。
# 記録の Python / Unicode のバージョンか本家の commit が今と違うときは止まる (UPDATE=1 のときだけ上書き)。
golden:
	go run ./oracle/cmd/golden

# 本家のファイルを固定の commit から取り直し、oracle/upstream.sha256 を書く (commit の前に差分を読む)。
pin-upstream:
	go run ./oracle/cmd/golden -pin
