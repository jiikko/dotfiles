#!/bin/sh
# PostToolUse(Write|Edit) フック: Claude が編集・作成した .go ファイルを gofmt -w で
# その場で整形する。
#
# なぜ: gofmt 未整形のコードが数コミット生き残った実例 (2026-07-31: glogx の tui.go 他。
# golangci-lint に formatter 系リンターが無く CI をすり抜けた)。lint 側での強制はせず
# 編集時整形で防ぐ方針 (ユーザー判断 2026-07-31: hook で fmt を走らせれば十分)。
#
# 入力: PostToolUse の hook JSON を stdin で受け取る (.tool_input.file_path を見る)
# 出力: なし。.go 以外・gofmt 不在・整形失敗はすべて静かに no-op (編集自体を妨げない)。

command -v jq >/dev/null 2>&1 || exit 0
command -v gofmt >/dev/null 2>&1 || exit 0

f=$(jq -r '.tool_input.file_path // .tool_response.filePath // empty')
case "$f" in
*.go)
  [ -f "$f" ] && gofmt -w "$f" 2>/dev/null
  ;;
esac
exit 0
