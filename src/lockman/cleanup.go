package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// 残骸の保持期間。閾値は「進行中の他者を巻き込まない」ための余裕。
const (
	scratchRetention   = time.Hour // tmp/ と probe/
	graveyardRetention = 7 * 24 * time.Hour
	cleanupInterval    = 10 * time.Minute // レート制限
	cleanupStampName   = ".cleanup_at"
)

// minRetention は sweep に渡せる保持期間の下限。残骸の実寿命はミリ秒だが、保持期間は
// 「I/O が返らないまま進行中の他者」を巻き込まないための余裕なので、既定の I/O の上限
// (defaultIOTimeout) より桁で長く取る。
//
// 🚨 縛るのは定数ではなく **sweep に渡る値**。定数に対する検査 (「scratchRetention が
// 10 分以上」) は、別の定数を作って sweep へ渡す / 下限も一緒に下げる、の 2 形で静かに
// 迂回される (issue 358 の敵対レビューが両方を実測。どちらもビルドもテストも緑だった)。
//
// 🚨 `--io-timeout` には上限の検証が無いので、既定から大きく離れた値 (例: 5h) を渡すと
// この余裕は成立しない。値の検証は issue 359 の「軽微だが実在する」節で 356 / 357 と
// 一緒に直す。
const minRetention = 10 * time.Minute

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
func (l *Locker) Cleanup(force bool) CleanupResult {
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
	l.sweepDir(tmpDirName, scratchRetention, now, &res)
	l.sweepDir(probeDirName, scratchRetention, now, &res)
	l.sweepDir(graveyardDirName, graveyardRetention, now, &res)
	l.stampCleanup()
	return res
}

// sweepDir は 1 ディレクトリぶんの掃除。retention が下限を割っていたら **何も消さずに**
// エラーを積む (判定できない・危険なときは触らないほうが安全)。
func (l *Locker) sweepDir(sub string, retention time.Duration, now time.Time, res *CleanupResult) {
	if retention < minRetention {
		res.Errors = append(res.Errors, fmt.Sprintf(
			"%s の保持期間 %s が下限 %s を下回る: 走行中の acquire の残骸を消しうるので掃除しない",
			sub, retention, minRetention))
		return
	}
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
