package disk

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func longestCommandPrefix[V any](line string, responses map[string]V) string {
	best := ""
	for key := range responses {
		if !strings.HasPrefix(line, key) {
			continue
		}
		if len(line) > len(key) && line[len(key)] != ' ' {
			continue
		}
		if len(key) > len(best) {
			best = key
		}
	}
	return best
}

func TestRunnerCommandDispatch(t *testing.T) {
	sentinel := errors.New("command failed")
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"exact", []string{"list"}, "SHORT"},
		{"longest", []string{"list", "devices"}, "LONG"},
		{"arguments", []string{"list", "devices", "-j"}, "LONG"},
		{"path boundary", []string{"listExtra"}, ""},
		{"tab boundary", []string{"list\tdevices"}, ""},
		{"no match", []string{"unknown"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scan := &fakeRunner{resp: map[string]fakeResp{
				"":                  {out: "EMPTY"},
				"tool list":         {out: "SHORT", rc: 7, err: sentinel},
				"tool list devices": {out: "LONG", rc: 7, err: sentinel},
			}}
			deleted := &deleteRunner{resp: map[string]cmdResp{
				"":                  {stdout: "EMPTY"},
				"tool list":         {stdout: "SHORT", stderr: "delete stderr", rc: 7, err: sentinel},
				"tool list devices": {stdout: "LONG", stderr: "delete stderr", rc: 7, err: sentinel},
			}}
			callbacks := 0
			deleted.onCall = func(args []string) {
				callbacks++
				if len(deleted.calls) != 1 || !reflect.DeepEqual(args, deleted.calls[0]) {
					t.Fatalf("onCall must follow call recording: args=%v calls=%v", args, deleted.calls)
				}
				args[0] = "mutated"
			}
			for _, r := range []struct {
				name    string
				run     func(context.Context, string, ...string) (string, string, int, error)
				stderr  string
				missing string
			}{
				{"scan", scan.run, "stderr", "unexpected: tool"},
				{"delete", deleted.run, "delete stderr", "unexpected: tool " + strings.Join(tc.args, " ")},
			} {
				out, stderr, rc, err := r.run(context.Background(), "tool", tc.args...)
				if tc.want == "" {
					if out != "" || stderr != r.missing || rc != 1 || err != nil {
						t.Errorf("%s unmatched: %q %q %d %v", r.name, out, stderr, rc, err)
					}
				} else if out != tc.want || stderr != r.stderr || rc != 7 || !errors.Is(err, sentinel) {
					t.Errorf("%s matched: %q %q %d %v", r.name, out, stderr, rc, err)
				}
			}
			wantCalls := [][]string{append([]string{"tool"}, tc.args...)}
			if !reflect.DeepEqual(scan.calls, wantCalls) || !reflect.DeepEqual(deleted.calls, wantCalls) || callbacks != 1 {
				t.Errorf("calls: scan=%v delete=%v callbacks=%d want=%v", scan.calls, deleted.calls, callbacks, wantCalls)
			}
		})
	}
}
