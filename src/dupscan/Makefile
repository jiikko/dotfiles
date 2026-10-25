# dupscan (Go) の静的解析とテスト。root Makefile の test-go-lint / test-go と CI
# (.github/workflows/src_dupscan.yml) から `make -C src/dupscan lint|test` として呼ばれる
# 自己完結ターゲット。golangci-lint はインストール不要で、scripts/golangci_lint.sh が版を固定して一度だけビルドし起動する。
# バージョンは src/.golangci.yml (version: "2") と整合させること。
GOLANGCI_LINT_VERSION := v2.15.0

# 入口の lint / test は serial-run の順番待ちに入る (make/serial-run.mk。CI・make -n などでは包まない)。本体は lint-all / test-all。
include $(dir $(lastword $(MAKEFILE_LIST)))../../make/serial-run.mk

.PHONY: lint lint-all test test-all

# go_module_graph.sh は秒で終わるので順番待ちの外で回す (lint: の recipe が直接呼ぶことを scripts/check_go_project_lanes.sh が見る)。
lint:
	../../scripts/go_module_graph.sh
	@+$(serial_run) $(MAKE) --no-print-directory lint-all

lint-all:
	../../scripts/golangci_lint.sh $(GOLANGCI_LINT_VERSION) run ./...

test:
	@+$(serial_run) $(MAKE) --no-print-directory test-all

test-all:
	go test -race ./...
