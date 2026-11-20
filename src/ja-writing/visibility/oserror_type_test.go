package visibility_test

import (
	"syscall"
	"testing"

	"ja-writing/visibility"
)

// measuredOSErrorTypes は python3.15 (3.15.0) で errno 0〜120 について type(OSError(errno, "x")).__name__ を引き、OSError でなかったもの
// (`python3.15 -I -c 'for n in range(121): ...'`)。
var measuredOSErrorTypes = map[syscall.Errno]string{
	1: "PermissionError", 2: "FileNotFoundError", 3: "ProcessLookupError", 4: "InterruptedError", 10: "ChildProcessError",
	13: "PermissionError", 17: "FileExistsError", 20: "NotADirectoryError", 21: "IsADirectoryError", 32: "BrokenPipeError",
	35: "BlockingIOError", 36: "BlockingIOError", 37: "BlockingIOError", 53: "ConnectionAbortedError", 54: "ConnectionResetError",
	58: "BrokenPipeError", 60: "TimeoutError", 61: "ConnectionRefusedError", 107: "PermissionError",
}

// errno 0〜120 の全部で、OSError の型の名前が本家 (CPython) の実測と同じ。表の項目を 1 つ消しても足しても、その errno で落ちる。
func TestOSErrorTypeMatchesPython(t *testing.T) {
	named := 0
	for n := syscall.Errno(0); n <= 120; n++ {
		want, ok := measuredOSErrorTypes[n]
		if !ok {
			want = "OSError"
		} else {
			named++
		}
		if got := visibility.OSErrorType(n); got != want {
			t.Errorf("errno %d: %s、本家 %s", n, got, want)
		}
	}
	if named != len(measuredOSErrorTypes) || named != 19 {
		t.Fatalf("実測の表のうち 0〜120 で比べたのは %d 件 (表は %d 件、測ったのは 19 件)", named, len(measuredOSErrorTypes))
	}
}
