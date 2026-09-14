# doctor (Go) の静的解析とテスト。root Makefile の test-go-lint / test-go と CI
# (.github/workflows/src_doctor.yml) から `make -C src/doctor lint|test` として呼ばれる
# 自己完結ターゲット。golangci-lint はインストール不要で go run 経由・バージョン固定。
# 設定はこのディレクトリの .golangci.yml を自動で読む。
# バージョンは .golangci.yml (version: "2") と整合させること。
GOLANGCI_LINT_VERSION := v2.5.0

.PHONY: lint test test-derived-fields

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run ./...

test: test-derived-fields
	go test -race ./...

# 🚨 disk/derived_fields_bypass_test.go は **この module の外** (src/glogx のソースと
# .github/workflows/src_doctor.yml) を読む。cmd/go は testlog の open/stat のうち
# **module root の外を再チェックしない** ($GOROOT/src/cmd/go/internal/test/test.go の
# "Do not recheck files outside the module, GOPATH, or GOROOT root.") ので、
# **キャッシュキーに入力が 1 つも入らない**。doctor のソースが変わらない限り
# `ok (cached)` を返し続け、検査は 1 度も走らない。
#
# 実測 2026-09-14: glogx に本物の違反 (`disk.Report{Results: rs}`) を植えても
# `go test -race ./disk/` は `ok (cached)` で rc=0、`-count=1` を付けると FAIL。
# 🚨 **これが最も起きやすいのは、この検査が守りたい状況そのもの** — 「glogx だけを
# 触った push」では doctor のソースが変わらないので、必ずキャッシュに当たる。
#
# ここだけキャッシュを無効化し、**その検査が実際に走った証拠 (PASS 行) を確認**する
# (_claude/rules/verify-execution-not-just-exit-code.md)。テストを改名したら
# -run が空振りするので、その空振りもここで落とす。
test-derived-fields:
	@out="$$(go test -race -count=1 -v -run '^TestDerivedFieldsGoThroughOwner$$' ./disk/ 2>&1)"; \
	if ! printf '%s\n' "$$out" | grep -q '^--- PASS: TestDerivedFieldsGoThroughOwner'; then \
		printf '%s\n' "$$out"; \
		echo "✗ 導出フィールドの走査テストが PASS していない (改名したなら -run のパターンも直すこと)"; \
		exit 1; \
	fi
	@echo "[derived-fields] disk.Report/Result の導出フィールドの迂回チェック OK (キャッシュ無効で実行)"
