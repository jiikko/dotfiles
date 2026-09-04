package main

import (
	"os"
	"os/exec"
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
	cases := map[string]struct {
		tmpName string // err に載る temp 名 (出所が読めることの検証)
		save    func() error
	}{
		"disk-cache": {"doctor-disk.json.tmp.", func() error { return saveDoctorDiskCache(doctorDiskCache{Total: 1}) }},
		"snapshot":   {"doctor-snapshot.json.tmp.", func() error { return saveDoctorSnapshot(doctorSnapshot{}) }},
	}
	// 子プロセス側: 自分の枠を落として実際に検査する
	if name := os.Getenv(rlimitChildEnv); name != "" {
		tc, ok := cases[name]
		if !ok {
			t.Fatalf("未知のケース: %s", name)
		}
		checkWriteFailureCleansTemp(t, tc.tmpName, tc.save)
		return
	}
	for name := range cases {
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			// 🚨 **子プロセスで走らせる**。RLIMIT_FSIZE はプロセス全体に効くので、同じプロセスで
			// 落とすと `go test` 自身の testlog 書き込みが EFBIG になり、テストが全部 PASS でも
			// パッケージが FAIL する (実測 2026-09-04: origin/master で再現。タイミング依存なので
			// 緑の run もある)。os.Args[0] の直接起動なら -test.testlogfile が渡らないので、
			// 枠を落としても framework は壊れない
			cmd := exec.Command(os.Args[0], "-test.run", "^TestSaveDoctorCachesCleanTempOnWriteFailure$", "-test.v")
			// 🚨 テスト名はリテラル。改名すると -test.run が何にも当たらず、子は PASS を
			// 出さないので下の「走っていない」で落ちる (fail-closed)
			cmd.Env = append(os.Environ(), rlimitChildEnv+"="+name, "XDG_CACHE_HOME="+base)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("子プロセスが失敗した: %v\n%s", err, out)
			}
			// 子の中で assert しているので、ここでは「走ったこと」を確認する
			// (実行されていないのに緑、を作らない)
			if !strings.Contains(string(out), "PASS") {
				t.Fatalf("子プロセスが走っていない:\n%s", out)
			}
		})
	}
}

// rlimitChildEnv は「この実行は枠を落とす子プロセス」の目印 (値はケース名)。
const rlimitChildEnv = "GLOGX_TEST_RLIMIT_CASE"

// checkWriteFailureCleansTemp は RLIMIT_FSIZE を 0 にして save を呼び、
// write 分岐を通ったこと・temp が残らないことを確かめる (子プロセスでのみ呼ぶ)。
func checkWriteFailureCleansTemp(t *testing.T, tmpName string, save func() error) {
	t.Helper()
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		t.Fatal("判定不能: XDG_CACHE_HOME が渡っていない")
	}
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &lim); err != nil {
		t.Fatalf("判定不能: Getrlimit が失敗した: %v", err)
	}
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: 0, Max: lim.Max}); err != nil {
		t.Fatalf("判定不能: RLIMIT_FSIZE を 0 にできない: %v", err)
	}
	err := save()
	if rerr := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &lim); rerr != nil {
		t.Fatalf("RLIMIT_FSIZE を戻せなかった: %v", rerr)
	}
	if err == nil {
		t.Fatal("write が失敗するはずの構成でエラーが返らない")
	}
	// 「判定不能」を緑にしない: CreateTemp 段で落ちていると write 分岐を通っていない
	if !strings.Contains(err.Error(), "file too large") {
		t.Fatalf("判定不能: write 分岐に到達していない (err=%v)", err)
	}
	// err には temp のフルパスが載る。**名前から出所が読めること**をここで固定する
	if !strings.Contains(err.Error(), tmpName) {
		t.Errorf("temp の名前から出所が読めない: err=%v (期待: %s を含む)", err, tmpName)
	}
	leftovers, gerr := filepath.Glob(filepath.Join(base, "glog", "*.tmp.*"))
	if gerr != nil {
		t.Fatal(gerr)
	}
	if len(leftovers) != 0 {
		t.Errorf("write 失敗後に temp が残っている: %v", leftovers)
	}
}
