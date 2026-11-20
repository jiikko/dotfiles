package check

import (
	"os"
	"strings"
	"testing"
)

func TestReadRecordStatus(t *testing.T) {
	s := newSandbox(t)
	if err := os.Mkdir(s.path("folder"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.path("file"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"ok.json":     `{"a": 1}`,
		"empty.json":  "",
		"list.json":   `[1]`,
		"broken.json": `{"a":`,
		"bom.json":    "\xef\xbb\xbf{}",
		"bytes.json":  "{\"a\": \"\xff\"}",
		"nan.json":    `{"a": NaN}`,
	}
	for name, body := range files {
		if err := os.WriteFile(s.path(name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name string
		path string
		want RecordStatus
	}{
		{"JSON の dict", s.path("ok.json"), RecordOK},
		{"NaN を含む", s.path("nan.json"), RecordOK},
		{"無い", s.path("missing.json"), RecordMissing},
		{"親がファイル", s.path("file", "x.json"), RecordBad},
		{"フォルダ", s.path("folder"), RecordBad},
		{"空", s.path("empty.json"), RecordBad},
		{"dict でない", s.path("list.json"), RecordBad},
		{"JSON でない", s.path("broken.json"), RecordBad},
		{"BOM", s.path("bom.json"), RecordBad},
		{"UTF-8 でない", s.path("bytes.json"), RecordBad},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, record, err := ReadRecordStatus(c.path)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want || (record != nil) != (c.want == RecordOK) {
				t.Errorf("ReadRecordStatus = %s (%v), want %s", got, record != nil, c.want)
			}
		})
	}
}

func TestReadRecordReturnsErrorForTooDeepJSON(t *testing.T) {
	s := newSandbox(t)
	path := s.path("deep.json")
	if err := os.WriteFile(path, []byte(strings.Repeat("[", 200000)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadRecordStatus(path); err == nil {
		t.Error("本家が落ちる入れ子の深さを bad として読んだ")
	}
}

func TestReadRecordStatusReadsAgainAfterWriterFinishes(t *testing.T) {
	s := newSandbox(t)
	path := s.path("record.json")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	pauses := 0
	swap(t, &emptyRecordPause, func() {
		pauses++
		if err := os.WriteFile(path, []byte(`{"a": 1}`), 0o600); err != nil {
			t.Error(err)
		}
	})
	got, record, err := ReadRecordStatus(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != RecordOK || record == nil || pauses != 1 {
		t.Errorf("待つ間に書き終えた記録 = %s (%v)・待ち %d 回, want ok・待ち 1 回", got, record != nil, pauses)
	}
}

func TestReadRecordStatusWaitsOnceOnlyForEmptyRecord(t *testing.T) {
	s := newSandbox(t)
	empty, full := s.path("empty.json"), s.path("full.json")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	pauses := 0
	swap(t, &emptyRecordPause, func() { pauses++ })
	if got, _, err := ReadRecordStatus(empty); err != nil || got != RecordBad || pauses != 1 {
		t.Errorf("空のままの記録 = %s, %v・待ち %d 回, want bad・待ち 1 回", got, err, pauses)
	}
	pauses = 0
	if got, _, err := ReadRecordStatus(full); err != nil || got != RecordOK || pauses != 0 {
		t.Errorf("中身のある記録 = %s, %v・待ち %d 回, want ok・待ち 0 回", got, err, pauses)
	}
}
