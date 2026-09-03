package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"glogx/usage"
	"termsafe"
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

// loadUsageCache は fresh かつキャッシュ契約を満たすスナップショットを返す。契約は
// Claude 枠が必須、codex 枠は best-effort。欠損・破損・TTL 切れ・Claude 枠なしは
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
	// FetchAll は Claude 失敗 + codex 成功を err=nil の部分スナップショットとして返し得る。
	// それを起動時キャッシュとして採用せず、Claude 枠を取り直せるよう miss にする。
	if entry.Snapshot == nil || !entry.Snapshot.HasClaude() {
		return nil, false
	}
	// 逆側の部分スナップショット (Claude 成功 + codex 失敗) も、codex CLI が入っている環境
	// では miss にする: codex app-server の一時失敗 (起動失敗等) が TTL の間キャッシュされ、
	// U を押しても codex 行が出ない「失敗の固定化」になっていた (ユーザー報告 2026-08-09)。
	// codex 未インストールの環境は codex 欠損が定常なので miss にしない (キャッシュの意味が
	// 消えて毎起動 claude subprocess ~2s を払うことになる)。codex が入っているのに常時失敗
	// する環境 (未ログイン等) では同じ理由でキャッシュが効かなくなるが、取得は起動の
	// クリティカルパス外の background なので、失敗の固定化より安い方を取る
	if !entry.Snapshot.HasCodex() {
		if _, err := lookPathFn("codex"); err == nil {
			return nil, false
		}
	}
	// age < 0 (未来) も取り直す (issue 201)
	if age := now.Sub(entry.FetchedAt); age < 0 || age >= usageCacheTTL {
		return nil, false
	}
	// 🚨 **表示に載る文字列は入口で 1 回 termsafe を通す** (src/glogx/CLAUDE.md の規律。issue 230)。
	// このファイルは一般ユーザー権限で書き換えられる。live の取得経路は安全 (Claude は
	// defaultOrder の完全一致でしか描かれず、codex の Label は分から組み立てる) だが、
	// **codex 枠は Source で拾う**ので、キャッシュに書かれた Label がそのまま
	// RenderLine / RenderTableGroups / RenderDashboard の 3 経路へ出る (敵対レビューが再現)。
	// 出所ごとに書き分けると漏れるので、復元直後にここで閉じる
	for i := range entry.Snapshot.Windows {
		entry.Snapshot.Windows[i].Label = termsafe.PlainLine(entry.Snapshot.Windows[i].Label)
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
