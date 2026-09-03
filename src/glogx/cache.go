package main

import (
	"doctor/cachedir"

	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// CI 結果のローカルキャッシュ。初回表示の体感速度と API rate 消費の抑制が目的 (issue の設計)。
// CI は再実行されうるため、完了状態 (success/failure) も永久キャッシュにはしない。

// cacheEntry は SHA 1 件分のキャッシュ。
type cacheEntry struct {
	State     CIState   `json:"state"`
	FetchedAt time.Time `json:"fetchedAt"`
}

type cacheFile struct {
	Statuses map[string]cacheEntry `json:"statuses"`
}

// maxCacheEntries はキャッシュファイルのエントリ数の上限 (超過分は取得時刻の新しい順に
// 残す)。TTL 切れの間引きと合わせた二段構えで、ファイルが膨れ続けないことを保証する。
const maxCacheEntries = 2000

// cacheTTL は状態ごとの有効期間 (issue の TTL 表)。
func cacheTTL(state CIState) time.Duration {
	switch state {
	case StateSuccess, StateFailure:
		return 24 * time.Hour
	case StateNeutral:
		return time.Hour
	case StatePending:
		return 10 * time.Second
	case StateNone:
		return 5 * time.Minute
	default: // unknown (API エラー含む)
		return 30 * time.Second
	}
}

func (e cacheEntry) fresh(now time.Time) bool {
	// age < 0 (FetchedAt が未来) は fresh にしない。時計を戻した / NTP の大補正 /
	// 別マシンのキャッシュを持ってきたときに、古い CI 状態を**永久に**使い続けるのを防ぐ
	// (issue 201。doctor 側は既に同じ規律を持っており、ここが非対称だった)。
	age := now.Sub(e.FetchedAt)
	return age >= 0 && age < cacheTTL(e.State)
}

// cacheBaseDir は glogx のキャッシュ置き場 ($XDG_CACHE_HOME/glog、未設定時は ~/.cache/glog)。
// CI キャッシュと claude バージョンキャッシュ (claude_version.go) で共有する。
// 実体は doctor module の cachedir (doctor のスキャン結果も同じ置き場に保存するため。issue 148)。
func cacheBaseDir() (string, error) { return cachedir.Base() }

// CachePath はリポジトリごとのキャッシュファイルパス。
// $XDG_CACHE_HOME/glog/github.com/<owner>/<name>.json (未設定時は ~/.cache/glog/...)。
func CachePath(repo Repo) (string, error) {
	base, err := cacheBaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "github.com", repo.Owner, repo.Name+".json"), nil
}

// LoadCache は fresh なエントリだけを返す。ファイル欠損・破損は「キャッシュなし」に落とす
// (キャッシュ都合でコマンドを失敗させない)。
func LoadCache(path string, now time.Time) map[string]CIState {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]CIState{}
	}
	var file cacheFile
	if err := json.Unmarshal(data, &file); err != nil {
		return map[string]CIState{}
	}
	statuses := make(map[string]CIState, len(file.Statuses))
	for sha, entry := range file.Statuses {
		if entry.fresh(now) {
			statuses[sha] = entry.State
		}
	}
	return statuses
}

// SaveCache は取得結果を既存キャッシュへマージして原子的に書き込む (temp + rename)。
// unknown (取得失敗) も保存する — 30 秒 TTL の負キャッシュとして働く (下の保存ループの
// コメント参照)。TTL 切れのエントリは LoadCache が無視するだけの死データなので保存時に間引く
// (最長 TTL が 24h のため、ファイルは常に直近 1 日分程度に収まり膨れ続けない)。
func SaveCache(path string, fetched map[string]CIState, now time.Time) error {
	var file cacheFile
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &file) // 破損していたら作り直す
	}
	if file.Statuses == nil {
		file.Statuses = map[string]cacheEntry{}
	}
	for sha, entry := range file.Statuses {
		if !entry.fresh(now) {
			delete(file.Statuses, sha)
		}
	}
	// unknown (取得失敗) も保存する。TTL 30 秒の負キャッシュとして働き、API 障害中に
	// 実行のたび 10 秒 timeout を繰り返すのを防ぐ (issue の TTL 表「API error 30秒」)
	for sha, state := range fetched {
		file.Statuses[sha] = cacheEntry{State: state, FetchedAt: now}
	}
	pruneToLimit(file.Statuses)
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, data)
}

// pruneToLimit はエントリ数を maxCacheEntries に抑える (取得時刻の新しい順に残す)。
func pruneToLimit(statuses map[string]cacheEntry) {
	if len(statuses) <= maxCacheEntries {
		return
	}
	type entryWithSHA struct {
		sha string
		at  time.Time
	}
	entries := make([]entryWithSHA, 0, len(statuses))
	for sha, entry := range statuses {
		entries = append(entries, entryWithSHA{sha: sha, at: entry.FetchedAt})
	}
	slices.SortFunc(entries, func(a, b entryWithSHA) int {
		return b.at.Compare(a.at) // 新しい順
	})
	for _, e := range entries[maxCacheEntries:] {
		delete(statuses, e.sha)
	}
}

// writeAtomic は temp + rename で書く。write / Close / rename のどの分岐で失敗しても temp を掃除する。
//
// temp の名前は `<元のファイル名>.tmp.<乱数>` で、**この中で導出する**。
// 🚨 **pattern を引数にしない** (issue 219 の敵対レビューで実測): 引数にすると
// 「production が作る名前」と「テストが glob する名前」が別々のリテラルになり、
// 呼び出し側の 1 行を書き換えるだけで残骸テストが無言で vacuous になる
// (rename 失敗で temp が実際に残る変異を当てても、スイート全体が緑のまま通った)。
// 出所が読める名前という要求は、導出でも同じだけ満たせる。
//
// 掃除する道具を作るなら、writeAtomic 経由の全経路は **再帰の** `**/*.tmp.*` で当たる
// (CI キャッシュは `<base>/github.com/<owner>/<name>.json` なので top level の glob には
// 当たらない)。`doctor-history` の `.<乱数>.tmp` (src/doctor/disk/delete.go) と
// parallel-each は命名が別なので、別の glob が要る。
//
// 🚨 **閉じるのは error-return 経路だけ**。CreateTemp と Remove の間で SIGKILL / panic した
// 残骸はこの実装でも残る (issue 219)。
// ⚠️ **Close 分岐の掃除は変異検証の射程外**: RLIMIT_FSIZE では Close 失敗を作れないため、
// この分岐の os.Remove を外してもどのテストも赤くならない (issue 219 で実測)。
func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
