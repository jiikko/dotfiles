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

	tea "charm.land/bubbletea/v2"

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

// npmLatestURL は最新公開バージョンの照会先。npm registry の dist-tags は native installer
// 配布と同一バージョン系列なので比較指標として有効 (issue 024)。`npm view` の exec より依存
// (npm の有無) が少なく、stdlib だけで足りる。
const npmLatestURL = "https://registry.npmjs.org/@anthropic-ai/claude-code/latest"

// fetchNpmLatestVersion は npm registry の /latest manifest から version を読む共通実装
// (claude / codex の新バージョン検出で共用)。失敗はすべて空文字 (無通知に落とす)。
func fetchNpmLatestVersion(ctx context.Context, url string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ""
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
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

// fetchLatestClaudeVersion はテストで実ネットワークに触れないための差し替え点。
var fetchLatestClaudeVersion = func(ctx context.Context) string {
	return fetchNpmLatestVersion(ctx, npmLatestURL)
}

// fetchInstalledClaudeVersion はテストで claude CLI を起動しないための差し替え点。
//
// 🚨 ここを memo 化しない (perf 監査 2026-07-25 で「起動時に claude --version が 2 回走る」
// と指摘されたが、対応しないと判断した理由):
//   - 2 回目の出典は usage.Fetch 内の並列取得 (usage/usage.go)。ただし usage 側は
//     usageCacheTTL のディスクキャッシュが載ったので、cache hit する通常経路では
//     usage.Fetch 自体が走らず重複は消える。残るのは cache miss の起動だけ。
//   - どちらもバックグラウンドの tea.Cmd で初期描画のクリティカルパスに乗らない (~160ms)。
//   - 一方で usage.FetchVersion の process 内 memo 化は壊れる: runClaudeUpdate
//     (external_commands.go) が `claude update` の前後で FetchVersion を呼び、
//     before/after の差分を表示に使う。memo があると after が更新前の値になる。
//     TTL キャッシュにしても、glogx の外で claude を更新したときに古い版を出す新しい
//     失敗モードを作る。
//
// 得るもの (cache miss 時のみ 160ms の非同期 fork 1 本) に対して払うものが大きいので現状維持。
// usage 側のキャッシュを外す / update の before-after 表示をやめる、のどちらかが起きたら再評価。
var fetchInstalledClaudeVersion = usage.FetchVersion

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
	// age < 0 (未来) も取り直す (時計の巻き戻しで古い版を永久に使い続けない。issue 201)
	if age := now.Sub(c.FetchedAt); c.Latest == "" || age < 0 || age >= claudeVersionTTL {
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

// checkCLIVersionCmd は「latest 照会 (TTL キャッシュ) → インストール済みと比較 → 新しければ
// mk(latest) を返す」の共通フロー (claude / codex で共用)。キャッシュが fresh なら registry へ
// 出ない。インストール済みの取得 (CLI --version の exec) は「比較対象の latest が手に
// 入った後」だけ実行する — latest が取れない状況 (オフライン等) で無駄にプロセスを
// 起動しないため。全体がバックグラウンドの tea.Cmd (goroutine) で走り、初期描画の
// クリティカルパスには乗らない。通知不要ならば nil Msg (bubbletea が無視する)。
func checkCLIVersionCmd(cacheFile string, fetchLatest, fetchInstalled func(context.Context) string, mk func(latest string) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), claudeVersionFetchTimeout)
		defer cancel()
		now := time.Now()
		base, err := cacheBaseDir()
		if err != nil {
			return nil
		}
		path := filepath.Join(base, cacheFile)
		latest, cached := loadClaudeVersionCache(path, now)
		if !cached {
			latest = fetchLatest(ctx)
			if latest == "" {
				return nil // 取得失敗はキャッシュも更新しない (次回起動で再試行)
			}
			_ = saveClaudeVersionCache(path, latest, now) // 保存失敗しても通知自体は成立させる
		}
		installed := fetchInstalled(ctx)
		if installed == "" || !versionLess(installed, latest) {
			return nil
		}
		return mk(latest)
	}
}

// versionCacheFileFor は target ("claude" / "codex") のバージョンキャッシュファイル名。
// 🚨 既定を claude 側に倒している: 未知の target は現状ありえない (updateMsg.target の出所は
// startUpdate / startCodexUpdate の 2 つだけ) が、増えたときに空文字を返して
// cachedLatestVersion を無言で "" にするより、明示的な既定の方が誤りに気づける。
func versionCacheFileFor(target string) string {
	if target == "codex" {
		return codexVersionCacheFile
	}
	return claudeVersionCacheFile
}

// cachedLatestVersion は起動時チェックが保存した latest (TTL 内) を返す。欠損・破損・TTL 切れ・
// キャッシュ基点が引けない場合は "" (= latest 不明)。registry へは出ない — 呼び出し元はどちらも
// 打鍵に同期した経路 (C / X の判定と結果トースト) なので、ここでネットワークを待たせない。
func cachedLatestVersion(cacheFile string) string {
	base, err := cacheBaseDir()
	if err != nil {
		return ""
	}
	latest, ok := loadClaudeVersionCache(filepath.Join(base, cacheFile), time.Now())
	if !ok {
		return ""
	}
	return latest
}

// installedIsLatest は「起動時チェックが保存した latest キャッシュ (TTL 内) とインストール済みの
// バージョンが一致 (以上) か」を返す。C / X の update 実行前の早期リターン判定 (2026-08-12):
// 既に latest と分かっているのに自己更新プロセス (npm 取得) を起動してモーダルでロックしない。
// latest 不明 (キャッシュ欠損・stale) や installed 取得失敗は false (= 従来どおり update を
// 実行) に倒す — オフラインや出力形式変更で手動 update を塞がないため。
func installedIsLatest(cacheFile string, fetchInstalled func(context.Context) string) (installed string, already bool) {
	latest := cachedLatestVersion(cacheFile)
	if latest == "" {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), claudeVersionFetchTimeout)
	defer cancel()
	installed = fetchInstalled(ctx)
	if installed == "" {
		return "", false
	}
	// 🚨 !versionLess(installed, latest) にしない: versionLess は比較不能 (セグメント数
	// 不一致・数値でない) を false に倒すため、否定すると「比較できない = 最新扱い」に
	// 反転し、形式変更のたび update が無音で塞がる (敵対レビュー指摘 2026-08-12)。
	// 「文字列一致 or latest < installed が証明できた」ときだけ skip する。
	return installed, installed == latest || versionLess(latest, installed)
}

// checkClaudeVersionCmd は起動時の Claude Code バージョン確認 1 回分。
func checkClaudeVersionCmd() tea.Cmd {
	return checkCLIVersionCmd(claudeVersionCacheFile, fetchLatestClaudeVersion, fetchInstalledClaudeVersion,
		func(latest string) tea.Msg { return claudeUpdateAvailableMsg{latest: latest} })
}
