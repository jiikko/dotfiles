package cmdenv

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

func TestUnimplementedNamesTheSubcommandAndReturnsUsageCode(t *testing.T) {
	var errOut bytes.Buffer
	rc := Unimplemented(Env{Stderr: &errOut}, "lint")
	if rc != ExitUsage || !strings.Contains(errOut.String(), "ja-writing lint:") {
		t.Fatalf("rc=%d stderr=%q", rc, errOut.String())
	}
}

func TestFromProcessFillsCwdAndGetenv(t *testing.T) {
	t.Setenv("JA_WRITING_CMDENV_TEST", "v")
	env, err := FromProcess([]string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if env.Cwd == "" || env.Getenv("JA_WRITING_CMDENV_TEST") != "v" || len(env.Args) != 1 || env.Stdin == nil || env.Stdout == nil || env.Stderr == nil {
		t.Fatalf("%+v", env)
	}
}

// Lookup は本家の os.environ と同じく、空の値を「ある」とし、無い名前と分ける。同じ名前が 2 つあれば先のもの。Environ が nil なら何も無い。
func TestLookupSeparatesEmptyFromMissing(t *testing.T) {
	env := Env{Environ: []string{"EMPTY=", "TWICE=first", "TWICE=second", "WITH=a=b"}}
	for _, tc := range []struct {
		name, want string
		ok         bool
	}{{"EMPTY", "", true}, {"MISSING", "", false}, {"TWICE", "first", true}, {"WITH", "a=b", true}} {
		if got, ok := env.Lookup(tc.name); got != tc.want || ok != tc.ok {
			t.Errorf("Lookup(%q) = %q, %v (期待 %q, %v)", tc.name, got, ok, tc.want, tc.ok)
		}
	}
	if _, ok := (Env{}).Lookup("HOME"); ok {
		t.Error("Environ が nil の Env に HOME がある")
	}
	if got := (Env{}).Environment(); got == nil || len(got) != 0 {
		t.Errorf("Environment() = %#v (空で nil でない並びを期待)", got)
	}
}

// 子に渡す環境は、同じ名前が 2 つあれば親の Lookup と同じ先のものになる (本家は os.environ から作った dict を子に渡す)。
// 重なりのまま exec に渡すと、子は後のものを読む。子の側で読んだ値で見る (並びの形だけを見ると、exec の畳み方の違いが見えない)。
func TestChildSeesSameValueAsLookup(t *testing.T) {
	env := Env{Environ: []string{"TWICE=first", "NOEQUAL", "PATH=/usr/bin:/bin", "TWICE=second", "EMPTY="}}
	want, _ := env.Lookup("TWICE")
	cmd := exec.Command("/usr/bin/printenv", "TWICE")
	cmd.Env = env.Environment()
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != want {
		t.Fatalf("子は %q を読んだ (親の Lookup は %q)", got, want)
	}
	if got := env.Environment(); strings.Join(got, "\n") != "TWICE=first\nPATH=/usr/bin:/bin\nEMPTY=" {
		t.Fatalf("Environment() = %q", got)
	}
}
