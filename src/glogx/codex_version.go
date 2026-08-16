package main

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"glogx/usage"
)

// codex CLI の新バージョン検出。仕組みは claude 側 (claude_version.go) の完全な鏡像で、
// 共通フローは checkCLIVersionCmd に集約済み。更新の実行は X (runCodexUpdate)。
// 取得失敗 (未インストール / オフライン / 出力形式変更) はすべて無音でスキップする。

// codexUpdateAvailableMsg は「インストール済みより新しい codex が公開されている」合図。
type codexUpdateAvailableMsg struct{ latest string }

// codexVersionCacheFile は最新バージョン照会結果のキャッシュ (TTL は claudeVersionTTL を共用)。
const codexVersionCacheFile = "codex-latest-version.json"

// npmCodexLatestURL は照会先。ローカルの codex は npm (@openai/codex) 配布 (実測 2026-08-09)。
const npmCodexLatestURL = "https://registry.npmjs.org/@openai/codex/latest"

// fetchLatestCodexVersion はテストで実ネットワークに触れないための差し替え点。
var fetchLatestCodexVersion = func(ctx context.Context) string {
	return fetchNpmLatestVersion(ctx, npmCodexLatestURL)
}

// fetchInstalledCodexVersion はテストで codex CLI を起動しないための差し替え点
// (claude 側 fetchInstalledClaudeVersion と同じく実体は usage パッケージ。usage オーバーレイの
// タイトル表示でも同じ取得が要るため、exec とパースを二重に持たない)。
var fetchInstalledCodexVersion = usage.FetchCodexVersion

// checkCodexVersionCmd は起動時の codex バージョン確認 1 回分 (claude 側と並ぶ)。
func checkCodexVersionCmd() tea.Cmd {
	return checkCLIVersionCmd(codexVersionCacheFile, fetchLatestCodexVersion, fetchInstalledCodexVersion,
		func(latest string) tea.Msg { return codexUpdateAvailableMsg{latest: latest} })
}
