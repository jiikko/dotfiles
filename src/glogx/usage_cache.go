package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"glogx/usage"
)

// usage スナップショット (Claude /usage + codex rateLimits の併合結果) のディスクキャッシュ。
// glogx は git log ラッパーで 1 日に何十回も起動されるが、`claude -p /usage` は 1 回 ≈ 2.0s
// wall / 1.8s CPU かかる (実測 2026-07-25。node 起動 + Claude Code セッション初期化が支配的で、
// /usage 自身の処理は 462ms)。起動のたびに払うのは無駄なので、直近の取得結果を短 TTL で
// 再利用する。
//
// キャッシュ層をここ (package main) に置く理由: usage パッケージは「glogx / bubbletea に
// 一切依存しない自己完結」という契約を持つ (usage/usage.go の doc)。キャッシュ置き場は
// cacheBaseDir() = glogx 側の関心事なので、usage には持ち込まない。claude_version.go の
// バージョンキャッシュと同じ構図。

// usageCacheFile は usageCachePath のファイル部。リポジトリ非依存なので CI キャッシュ
// (github.com/<owner>/<name>.json) とは別階層に置く。
const usageCacheFile = "claude-usage.json"

// usageCacheTTL は表示の許容陳腐度。usageRefreshInterval と同値にしてあるのは意図的で、
// 「表示は最大 usageRefreshInterval だけ古い」という単一の契約を起動時とセッション中で
// 揃えるため (セッション中は定期リフレッシュ、起動時はこの TTL が同じ上限を与える)。
// 周期を変えたら陳腐度の上限も自動で追従する。
const usageCacheTTL = usageRefreshInterval

type usageCacheEntry struct {
	Snapshot  *usage.Snapshot `json:"snapshot"`
	FetchedAt time.Time       `json:"fetchedAt"`
}

func usageCachePath() (string, error) {
	base, err := cacheBaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, usageCacheFile), nil
}

// loadUsageCache は fresh なスナップショットを返す。欠損・破損・TTL 切れ・枠 0 件は
// 「キャッシュなし」に落とす (キャッシュ都合で表示を壊さない)。
func loadUsageCache(path string, now time.Time) (*usage.Snapshot, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var entry usageCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, false
	}
	// Windows 0 件は Fetch が error にする状態なので、キャッシュ経由でも表示に載せない
	// (usage.Snapshot の JSON 形が将来変わって黙って空デコードされた場合の受けも兼ねる)。
	if entry.Snapshot == nil || len(entry.Snapshot.Windows) == 0 {
		return nil, false
	}
	if now.Sub(entry.FetchedAt) >= usageCacheTTL {
		return nil, false
	}
	return entry.Snapshot, true
}

func saveUsageCache(path string, snap *usage.Snapshot, now time.Time) error {
	if snap == nil {
		return nil
	}
	data, err := json.MarshalIndent(usageCacheEntry{Snapshot: snap, FetchedAt: now}, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, data)
}
