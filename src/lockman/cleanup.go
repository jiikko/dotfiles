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

// CleanupResult は掃除の結果。失敗は致命にしないので、件数とエラーを持ち帰る
// (黙って捨てない)。Errors が非空なら `main.go` の dispatch が verbose でなくても
// stderr へ出す。
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
		//
		// 🚨 ただし metaDir 自体がまだ無いのは**良性** — 一度も使われていない
		// ディレクトリで、掃除の目的は既に達成されている。ここを失敗に数えると、
		// 3 周目が作った「stderr の警告 = 掃除が止まっている」というシグナルが
		// 正常系で鳴る (`cleanup` は ensureDirs を呼ばない唯一のサブコマンドなので、
		// .lockman の無い dir に打つと毎回鳴っていた。5 周目の実測)。
		//
		// 良性の線は **metaDir の有無**で引く。metaDir は在るのに probe/ を作れない
		// のは、ensureDirs が 4 つ同時に作る以上**異常**なので記録側へ倒す
		// (良性側へ広げると tmp/ graveyard/ のゴミを黙って掃かずに帰ることになり、
		// 3 周目が潰した「沈黙 = 成功」を作り直す)。
		//
		// 🚨 ここと sweepDir で**良性の線が 2 本ある**のは意図的。`tmp/` の欠損は
		// 「掃除の目的が達成済み」で先へ進めるが、`probe/` の欠損は**時刻が取れない**
		// ので先へ進めない (掃除の可否そのものが判定不能)。
		//
		// 🚨 良性とするのは metaDir が **無い**ときだけで、`statErr != nil` へ広げない。
		// stale mount / EACCES / ELOOP は「判定できない」であって良性ではなく、
		// 広げると脅威モデル②「壊れているのに黙って止まる」を作り直す
		// (6 周目の実測: 述語だけを広げる変異は全 39 ケース緑で通った)。
		if _, statErr := os.Stat(l.metaDir); os.IsNotExist(statErr) {
			return res
		}
		res.Errors = append(res.Errors, err.Error())
		return res
	}
	l.sweepDir(tmpDirName, scratchRetention, now, &res)
	l.sweepDir(probeDirName, scratchRetention, now, &res)
	l.sweepDir(graveyardDirName, graveyardRetention, now, &res)
	// 🚨 何か失敗していたら打刻しない。打刻するとレート制限が進んで以後 10 分は skip
	// され、「掃除が黙って止まっている」状態が見えなくなる (毎回出し直すほうが気づける)。
	// 対象は下限違反だけでなく **実際に起きうる失敗**も — 権限ドリフトや stale mount で
	// `ReadDir` が落ちる経路が本命 (下限違反は壊れたビルドでしか起きない)。
	// 🚨 コストは「再試行が早まる」では済まない。失敗が持続するあいだレート制限が
	// 丸ごと死に、acquire のたびに 3 dir をフル readdir する (実測 2026-09-12、
	// ローカル APFS / graveyard 2000 件: 12.2 → 17.5 ms/acquire = +43%。SMB では未実測で、
	// trigger は「SMB 共有で acquire が遅いという報告が出たとき同じ A-B を採る」)。
	// 🚨 コストは readdir の時間だけではない。defer の Cleanup が長くなるほど、
	// `timed()` が見捨てた goroutine が lock を置ききる窓も広がる
	// (実測 2026-09-12: graveyard 200 件で漏れ 40/40。issue 362)。
	// それでも打刻するより安い: 打刻すると壊れていること自体が 10 分ごとにしか見えない。
	if len(res.Errors) == 0 {
		// 🚨 打刻そのものの失敗も持ち帰る。握り潰すと、掃除は成功しているのに
		// レート制限が永久に進まない状態が rc=0 / stderr 0B の完全な無音になる
		// (5 周目の実測: .lockman が 0500 だと sweep は通り打刻だけ落ちる)。
		if err := l.stampCleanup(); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("掃除は終わったが打刻できない: %v", err))
		}
	}
	return res
}

// sweepDir は 1 ディレクトリぶんの掃除。retention が下限を割っていたら **何も消さずに**
// 拒否する (判定できない・危険なときは触らないほうが安全)。
//
// 🚨 下限が見ているのは retention だけで、now は検査していない。now を未来へ振ると
// 下限を割らずに新しい残骸を消せるが、production では now も `info.ModTime()` も
// 同じ `serverNow()` = サーバ側の打刻なので、その入力を作る経路が無い
// (2 周目の敵対レビューが指摘。到達可能性は未確認のまま、防御は足さない)。
// `Renew` が `clockSkewTolerance` を持つのは打刻がクライアント側へ落ちる場合の検算で、
// 掃除は正しさに関与しないため同じ検算は要らない。
func (l *Locker) sweepDir(sub string, retention time.Duration, now time.Time, res *CleanupResult) {
	// 🚨 掃除してよいのは残骸の 3 つのサブディレクトリだけ。**retention と同じく
	// 「渡った値」で縛る** — ここが無いと `l.sweepDir("", scratchRetention, now, &res)`
	// (「.lockman 直下に落ちた孤児も掃除したい」という自然な 1 行) で metaDir 直下が
	// 対象になり、**生きている lock が消えて二重取得が成立する**。
	// 5 周目の実測: その 1 行を足すと build も全 33 テストも緑のまま通り、E2E では
	// A の lock が消えて renew が「not the lock owner」、直後に C が acquire に成功した。
	// 上の 🚨 (lock 本体は絶対に対象にしない) を守っていたのはコメントだけだった。
	switch sub {
	case tmpDirName, probeDirName, graveyardDirName:
	default:
		res.Errors = append(res.Errors, fmt.Sprintf(
			"掃除の対象外のディレクトリ %q が渡された: lock 本体を消しうるので掃除しない "+
				"(掃除してよいのは %q / %q / %q だけ)",
			sub, tmpDirName, probeDirName, graveyardDirName))
		return
	}
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
			// 「他者が先に消した」は掃除の目的が達成された状態なので良性。
			// それ以外は記録する (下の os.Remove と扱いを揃える)。
			if !os.IsNotExist(err) {
				res.Errors = append(res.Errors, err.Error())
			}
			continue
		}
		if now.Sub(info.ModTime()) <= retention {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			// 🚨 ENOENT は良性。並行する acquire の掃除どうしは同じ残骸を取り合うので、
			// 「他者が先に消した」は**正常系**に出る。ここを記録すると、レート制限が
			// 死んで毎回フル sweep になり、利用者の端末に警告が出続ける
			// (実測 2026-09-12: 同一 dir への同時 acquire 6 試行すべてで打刻が飛び、
			//  stderr に 1.3〜3.5 KB。`_av1ify_lock.zsh` は stdout しか落としていない)。
			if !os.IsNotExist(err) {
				res.Errors = append(res.Errors, err.Error())
			}
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

func (l *Locker) stampCleanup() error {
	path := filepath.Join(l.metaDir, cleanupStampName)
	// mtime はサーバに打刻させる (utimes を使うとクライアントの時計が入る)。
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, lockFileMode)
	if err != nil {
		return err
	}
	// 🚨 umask に削られたモードを戻す (`ensureDirs` が dir にやっているのと同じ手当て)。
	// `.cleanup_at` は **作成者以外が「書き込みで」開く唯一のファイル**なので
	// (lock の O_TRUNC は所有者だけ、tmp/probe/graveyard の削除は dir の権限で足りる)、
	// 0644 のまま残すと別ユーザーの打刻が**恒久的に** EACCES になる。
	// lock.go が想定する 0777 no-sticky の共有 (SMB) 構成では実際に起きる。
	// 6 周目の実測: この 1 行が無いと `with` の 3 回とも同じ警告が出て mtime も進まない
	// (= レート制限が死んだまま、3 周目が作った警告チャネルが正常系で鳴り続ける)。
	_ = os.Chmod(path, lockFileMode)
	if _, err := f.Write([]byte("lockman\n")); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
