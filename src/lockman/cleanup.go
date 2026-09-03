package main

import (
	"os"
	"path/filepath"
	"time"
)

// 残骸の保持期間。閾値は「進行中の他者を巻き込まない」ための余裕であり、
// **短くしないこと** (縮めるほど走行中の acquire を消す確率が上がる)。
const (
	scratchRetention   = time.Hour // tmp/ と probe/
	graveyardRetention = 7 * 24 * time.Hour
	cleanupInterval    = 10 * time.Minute // レート制限
	cleanupStampName   = ".cleanup_at"
)

// CleanupResult は掃除の結果。失敗は致命にしないので、件数を持ち帰って
// status --json / --verbose にだけ出す (黙って捨てない)。
type CleanupResult struct {
	Removed int      `json:"removed"`
	Skipped bool     `json:"skipped"`
	Errors  []string `json:"errors,omitempty"`
}

// Cleanup は残骸だけを掃除する。
//
// 🚨 lock 本体は絶対に対象にしない。「期限切れを消してから作る」は二重取得が最も出る
// 経路で、それを掃除という別経路から持ち込むと、rename 引き継ぎで勝者を 1 人に絞った
// 意味が消える。期限切れの回収は Acquire の引き継ぎだけが行う。
// .lockman/ 自体も消さない (再作成の churn と rmdir の競合を避ける)。
func (l *Locker) Cleanup(force bool, selfToken string) CleanupResult {
	var res CleanupResult
	if !force && !l.cleanupDue() {
		res.Skipped = true
		return res
	}
	now, err := l.serverNow()
	if err != nil {
		// 時刻が取れないなら何もしない。掃除は正しさに関与しないので、
		// 疑わしいときは触らないほうが安全。
		res.Errors = append(res.Errors, err.Error())
		return res
	}
	sweep := func(sub string, retention time.Duration) {
		dir := filepath.Join(l.metaDir, sub)
		entries, err := os.ReadDir(dir)
		if err != nil {
			if !os.IsNotExist(err) {
				res.Errors = append(res.Errors, err.Error())
			}
			return
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			// 自分の残骸は消さない (自分の acquire が進行中かもしれない)。
			if selfToken != "" && (e.Name() == selfToken || e.Name() == selfToken+".json") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			if now.Sub(info.ModTime()) <= retention {
				continue
			}
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
				res.Errors = append(res.Errors, err.Error())
				continue
			}
			res.Removed++
		}
	}
	sweep(tmpDirName, scratchRetention)
	sweep(probeDirName, scratchRetention)
	sweep(graveyardDirName, graveyardRetention)
	l.stampCleanup()
	return res
}

// cleanupDue はレート制限。判定に必要なファイルが読めなければ「掃除しない」に倒す
// (掃除は正しさに関与しないので、疑わしいときは何もしない)。
func (l *Locker) cleanupDue() bool {
	st, err := os.Stat(filepath.Join(l.metaDir, cleanupStampName))
	if err != nil {
		return os.IsNotExist(err) // 一度も掃除していないなら掃除する
	}
	now, err := l.serverNow()
	if err != nil {
		return false
	}
	return now.Sub(st.ModTime()) > cleanupInterval
}

func (l *Locker) stampCleanup() {
	path := filepath.Join(l.metaDir, cleanupStampName)
	// mtime はサーバに打刻させる (utimes を使うとクライアントの時計が入る)。
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, lockFileMode)
	if err != nil {
		return
	}
	_, _ = f.Write([]byte("lockman\n"))
	f.Close()
}
