package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"zundamon-kaisetsu/internal/build"
	"zundamon-kaisetsu/internal/paths"
	"zundamon-kaisetsu/internal/pyjson"
	"zundamon-kaisetsu/internal/script"
	"zundamon-kaisetsu/internal/synth"
)

// buildFixtureScript は testdata/build (合成済みの wav つき) を一時 dir に写し、台本を edit で直してから書く。
// edit は合成の鍵に入らないキー (lead_in / gap / pause_after / chapter / show) だけを触ること (鍵が変わると wav が無くなる)。
func buildFixtureScript(t *testing.T, edit func(raw map[string]any)) string {
	t.Helper()
	dir := t.TempDir()
	copyTree(t, filepath.Join("testdata", "build", "script.work"), filepath.Join(dir, "script.work"))
	copyTree(t, filepath.Join("testdata", "build", "faces"), filepath.Join(dir, "faces"))
	b, err := os.ReadFile(filepath.Join("testdata", "build", "script.json"))
	must(t, err)
	var raw map[string]any
	must(t, script.DecodeJSON(b, &raw))
	if edit != nil {
		edit(raw)
	}
	path := filepath.Join(dir, "script.json")
	writeScriptFile(t, path, raw)
	return path
}

// LineTimes (synth の字幕の速さの警告と times が使う) は、Assemble (build) の timeline と同じ開始・終了と長さを出す。
// 間の秒数の丸めを共有しているので、lead_in / gap / pause_after を既定から外しても一致する
func TestLineTimesMatchAssemble(t *testing.T) {
	for name, edit := range map[string]func(map[string]any){
		"既定の間": nil,
		"間を変える": func(raw map[string]any) {
			raw["lead_in"], raw["gap"] = 0.123, 0.4567
			raw["lines"].([]any)[1].(map[string]any)["pause_after"] = 1.0001
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := buildFixtureScript(t, edit)
			env := testEnv(t)
			s, err := script.LoadScript(path, env)
			must(t, err)
			data, _, err := build.Assemble(s, env)
			must(t, err)
			spans, duration, err := build.LineTimes(s)
			must(t, err)
			if len(spans) != len(data.Lines) || len(spans) < 2 {
				t.Fatalf("行の数: LineTimes %d / Assemble %d", len(spans), len(data.Lines))
			}
			for i, sp := range spans {
				if sp.Start != data.Lines[i].Start || sp.End != data.Lines[i].End {
					t.Errorf("lines[%d]: LineTimes %v / Assemble %v-%v", i, sp, data.Lines[i].Start, data.Lines[i].End)
				}
			}
			if pyjson.Round3(duration) != data.Duration {
				t.Errorf("長さ: LineTimes %v / Assemble %v", duration, data.Duration)
			}
		})
	}
}

// times は行ごとの 行番号 / 開始 / 終了 / 話者 / チャプター / 図解 / 字幕 を TSV で出す (dispatch を通す)。wav が無い行があれば止める
func TestTimesCommand(t *testing.T) {
	path := buildFixtureScript(t, func(raw map[string]any) {
		lines := raw["lines"].([]any)
		lines[1].(map[string]any)["chapter"] = "第2\t章"
		lines[1].(map[string]any)["show"] = map[string]any{"type": "keyword", "text": "語"}
		lines[2].(map[string]any)["show"] = nil
	})
	env := testEnv(t)
	var out bytes.Buffer
	env.Stdout = &out
	must(t, dispatch([]string{"times", path}, env))
	s, err := script.LoadScript(path, env)
	must(t, err)
	spans, _, err := build.LineTimes(s)
	must(t, err)
	rows := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(rows) != len(s.Lines) {
		t.Fatalf("行の数 %d, want %d:\n%s", len(rows), len(s.Lines), out.String())
	}
	for i, row := range rows {
		col := strings.Split(row, "\t")
		if len(col) != 7 {
			t.Fatalf("欄の数 %d (7 のはず): %q", len(col), row)
		}
		if col[0] != strconv.Itoa(i) || col[3] != pyjson.Str(s.Lines[i]["who"]) || col[6] != pyjson.Str(s.Lines[i]["text"]) {
			t.Errorf("lines[%d] の欄が違う: %q", i, row)
		}
		if col[1] != fmt.Sprintf("%.2f", spans[i].Start) || col[2] != fmt.Sprintf("%.2f", spans[i].End) {
			t.Errorf("lines[%d] の開始・終了 %s-%s が LineTimes (%v) と違う", i, col[1], col[2], spans[i])
		}
	}
	if c := strings.Split(rows[1], "\t"); c[4] != "第2 章" || c[5] != "keyword" { // チャプター名のタブも空白にする
		t.Errorf("チャプターと図解の欄: %q", rows[1])
	}
	if c := strings.Split(rows[2], "\t"); c[4] != "" || c[5] != "消す" {
		t.Errorf("show: null の行: %q", rows[2])
	}
	if c := strings.Split(rows[0], "\t"); c[5] != "" {
		t.Errorf("show を書いていない行の図解の欄が空でない: %q", rows[0])
	}
	// 字幕のタブ・改行は空白にして、欄と行の区切りを崩さない
	raw := map[string]any{}
	b, err := os.ReadFile(path)
	must(t, err)
	must(t, script.DecodeJSON(b, &raw))
	l0 := raw["lines"].([]any)[0].(map[string]any)
	l0["read"] = script.SpokenText(s, s.Lines[0]) // 音声 (合成の鍵) を据え置いて字幕だけを変える
	l0["text"] = "a\tb\nc"
	writeScriptFile(t, path, raw)
	out.Reset()
	must(t, dispatch([]string{"times", path}, env))
	if first := strings.SplitN(out.String(), "\n", 2)[0]; !strings.HasSuffix(first, "\ta b c") || strings.Count(out.String(), "\n") != len(s.Lines) {
		t.Errorf("タブ・改行を含む字幕で TSV が崩れた:\n%s", out.String())
	}
	// 合成していない行があれば止める (古い wav の時刻を出さない)
	must(t, os.RemoveAll(filepath.Join(filepath.Dir(path), "script.work")))
	if err := dispatch([]string{"times", path}, env); err == nil || !strings.Contains(err.Error(), "合成済みの wav が無い") {
		t.Errorf("wav が無いのに止まらない: %v", err)
	}
}

// synth は、字幕が速すぎる行を build を待たずに警告する。止めはしない (rc は 0。止めるのは build)
func TestSynthWarnsFastCaptions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/speakers":
			_, _ = io.WriteString(w, `[{"name":"四国めたん","styles":[{"name":"ノーマル","id":2}]},{"name":"ずんだもん","styles":[{"name":"ノーマル","id":3}]}]`)
		case "/audio_query":
			_, _ = io.WriteString(w, `{"accent_phrases":[],"speedScale":1.0}`)
		case "/synthesis":
			_, _ = w.Write(wavBytes(fmtChunk(1, 24000, 16), chunk("data", make([]byte, 4800)))) // 0.1 秒
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	run := func(texts ...string) string {
		t.Helper()
		var lines []any
		for _, x := range texts {
			lines = append(lines, map[string]any{"who": "metan", "text": x})
		}
		path := filepath.Join(t.TempDir(), "s.json")
		writeScriptFile(t, path, map[string]any{"lines": lines})
		env := testEnv(t)
		env.Engine = srv.URL
		var errb strings.Builder
		env.Stderr = &errb
		must(t, synth.CmdSynth(env, path, false))
		return errb.String()
	}
	// 1 行目は 0.4 秒目から 0.85 秒目 (次の行の頭) まで 0.45 秒出る: 3 字なら 1 秒 6.7 字、4 字なら 8.9 字
	if got := run("あああ", "い"); strings.Contains(got, "字幕が速くて") {
		t.Errorf("前提: 速くない台本で警告した:\n%s", got)
	}
	got := run("ああああ", "い")
	if !strings.Contains(got, "synth: 字幕が速くて読み切れないおそれのある行が 1 行") || !strings.Contains(got, "lines[0] ") || !strings.Contains(got, "build が書き出す前に止まる") {
		t.Errorf("速い行の警告が無い:\n%s", got)
	}
}

// build は区切りのカードを、チャプターの 1 行目の音声が終わるまで出す (ChapterCardEnds を Assemble に配線している)。
// testdata の音声は 1 行 1 秒未満で、最短の 1.5 秒と区別できないので、チャプター「はじめに」の 1 行目の wav を 3 秒にして見る
func TestAssembleCardsLastUntilFirstLineEnds(t *testing.T) {
	path := buildFixtureScript(t, nil)
	env := testEnv(t)
	s, err := script.LoadScript(path, env)
	must(t, err)
	first := slices.IndexFunc(s.Lines, func(l map[string]any) bool { return pyjson.Str(l["chapter"]) == "はじめに" })
	if first < 0 {
		t.Fatal("前提: testdata の台本にチャプター「はじめに」が無い")
	}
	p, err := script.LineParams(s, s.Lines[first])
	must(t, err)
	wavPath, _ := script.CachePaths(paths.WorkDir(s.Path), p)
	must(t, os.WriteFile(wavPath, wavBytes(fmtChunk(1, 24000, 16), chunk("data", make([]byte, 2*24000*3))), 0o644))
	data, _, err := build.Assemble(s, env)
	must(t, err)
	line := data.Lines[first]
	if line.End-line.Start < 2.9 {
		t.Fatalf("前提: 差し替えた 1 行目が 3 秒になっていない (%v-%v)", line.Start, line.End)
	}
	on := build.RunCursor(data.Cards)
	at := func(sec float64) int { return on(int(sec * build.MouthFPS)) }
	if at(line.Start+0.1) != 1 || at(line.Start+2.5) != 1 || at(line.End+0.1) != 0 {
		t.Errorf("カードが 1 行目の終わり (%v) まで出ていない: 開始直後 %d / 2.5 秒後 %d / 終わりの後 %d (runs %v)",
			line.End, at(line.Start+0.1), at(line.Start+2.5), at(line.End+0.1), data.Cards)
	}
}

// 合成済みの wav が壊れていても、synth は字幕の速さの検査を案内に留めて rc=0 で終わる (壊れた wav は build が止める)
func TestSynthToleratesBrokenCachedWav(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/speakers":
			_, _ = io.WriteString(w, `[{"name":"四国めたん","styles":[{"name":"ノーマル","id":2}]},{"name":"ずんだもん","styles":[{"name":"ノーマル","id":3}]}]`)
		case "/audio_query":
			_, _ = io.WriteString(w, `{"accent_phrases":[],"speedScale":1.0}`)
		case "/synthesis":
			_, _ = w.Write(wavBytes(fmtChunk(1, 24000, 16), chunk("data", make([]byte, 4800))))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "s.json")
	writeScriptFile(t, path, map[string]any{"lines": []any{map[string]any{"who": "metan", "text": "あ"}}})
	env := testEnv(t)
	env.Engine = srv.URL
	must(t, synth.CmdSynth(env, path, false))
	s, err := script.LoadScript(path, env)
	must(t, err)
	p, err := script.LineParams(s, s.Lines[0])
	must(t, err)
	wavPath, _ := script.CachePaths(paths.WorkDir(s.Path), p)
	must(t, os.WriteFile(wavPath, []byte("RIFF"), 0o644)) // キャッシュに当たる (在る) が壊れた wav
	var errb strings.Builder
	env.Stderr = &errb
	if err := synth.CmdSynth(env, path, false); err != nil {
		t.Errorf("壊れたキャッシュの wav で synth が失敗した: %v", err)
	}
	if !strings.Contains(errb.String(), "字幕の速さを確かめられない") {
		t.Errorf("確かめられなかったことを知らせていない:\n%s", errb.String())
	}
}
