package cmdenv

import (
	"bytes"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"ja-writing/internal/pyunicode"
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

// FromProcess は環境変数と引数を本家の os.environ・sys.argv と同じ字 (os.fsdecode) で持ち、Getenv もその字を返す。python3.15 (3.15.0) の
// os.environ と sys.argv で b"\xed\xa0\x80" は '\udced\udca0\udc80'、b"\xff" は '\udcff' (WTF-8 のサロゲートの形も、3 バイトとも surrogateescape で読む)。
func TestFromProcessReadsEnvironmentLikeFsdecode(t *testing.T) {
	for name, c := range map[string]struct {
		raw  string
		want []rune
	}{
		"JA_WRITING_RAW_D800": {"\xed\xa0\x80", []rune{0xDCED, 0xDCA0, 0xDC80}},
		"JA_WRITING_RAW_DCFF": {"\xed\xb3\xbf", []rune{0xDCED, 0xDCB3, 0xDCBF}},
		"JA_WRITING_RAW_FF":   {"a\xffb", []rune{'a', 0xDCFF, 'b'}},
		"JA_WRITING_RAW_UTF8": {"日本", []rune{'日', '本'}},
	} {
		t.Setenv(name, c.raw)
		env, err := FromProcess([]string{"lint", c.raw})
		if err != nil {
			t.Fatal(err)
		}
		if got := pyunicode.Runes(env.Getenv(name)); !slices.Equal(got, c.want) {
			t.Errorf("%s: Getenv の字は %U、本家の os.environ は %U", name, got, c.want)
		}
		if got := pyunicode.Runes(env.Args[1]); env.Args[0] != "lint" || !slices.Equal(got, c.want) {
			t.Errorf("%s: 引数の字は %q %U、本家の sys.argv は %U", name, env.Args[0], got, c.want)
		}
		found := false
		for _, entry := range env.Environment() {
			if key, value, _ := strings.Cut(entry, "="); key == name {
				found = true
				if got := pyunicode.Runes(value); !slices.Equal(got, c.want) {
					t.Errorf("%s: Environment の字は %U、本家の os.environ は %U", name, got, c.want)
				}
			}
		}
		if !found {
			t.Errorf("%s: Environment に無い", name)
		}
	}
}

// Lookup は Environ の字をそのまま返す (本家の os.environ の値。OS に渡すバイトにするのは internal/pyos)。記録に入る環境変数
// (YOMIYASU_SESSION・CODEX_THREAD_ID) の "\udcff" が 0xff のバイトや U+FFFD にならない。
func TestLookupReturnsEnvironValue(t *testing.T) {
	dcff := pyunicode.FromRunes([]rune{0xDCFF})
	d800 := pyunicode.FromRunes([]rune{0xD800})
	env := Env{Environ: []string{"A=x" + dcff, "B=" + d800, "C=日本"}}
	for name, want := range map[string]string{"A": "x" + dcff, "B": d800, "C": "日本"} {
		if got := env.Getenv(name); got != want {
			t.Errorf("%s: % x、期待は % x", name, got, want)
		}
	}
}
