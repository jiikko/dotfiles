package main

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// doctor のキャッシュ書き込みで **write が失敗したとき** に temp が残らないこと (issue 219)。
//
// 🚨 このテストが見ているのは `writeAtomic` の **write 分岐** (Write 失敗時の os.Remove)。
// rename 分岐は `TestSaveCacheCleansTempOnRenameFailure` が pin しており、そちらを外しても
// 本テストは緑のまま = 別の分岐を見ている証拠になる
// (mutation-verify-new-tests.md「スイートの red を効いていると読まない」)。
//
// write 失敗の作り方: `RLIMIT_FSIZE` を 0 にすると `(*os.File).Write` が EFBIG
// (`file too large`) を返す。Go は SIGXFSZ で死なない (実測 2026-09-04)。
// RAM ディスク (`hdiutil attach ram://`) を使わない理由は、root も要らず 0.2 秒で済み、
// 後始末が破壊的操作にならないため。production に seam を足す必要もない。
//
// ⚠️ RLIMIT_FSIZE は **プロセス全体**に効くので、このテストで `t.Parallel()` を呼ばないこと
// (この package の `t.Parallel()` は 2 箇所だけで、どちらも「足せない」と注記済み)。
// 失敗しても必ず戻すため defer で復元する。
func TestSaveDoctorCachesCleanTempOnWriteFailure(t *testing.T) {
	for _, tc := range []struct {
		name    string
		tmpName string // err に載る temp 名 (出所が読めることの検証)
		save    func() error
	}{
		{"disk cache", "doctor-disk.json.tmp.", func() error { return saveDoctorDiskCache(doctorDiskCache{Total: 1}) }},
		{"snapshot", "doctor-snapshot.json.tmp.", func() error { return saveDoctorSnapshot(doctorSnapshot{}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			t.Setenv("XDG_CACHE_HOME", base) // cachedir.Base() が見る。t.Parallel は使わない

			var lim syscall.Rlimit
			if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &lim); err != nil {
				t.Fatalf("判定不能: Getrlimit が失敗した: %v", err)
			}
			if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: 0, Max: lim.Max}); err != nil {
				t.Fatalf("判定不能: RLIMIT_FSIZE を 0 にできない: %v", err)
			}
			err := tc.save()
			if rerr := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &lim); rerr != nil {
				t.Fatalf("RLIMIT_FSIZE を戻せなかった (以降のテストが壊れる): %v", rerr)
			}

			if err == nil {
				t.Fatal("write が失敗するはずの構成でエラーが返らない")
			}
			// 「判定不能」を緑にしない: CreateTemp 段で落ちていると write 分岐を通っていない
			// (実測: ディレクトリを 0o500 にする形は CreateTemp が permission denied で落ちる)
			if !strings.Contains(err.Error(), "file too large") {
				t.Fatalf("判定不能: write 分岐に到達していない (err=%v)", err)
			}
			// err には temp のフルパスが載る。**名前から出所が読めること**をここで固定する
			// (残骸を掃く経路が無いので名前が唯一の手がかり。`*.tmp.*` の 1 glob で掃ける形)
			if !strings.Contains(err.Error(), tc.tmpName) {
				t.Errorf("temp の名前から出所が読めない: err=%v (期待: %s を含む)", err, tc.tmpName)
			}
			leftovers, gerr := filepath.Glob(filepath.Join(base, "glog", "*.tmp.*"))
			if gerr != nil {
				t.Fatal(gerr)
			}
			if len(leftovers) != 0 {
				t.Errorf("write 失敗後に temp が残っている: %v", leftovers)
			}
		})
	}
}
