package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"glogx/usage"
)

// Claude Code CLI の新バージョン検出 (issue 024)。起動時にバックグラウンドで最新公開バージョン
// と比較し、更新可能ならトーストで知らせるだけの機能。更新の実行は既存の C (claude update)。
// バージョン通知は付加情報なので、取得失敗 (オフライン / タイムアウト / パース不能) はすべて
// 無音でスキップする (usage.FetchVersion の「欠けても主処理は成立」と同じ方針)。

// claudeUpdateAvailableMsg は「インストール済みより新しいバージョンが公開されている」合図。
type claudeUpdateAvailableMsg struct{ latest string }

// claudeVersionTTL は最新バージョン取得結果のキャッシュ有効期間。リリース頻度に対して
// 1 時間あれば十分新鮮で、起動のたびに registry へ問い合わせない (issue 024 の要件)。
const claudeVersionTTL = time.Hour

// claudeVersionFetchTimeout は registry への HTTP と claude --version を合わせた上限。
// 起動直後のバックグラウンド処理であり遅延しても気づかれないが、goroutine を長く残さない。
const claudeVersionFetchTimeout = 5 * time.Second

// claudeVersionCacheFile は claudeVersionCachePath のファイル部。リポジトリ非依存なので
// CI キャッシュ (github.com/<owner>/<name>.json) とは別階層に置く。
const claudeVersionCacheFile = "claude-latest-version.json"

type claudeVersionCache struct {
	Latest    string    `json:"latest"`
	FetchedAt time.Time `json:"fetchedAt"`
}

func claudeVersionCachePath() (string, error) {
	base, err := cacheBaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, claudeVersionCacheFile), nil
}

// npmLatestURL は最新公開バージョンの照会先。npm registry の dist-tags は native installer
// 配布と同一バージョン系列なので比較指標として有効 (issue 024)。`npm view` の exec より依存
// (npm の有無) が少なく、stdlib だけで足りる。
const npmLatestURL = "https://registry.npmjs.org/@anthropic-ai/claude-code/latest"

// fetchLatestClaudeVersion はテストで実ネットワークに触れないための差し替え点。
// 失敗はすべて空文字 (無通知に落とす)。
var fetchLatestClaudeVersion = func(ctx context.Context) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, npmLatestURL, nil)
	if err != nil {
		return ""
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	// レスポンスは package manifest 1 件分 (数 KB)。上限を張って異常応答で膨れないようにする。
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ""
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &manifest); err != nil {
		return ""
	}
	return strings.TrimSpace(manifest.Version)
}

// fetchInstalledClaudeVersion はテストで claude CLI を起動しないための差し替え点。
var fetchInstalledClaudeVersion = func(ctx context.Context) string {
	return usage.FetchVersion(ctx)
}

// versionLess は "2.1.216" 形式の 3 セグメント数値比較で a < b を返す。semver ライブラリは
// 入れない (pre-release 等は claude の配布に現れず、必要になったら再評価)。パース不能・
// セグメント数不一致は false (= 通知しない) に倒す。
func versionLess(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	if len(as) != 3 || len(bs) != 3 {
		return false
	}
	for i := range as {
		an, errA := strconv.Atoi(as[i])
		bn, errB := strconv.Atoi(bs[i])
		if errA != nil || errB != nil {
			return false
		}
		if an != bn {
			return an < bn
		}
	}
	return false
}

// loadClaudeVersionCache は fresh なキャッシュ値を返す。欠損・破損・TTL 切れは「なし」。
func loadClaudeVersionCache(path string, now time.Time) (latest string, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var c claudeVersionCache
	if err := json.Unmarshal(data, &c); err != nil {
		return "", false
	}
	if c.Latest == "" || now.Sub(c.FetchedAt) >= claudeVersionTTL {
		return "", false
	}
	return c.Latest, true
}

func saveClaudeVersionCache(path, latest string, now time.Time) error {
	data, err := json.MarshalIndent(claudeVersionCache{Latest: latest, FetchedAt: now}, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, data)
}

// checkClaudeVersionCmd は起動時のバージョン確認 1 回分。キャッシュが fresh なら registry へ
// 出ない。インストール済みの取得 (claude --version の exec) は「比較対象の latest が手に
// 入った後」だけ実行する — latest が取れない状況 (オフライン等) で無駄に node プロセスを
// 起動しないため。全体がバックグラウンドの tea.Cmd (goroutine) で走り、初期描画の
// クリティカルパスには乗らない。通知不要ならば nil Msg (bubbletea が無視する)。
func checkClaudeVersionCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), claudeVersionFetchTimeout)
		defer cancel()
		now := time.Now()
		path, err := claudeVersionCachePath()
		if err != nil {
			return nil
		}
		latest, cached := loadClaudeVersionCache(path, now)
		if !cached {
			latest = fetchLatestClaudeVersion(ctx)
			if latest == "" {
				return nil // 取得失敗はキャッシュも更新しない (次回起動で再試行)
			}
			_ = saveClaudeVersionCache(path, latest, now) // 保存失敗しても通知自体は成立させる
		}
		installed := fetchInstalledClaudeVersion(ctx)
		if installed == "" || !versionLess(installed, latest) {
			return nil
		}
		return claudeUpdateAvailableMsg{latest: latest}
	}
}
