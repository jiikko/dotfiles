package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 引数の解析は Python 版の argparse と同じ意味にする (issue 641 の設計レビュー P1-3)。
func TestParseArgsLikeArgparse(t *testing.T) {
	specs := []optSpec{
		{names: []string{"-o", "--output"}, dest: "output", takes: true},
		{names: []string{"--format"}, dest: "format", takes: true, choices: []string{"html", "mp4", "both"}},
		{names: []string{"--force"}, dest: "force"},
		{names: []string{"--jobs"}, dest: "jobs", takes: true, check: jobsArg},
	}
	ok := []struct {
		args []string
		pos  []string
		opts map[string]string
	}{
		{[]string{"s.json", "-o", "out", "--format", "mp4"}, []string{"s.json"}, map[string]string{"output": "out", "format": "mp4"}},
		{[]string{"-o", "out", "s.json"}, []string{"s.json"}, map[string]string{"output": "out"}},
		{[]string{"--out=out", "--form", "both", "s.json"}, []string{"s.json"}, map[string]string{"output": "out", "format": "both"}},
		{[]string{"-oout", "s.json", "--form", "mp4"}, []string{"s.json"}, map[string]string{"output": "out", "format": "mp4"}},
		{[]string{"s.json", "--", "-x"}, []string{"s.json", "-x"}, map[string]string{}},
		{[]string{"--jobs", "16", "-1.5"}, []string{"-1.5"}, map[string]string{"jobs": "16"}},
	}
	for _, c := range ok {
		p, _, err := parseArgs(c.args, specs, false)
		if err != nil {
			t.Errorf("%v: %v", c.args, err)
			continue
		}
		if strings.Join(p.pos, "|") != strings.Join(c.pos, "|") {
			t.Errorf("%v: 位置引数 got %v want %v", c.args, p.pos, c.pos)
		}
		for k, v := range c.opts {
			if p.opts[k] != v {
				t.Errorf("%v: %s got %q want %q", c.args, k, p.opts[k], v)
			}
		}
	}
	bad := map[string][]string{
		"略記が曖昧 (--fo は format と force の両方)": {"--f", "x"},
		"値が無い":      {"-o"},
		"値がオプション":   {"-o", "--format", "mp4"},
		"知らない":      {"--nope"},
		"選べない値":     {"--format", "avi"},
		"範囲外":       {"--jobs", "17"},
		"値を取らないのに値": {"--force=1"},
	}
	for name, args := range bad {
		if _, _, err := parseArgs(args, specs, false); err == nil {
			t.Errorf("%s (%v): 誤りにならなかった", name, args)
		}
	}
}

func TestRunExitCodes(t *testing.T) {
	cases := []struct {
		args []string
		rc   int
	}{
		{[]string{}, 2},
		{[]string{"nope"}, 2},
		{[]string{"build", "s.json"}, 2},               // -o が無い
		{[]string{"synth"}, 2},                         // 台本が無い
		{[]string{"synth", "a.json", "b.json"}, 2},     // 位置引数が多い
		{[]string{"check", "--engine", "http://x"}, 2}, // --engine はサブコマンドより前だけ
		{[]string{"--help"}, 0},
		{[]string{"build", "s.json", "-o", "x", "--bitrate", "7"}, 2},
	}
	for _, c := range cases {
		var out, errb bytes.Buffer
		env := &Env{Stdout: &out, Stderr: &errb}
		if rc := run(c.args, env); rc != c.rc {
			t.Errorf("%v: rc=%d want %d (stderr: %s)", c.args, rc, c.rc, errb.String())
		}
	}
}

// skill のディレクトリが渡されていなければ、立ち絵の既定の置き場を見ずに進めず止める (設計レビュー P2-10)。
func TestRequireSkillDir(t *testing.T) {
	var errb bytes.Buffer
	env := &Env{Stdout: &errb, Stderr: &errb}
	if rc := run([]string{"build", "testdata/build/script.json", "-o", filepath.Join(t.TempDir(), "x")}, env); rc != 1 {
		t.Fatalf("rc=%d want 1: %s", rc, errb.String())
	}
	if !strings.Contains(errb.String(), skillDirEnv+" が無い。bin/zundamon-kaisetsu から起動する") {
		t.Errorf("起動の仕方の案内が無い: %s", errb.String())
	}
}

// 文字列でない text / read は、Python 版と違うキャッシュの鍵を黙って作らないよう拒否する (設計レビュー P2-5)。
func TestRejectNonStringTextAndRead(t *testing.T) {
	for _, line := range []map[string]any{
		{"who": "metan", "text": json.Number("5")},
		{"who": "metan", "text": nil},
		{"who": "metan", "text": "x", "read": nil},
		{"who": "metan", "text": "x", "read": true},
	} {
		path := writeScript(t, map[string]any{"lines": []any{line}})
		_, err := loadScript(path, testEnv(t))
		if err == nil || !strings.Contains(err.Error(), "は文字列で書く") {
			t.Errorf("%v: 拒否しなかった (%v)", line, err)
		}
	}
}

func wavBytes(chunks ...[]byte) []byte {
	var body []byte
	for _, c := range chunks {
		body = append(body, c...)
	}
	h := []byte("RIFF")
	h = binary.LittleEndian.AppendUint32(h, uint32(4+len(body)))
	return append(append(h, []byte("WAVE")...), body...)
}

func chunk(id string, data []byte) []byte {
	c := append([]byte(id), binary.LittleEndian.AppendUint32(nil, uint32(len(data)))...)
	c = append(c, data...)
	if len(data)%2 == 1 {
		c = append(c, 0)
	}
	return c
}

func fmtChunk(ch, rate, bits int) []byte {
	b := binary.LittleEndian.AppendUint16(nil, 1)
	b = binary.LittleEndian.AppendUint16(b, uint16(ch))
	b = binary.LittleEndian.AppendUint32(b, uint32(rate))
	b = binary.LittleEndian.AppendUint32(b, uint32(rate*ch*bits/8))
	b = binary.LittleEndian.AppendUint16(b, uint16(ch*bits/8))
	b = binary.LittleEndian.AppendUint16(b, uint16(bits))
	return chunk("fmt ", b)
}

// wav はチャンクを順にたどって読む (ヘッダ 44 バイト決め打ちだと LIST チャンクを持つ wav で壊れる)。
func TestCheckWavBytes(t *testing.T) {
	pcm := make([]byte, 200)
	good := wavBytes(chunk("LIST", []byte("INFOxyz")), fmtChunk(1, 24000, 16), chunk("data", pcm))
	got, n, err := checkWavBytes(good, "t")
	if err != nil || n != 100 || len(got) != 200 {
		t.Fatalf("LIST チャンク付きを読めない: n=%d len=%d err=%v", n, len(got), err)
	}
	truncated := good[:len(good)-10]
	if _, _, err := checkWavBytes(truncated, "t"); err == nil || !strings.Contains(err.Error(), "途中で切れている") {
		t.Errorf("途中で切れた wav を通した: %v", err)
	}
	stereo := wavBytes(fmtChunk(2, 24000, 16), chunk("data", pcm))
	if _, _, err := checkWavBytes(stereo, "t"); err == nil || !strings.Contains(err.Error(), "mono / 16bit / 24000Hz ではない") {
		t.Errorf("stereo を通した: %v", err)
	}
	if _, _, err := checkWavBytes([]byte("nope"), "t"); err == nil || !strings.Contains(err.Error(), "wav として読めない") {
		t.Errorf("wav でないものを通した: %v", err)
	}
	// 書いた wav を読み直せる (build の連結に使う)
	p := filepath.Join(t.TempDir(), "j.wav")
	if err := writeWav(p, pcm); err != nil {
		t.Fatal(err)
	}
	if b, _, err := readWav(p); err != nil || !bytes.Equal(b, pcm) {
		t.Errorf("writeWav → readWav が戻らない: %v", err)
	}
}

// HTML への差し込みは、</script> で JSON が途切れず、タイトルは & < > だけエスケープし、印を 1 回の走査で置き換える。
func TestRenderHTML(t *testing.T) {
	env := testEnv(t)
	data := &PlayerData{Title: "A&B <__DATA_JSON__>", Lines: []timelineLine{{Text: "</script><b>"}}, Frames: [][4]int{{0, -1, 0, 0}}}
	html, err := renderHTML(data, env)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "</script><b>") {
		t.Error("字幕の </script> がそのまま入っている")
	}
	if !strings.Contains(html, "<title>A&amp;B &lt;__DATA_JSON__&gt;</title>") {
		t.Error("タイトルのエスケープが違う (印をタイトルの中まで置き換えていないか)")
	}
	start := strings.Index(html, `<script type="application/json" id="data">`)
	end := strings.Index(html[start:], "</script>")
	if start < 0 || end < 0 {
		t.Fatal("データの script が見つからない")
	}
	payload := html[start+len(`<script type="application/json" id="data">`) : start+end]
	var back PlayerData
	if err := json.Unmarshal([]byte(payload), &back); err != nil || back.Lines[0].Text != "</script><b>" {
		t.Errorf("埋め込んだデータが元に戻らない: %v", err)
	}
}

// CPU が混んでいる間は最大 20 分待ち、空けば抜け、負荷を読めなければ待たない (時計を差し替えて実時間は待たない)。
func TestWaitForIdleCPU(t *testing.T) {
	cases := []struct {
		name  string
		loads []float64
		ok    bool
		want  time.Duration
	}{
		{"空いている", []float64{0}, true, 0},
		{"途中で空く", []float64{1e9, 1e9, 1e9, 0}, true, 3 * loadPoll},
		{"空かない", []float64{1e9}, true, loadWaitMax},
		{"読めない", []float64{1e9}, false, 0},
	}
	for _, c := range cases {
		now := time.Unix(0, 0)
		i := 0
		var errb bytes.Buffer
		env := &Env{Stderr: &errb,
			Now:   func() time.Time { return now },
			Sleep: func(d time.Duration) { now = now.Add(d) },
			LoadAvg: func() (float64, bool) {
				v := c.loads[min(i, len(c.loads)-1)]
				i++
				return v, c.ok
			}}
		waitForIdleCPU(env)
		if got := now.Sub(time.Unix(0, 0)); got != c.want {
			t.Errorf("%s: %v 待った (want %v)。出力: %s", c.name, got, c.want, errb.String())
		}
	}
}
