package main

import (
	"encoding/json"
	"os"
	"path/filepath"
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

// cacheRetention を超えて古いエントリは保存時に間引く (ファイルの無限成長防止)。
const cacheRetention = 30 * 24 * time.Hour

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
	return now.Sub(e.FetchedAt) < cacheTTL(e.State)
}

// CachePath はリポジトリごとのキャッシュファイルパス。
// $XDG_CACHE_HOME/glog/github.com/<owner>/<name>.json (未設定時は ~/.cache/glog/...)。
func CachePath(repo Repo) (string, error) {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "glog", "github.com", repo.Owner, repo.Name+".json"), nil
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
// unknown は「取得できなかった」事実であって観測結果ではないため保存しない。
func SaveCache(path string, fetched map[string]CIState, now time.Time) error {
	var file cacheFile
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &file) // 破損していたら作り直す
	}
	if file.Statuses == nil {
		file.Statuses = map[string]cacheEntry{}
	}
	for sha, entry := range file.Statuses {
		if now.Sub(entry.FetchedAt) > cacheRetention {
			delete(file.Statuses, sha)
		}
	}
	for sha, state := range fetched {
		if state == StateUnknown {
			continue
		}
		file.Statuses[sha] = cacheEntry{State: state, FetchedAt: now}
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".glog-cache-*")
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
	return os.Rename(tmpName, path)
}
