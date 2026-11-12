package cmdenv

import (
	"bytes"
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
	t.Setenv("YOMIYASU_CMDENV_TEST", "v")
	env, err := FromProcess([]string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if env.Cwd == "" || env.Getenv("YOMIYASU_CMDENV_TEST") != "v" || len(env.Args) != 1 || env.Stdin == nil || env.Stdout == nil || env.Stderr == nil {
		t.Fatalf("%+v", env)
	}
}
