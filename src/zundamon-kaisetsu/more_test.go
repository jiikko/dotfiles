package main

// 敵対的レビュー (2026-10-06、壊す / 素通り の 2 観点) で見つかった穴を固定するテスト。golden は HEAD にあった Python 版から作った。

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMouthTrackSyntheticMatchesPython(t *testing.T) {
	var cases []struct {
		Name     string         `json:"name"`
		Query    map[string]any `json:"query"`
		Duration float64        `json:"duration"`
		Mouth    string         `json:"mouth"`
	}
	readGolden(t, "mouth_synth.json", &cases)
	if len(cases) < 20 {
		t.Fatalf("golden の件数が少ない: %d", len(cases))
	}
	for _, c := range cases {
		if got := mouthTrack(c.Query, c.Duration); got != c.Mouth {
			t.Errorf("%s (%.4f 秒): got %s want %s", c.Name, c.Duration, got, c.Mouth)
		}
	}
}

// 行の開始・終了がフレームの中点 (k+0.5)/30 とちょうど重なるときの等号の向き (start <= t / t < end)。
func TestFrameRunsBoundaryMatchesPython(t *testing.T) {
	var g struct {
		Timeline []struct {
			Start float64 `json:"start"`
			End   float64 `json:"end"`
			Mouth string  `json:"mouth"`
		} `json:"timeline"`
		Duration float64  `json:"duration"`
		Frames   [][4]int `json:"frames"`
	}
	readGolden(t, "frames_boundary.json", &g)
	var tl []timelineLine
	for _, x := range g.Timeline {
		tl = append(tl, timelineLine{Start: x.Start, End: x.End, mouth: x.Mouth})
	}
	if got := frameRuns(tl, g.Duration); !reflect.DeepEqual(got, g.Frames) {
		t.Errorf("got  %v\nwant %v", got, g.Frames)
	}
}

func TestStemMatchesPython(t *testing.T) {
	var cases []struct{ Name, Stem string }
	readGolden(t, "stem.json", &cases)
	if len(cases) == 0 {
		t.Fatal("golden が空")
	}
	for _, c := range cases {
		if got := pyStem(c.Name); got != c.Stem {
			t.Errorf("%q: got %q want %q", c.Name, got, c.Stem)
		}
	}
}

// 本番の主経路: 台本に faces を書かず、skill の assets にある立ち絵を使う。空文字のチャプターはチャプターにならない。
// 期待値は golden の script (faces を台本に書いた版) と同じ。
func TestBuildWithDefaultAssetsMatchesPython(t *testing.T) {
	var golden map[string]struct {
		Data map[string]any `json:"data"`
	}
	readGolden(t, "build_data.json", &golden)
	dir := t.TempDir()
	copyTree(t, filepath.Join("testdata", "build", "script.work"), filepath.Join(dir, "script.work"))
	copyTree(t, filepath.Join("testdata", "build", "faces", "metan"), filepath.Join(dir, "assets", "metan"))
	b, err := os.ReadFile(filepath.Join("testdata", "build", "script.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := decodeJSON(b, &raw); err != nil {
		t.Fatal(err)
	}
	delete(raw["cast"].(map[string]any)["metan"].(map[string]any), "faces")
	raw["lines"].([]any)[2].(map[string]any)["chapter"] = ""
	out, _ := json.Marshal(raw)
	path := filepath.Join(dir, "script.json")
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	env := testEnv(t)
	env.AssetsFaces = filepath.Join(dir, "assets")
	s, err := loadScript(resolvePath(path), env)
	if err != nil {
		t.Fatal(err)
	}
	data, _, err := assemble(s, env)
	if err != nil {
		t.Fatal(err)
	}
	gb, _ := json.Marshal(data)
	var got map[string]any
	if err := decodeJSON(gb, &got); err != nil {
		t.Fatal(err)
	}
	g, ok := golden["script"]
	if !ok || len(g.Data) == 0 {
		t.Fatal("golden に script が無い (比較 0 件のまま通らないように)")
	}
	want := g.Data
	normalizeNumbers(got)
	normalizeNumbers(want)
	for _, k := range sortedKeys(want) {
		if !reflect.DeepEqual(got[k], want[k]) {
			t.Errorf("%s が golden と違う (既定の置き場の立ち絵を読んでいないか、空のチャプターを数えた)", k)
		}
	}
}

// まだ話していないキャラは「通常」の顔で出るので、立ち絵を持つキャラには通常を必ず埋め込む。
func TestDefaultFaceAlwaysEmbedded(t *testing.T) {
	dir := t.TempDir()
	facesAbs, _ := filepath.Abs(filepath.Join("testdata", "build", "faces", "metan"))
	script := map[string]any{
		"cast":  map[string]any{"metan": map[string]any{"faces": facesAbs}},
		"lines": []any{map[string]any{"who": "zundamon", "text": "ぼくだけが話すのだ"}},
	}
	path := filepath.Join(dir, "solo.json")
	writeScriptFile(t, path, script)
	env := testEnv(t)
	s, err := loadScript(path, env)
	if err != nil {
		t.Fatal(err)
	}
	imgs, err := loadFaces(s, env)
	if err != nil {
		t.Fatal(err)
	}
	if got := sortedKeys(imgs["metan"]); !slices.Equal(got, []string{"通常"}) || len(imgs["metan"]["通常"]) != 3 {
		t.Errorf("話さないキャラの立ち絵: %v (want [通常] の 3 段階)", got)
	}
}

// frameStates はフレームごとの状態を、frameRuns の区間どおりに総フレーム数 ceil(尺 × fps) まで並べる (issue 660)
func TestFrameStates(t *testing.T) {
	runs := [][4]int{{0, -1, 0, 0}, {12, 0, 1, 2}, {15, 0, 0, 0}}
	got := frameStates(runs, 0.7) // 0.7 秒 = 21 フレーム
	var want [][3]int
	for range 12 {
		want = append(want, [3]int{-1, 0, 0})
	}
	for range 3 {
		want = append(want, [3]int{0, 1, 2})
	}
	for range 6 {
		want = append(want, [3]int{0, 0, 0})
	}
	if !slices.Equal(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
	// frameRuns と組み合わせると、フレーム k は時刻 (k+0.5)/fps の状態になる (プレイヤーの規則)
	tl := []timelineLine{{Start: 0.412, End: 0.6, mouth: "2"}, {Start: 0.938, End: 1.1, mouth: "1"}}
	fs := frameStates(frameRuns(tl, 1.2), 1.2)
	if len(fs) != 36 {
		t.Fatalf("総フレーム数: got %d want 36", len(fs))
	}
	for k, st := range fs {
		tm := (float64(k) + 0.5) / mouthFPS
		li := -1
		for i, l := range tl {
			if l.Start <= tm {
				li = i
			}
		}
		if st[0] != li {
			t.Errorf("フレーム %d (時刻 %.4f) の行: got %d want %d", k, tm, st[0], li)
		}
	}
}

// synth は合成の入力 (話速などの scale) をエンジンへ渡し、キャッシュの鍵どおりの名前で保存し、壊れた wav は残さない。
func TestSynthWithFakeEngine(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]any
	broken := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/speakers":
			_, _ = io.WriteString(w, `[{"name":"四国めたん","styles":[{"name":"ノーマル","id":2}]},{"name":"ずんだもん","styles":[{"name":"ノーマル","id":3}]}]`)
		case "/audio_query":
			_, _ = io.WriteString(w, `{"accent_phrases":[],"speedScale":1.0,"kana":"テスト","unknownField":{"keep":true}}`)
		case "/synthesis":
			b, _ := io.ReadAll(r.Body)
			var q map[string]any
			_ = json.Unmarshal(b, &q)
			mu.Lock()
			bodies = append(bodies, q)
			mu.Unlock()
			if broken {
				_, _ = w.Write([]byte("RIFFnope"))
				return
			}
			_, _ = w.Write(wavBytes(fmtChunk(1, 24000, 16), chunk("data", make([]byte, 480))))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "s.json")
	writeScriptFile(t, path, map[string]any{"lines": []any{map[string]any{"who": "metan", "text": "あ", "speed": 1.25, "pitch": 0.03, "intonation": 1.1, "volume": 0.8}}})
	env := testEnv(t)
	env.Engine = srv.URL
	if err := cmdSynth(env, path, false); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 1 {
		t.Fatalf("/synthesis の呼び出し: %d 回", len(bodies))
	}
	q := bodies[0]
	for k, want := range map[string]float64{"speedScale": 1.25, "pitchScale": 0.03, "intonationScale": 1.1, "volumeScale": 0.8, "outputSamplingRate": 24000} {
		if got, _ := q[k].(float64); got != want {
			t.Errorf("/synthesis の %s: got %v want %v", k, q[k], want)
		}
	}
	if q["outputStereo"] != false || q["unknownField"] == nil {
		t.Errorf("outputStereo か、エンジンの知らないフィールドが落ちた: %v", q)
	}
	s, err := loadScript(resolvePath(path), env)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := lineParams(s, s.Lines[0])
	wav, query := cachePaths(workDir(s.Path), p)
	if !isFile(wav) || !isFile(query) {
		t.Fatalf("キャッシュの鍵どおりの名前で保存されていない: %s", workDir(s.Path))
	}
	// 2 回目はキャッシュを使う (エンジンを呼ばない)
	if err := cmdSynth(env, path, false); err != nil || len(bodies) != 1 {
		t.Errorf("キャッシュを使わずに合成し直した (呼び出し %d 回, %v)", len(bodies), err)
	}
	// 壊れた wav が返ったら、何も残さずに止まる
	broken = true
	_ = os.Remove(wav)
	_ = os.Remove(query)
	if err := cmdSynth(env, path, false); err == nil {
		t.Error("壊れた wav で成功した")
	}
	entries, _ := os.ReadDir(workDir(s.Path))
	if len(entries) != 0 {
		t.Errorf("壊れた合成結果がキャッシュに残った: %v", entries)
	}
}

func TestArgparseEdgeCases(t *testing.T) {
	specs := []optSpec{
		{names: []string{"-o", "--output"}, dest: "output", takes: true},
		{names: []string{"--jobs"}, dest: "jobs", takes: true, check: jobsArg},
	}
	p, _, err := parseArgs([]string{"-o=out", "s.json"}, specs, false)
	if err != nil || p.opts["output"] != "out" {
		t.Errorf("-o=out: output=%q err=%v (argparse は = の後ろを値にする)", p.opts["output"], err)
	}
	p, _, err = parseArgs([]string{"-o", "-x y", "-a b"}, specs, false)
	if err != nil || p.opts["output"] != "-x y" || !slices.Equal(p.pos, []string{"-a b"}) {
		t.Errorf("空白を含む値はオプションと見なさない: %+v err=%v", p, err)
	}
	p, _, err = parseArgs([]string{"-h", "--jobs", "99"}, specs, false)
	if err != nil || !p.help {
		t.Errorf("-h の後ろの誤りより -h を優先する: err=%v", err)
	}
}

func TestLoadScriptRejectsMalformedFiles(t *testing.T) {
	cases := map[string]string{
		"余分な閉じ括弧":      `{"lines":[{"who":"metan","text":"a"}]}}`,
		"余分な閉じ括弧 (配列)": `{"lines":[{"who":"metan","text":"a"}]}]`,
		"不正な UTF-8":    "{\"lines\":[{\"who\":\"metan\",\"text\":\"\xff\"}]}",
	}
	for name, body := range cases {
		path := filepath.Join(t.TempDir(), "s.json")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := loadScript(path, testEnv(t)); err == nil || !strings.Contains(err.Error(), "読めない") {
			t.Errorf("%s: 受け入れた (%v)", name, err)
		}
	}
}

func TestWavOddLengthAndFloat(t *testing.T) {
	odd := wavBytes(fmtChunk(1, 24000, 16), chunk("data", make([]byte, 201)))
	if pcm, n, err := checkWavBytes(odd, "t"); err != nil || n != 100 || len(pcm) != 200 {
		t.Errorf("奇数長の data を読めない: n=%d len=%d err=%v", n, len(pcm), err)
	}
	ext := binaryFmtExtensible(3) // subformat = IEEE float
	if _, _, err := checkWavBytes(wavBytes(ext, chunk("data", make([]byte, 200))), "t"); err == nil {
		t.Error("拡張形式の float を PCM として受け入れた")
	}
	if _, _, err := checkWavBytes(wavBytes(binaryFmtExtensible(1), chunk("data", make([]byte, 200))), "t"); err != nil {
		t.Errorf("拡張形式の PCM を読めない: %v", err)
	}
	// 先頭 2 バイトだけ PCM で、残りが違う GUID は拒否する (Python 3.14 は GUID 全体を比べる)
	fake := binaryFmtExtensible(1)
	fake[len(fake)-1] ^= 0xff
	if _, _, err := checkWavBytes(wavBytes(fake, chunk("data", make([]byte, 200))), "t"); err == nil {
		t.Error("PCM でない GUID を受け入れた")
	}
}

func binaryFmtExtensible(sub uint16) []byte {
	b := []byte{0xFE, 0xFF, 1, 0}                                                    // tag, channels
	b = append(b, 0xC0, 0x5D, 0, 0)                                                  // 24000
	b = append(b, 0x80, 0xBB, 0, 0, 2, 0, 16, 0)                                     // byte rate, block align, bits
	b = append(b, 22, 0, 16, 0, 0, 0, 0, 0)                                          // cbSize, valid bits, channel mask
	guid := append([]byte{byte(sub), byte(sub >> 8)}, ksdataformatSubtypePCM[2:]...) // subformat の GUID
	b = append(b, guid...)
	return chunk("fmt ", b)
}

// ".." の直前が symlink のとき、Python の Path.resolve() と同じく symlink を解決した後の親へ戻る。
func TestResolvePathSymlinkThenParent(t *testing.T) {
	root := resolvePath(t.TempDir())
	real := filepath.Join(root, "real", "deep", "proj")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "home"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "home", "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if got, want := resolvePath(link+"/../s.json"), filepath.Join(root, "real", "deep", "s.json"); got != want {
		t.Errorf("got %s want %s", got, want)
	}
	t.Chdir(link)
	if got, want := resolvePath("../s.json"), filepath.Join(root, "real", "deep", "s.json"); got != want {
		t.Errorf("相対パス: got %s want %s", got, want)
	}
}

// JSON の整数の -0 は、Python では 0 (int) になり float() で 0.0 になる。-0.0 にすると鍵が Python 版と違う (敵対的レビュー P2-1)。
// golden の cachekey.json の最後の件は、Python に -0 を与えて作った鍵 (golden の line には Python が読んだ後の 0 が書かれている)。
func TestNegativeZeroIntLiteralKey(t *testing.T) {
	var cases []struct {
		Key string `json:"key"`
	}
	readGolden(t, "cachekey.json", &cases)
	want := cases[len(cases)-1].Key
	line := map[string]any{"who": "metan", "text": "a", "pitch": json.Number("-0"), "volume": json.Number("-0"), "intonation": json.Number("-0")}
	s := &Script{Raw: map[string]any{}, Cast: map[string]map[string]any{"metan": {"style_id": json.Number("2")}}}
	p, err := lineParams(s, line)
	if err != nil {
		t.Fatal(err)
	}
	if got := cacheKey(p); got != want {
		t.Errorf("整数の -0 の鍵が Python 版と違う: got %s want %s (%s)", got, want, cacheSerialize(p))
	}
}

// 舞台の右上の作成日: 台本の date を優先し、無ければ build した日。プレイヤーのデータに載ることまで見る。
func TestCreatedDate(t *testing.T) {
	env := testEnv(t)
	env.Now = func() time.Time { return time.Date(2026, 10, 6, 23, 59, 0, 0, time.Local) }
	s, err := loadScript(resolvePath(filepath.Join("testdata", "build", "script.json")), env)
	if err != nil {
		t.Fatal(err)
	}
	data, _, err := assemble(s, env)
	if err != nil {
		t.Fatal(err)
	}
	if data.Date != "2026-10-06" {
		t.Errorf("date の無い台本の作成日: %q (want build した日 2026-10-06)", data.Date)
	}
	s.Raw["date"] = "2026-09-30"
	if got := createdDate(s, env); got != "2026-09-30" {
		t.Errorf("台本の date を使っていない: %q", got)
	}
	for name, d := range map[string]any{"空": " ", "数": 20261006} {
		path := writeScript(t, map[string]any{"date": d, "lines": []any{map[string]any{"who": "metan", "text": "a"}}})
		if _, err := loadScript(path, env); err == nil || !strings.Contains(err.Error(), "date") {
			t.Errorf("%s の date を受け入れた (%v)", name, err)
		}
	}
}

// TestCreatedDateRenderedByPlayer は、player.html が作成日を舞台に出していることを確かめる (issue 646 の 2)。
// TestCreatedDate は PlayerData.Date までしか見ないので、要素や代入を消しても緑のままだった。
// 代入はまとめ撮り (#sheet=) の分岐より前に置く (分岐の後ろだと、mp4 の絵に作成日が写らない)。
func TestCreatedDateRenderedByPlayer(t *testing.T) {
	tb, err := os.ReadFile(testEnv(t).Template())
	if err != nil {
		t.Fatal(err)
	}
	tpl := string(tb)
	if !regexp.MustCompile(`<div class="stage-date" id="stage-date"></div>`).MatchString(tpl) {
		t.Error("舞台に作成日の要素 (#stage-date) が無い")
	}
	assign := regexp.MustCompile(`\$\('stage-date'\)\.textContent = D\.date \?`).FindStringIndex(tpl)
	sheet := strings.Index(tpl, "location.hash.match(/^#sheet=")
	switch {
	case assign == nil:
		t.Error("作成日 (D.date) を #stage-date に入れていない")
	case sheet < 0:
		t.Error("まとめ撮りの分岐が見つからない (テストの前提が変わった)")
	case assign[0] > sheet:
		t.Error("作成日の代入がまとめ撮りの分岐より後ろにある (mp4 に写らない)")
	}
}

// synth の「台本から外れた古いファイル」は合成のキャッシュだけを数える (build が置く mermaid/ を数えて、
// work/ ごと消すよう案内しない。issue 652)
func TestSynthCountsOnlyStaleCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/speakers":
			_, _ = io.WriteString(w, `[{"name":"四国めたん","styles":[{"name":"ノーマル","id":2}]},{"name":"ずんだもん","styles":[{"name":"ノーマル","id":3}]}]`)
		case "/audio_query":
			_, _ = io.WriteString(w, `{"accent_phrases":[],"speedScale":1.0}`)
		case "/synthesis":
			_, _ = w.Write(wavBytes(fmtChunk(1, 24000, 16), chunk("data", make([]byte, 480))))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "s.json")
	must(t, os.WriteFile(path, []byte(`{"lines":[{"who":"metan","text":"あ"}]}`), 0o644))
	env := testEnv(t)
	env.Engine = srv.URL
	must(t, cmdSynth(env, path, false))
	wd := filepath.Join(dir, "s.work")
	must(t, os.MkdirAll(filepath.Join(wd, "mermaid"), 0o755))
	must(t, os.WriteFile(filepath.Join(wd, "mermaid", "x.png"), []byte("png"), 0o644))
	must(t, os.WriteFile(filepath.Join(wd, "old.wav"), []byte("RIFF"), 0o644))
	var errb strings.Builder
	env.Stderr = &errb
	must(t, cmdSynth(env, path, false))
	if !strings.Contains(errb.String(), "古いキャッシュ 1 件") || strings.Contains(errb.String(), "ごと消して") {
		t.Errorf("古いキャッシュは old.wav の 1 件のはずで、work/ ごと消すよう案内しない (mermaid/ を消させない): %s", errb.String())
	}
}

// 出力先に書けないなら、音声の圧縮や撮影より前に止まる (issue 652)。無い dir / ディレクトリがある / 読み取り専用の既存のファイル
func TestBuildChecksOutputDirFirst(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(dir string) string // 出力の base を返す
	}{
		{"無い dir", func(dir string) string { return filepath.Join(dir, "no-such-dir", "out") }},
		{"ディレクトリがある", func(dir string) string {
			must(t, os.Mkdir(filepath.Join(dir, "out.mp4"), 0o755))
			return filepath.Join(dir, "out")
		}},
		{"自分を指す symlink", func(dir string) string {
			must(t, os.Symlink("out.mp4", filepath.Join(dir, "out.mp4")))
			return filepath.Join(dir, "out")
		}},
		{"読み取り専用の既存のファイル", func(dir string) string {
			must(t, os.WriteFile(filepath.Join(dir, "out.mp4"), []byte("old"), 0o444))
			return filepath.Join(dir, "out")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log := fakeMP4Tools(t)
			env := testEnv(t)
			// 確認をすり抜けたときも先へ進めるようにする (未設定だと撮影の前の CPU の空き待ちで落ち、何を見逃したかが分からない)
			env.LoadAvg = func() (float64, bool) { return 0, true }
			env.Now, env.Sleep = time.Now, func(time.Duration) {}
			out := tc.setup(t.TempDir())
			err := cmdBuild(env, filepath.Join("testdata", "build", "script.json"), out, "mp4", 1, 64)
			if err == nil || !strings.Contains(err.Error(), "書けない") {
				t.Fatalf("書けない出力先で止まるはず: %v", err)
			}
			for _, l := range calls(t, log) {
				if strings.HasPrefix(l, "ffmpeg ") || strings.HasPrefix(l, "chrome ") {
					t.Errorf("出力先を確かめる前に外部コマンドを起こした: %s", l)
				}
			}
		})
	}
}

// writeWav のヘッダの各欄を直接見る (自前の parser は byte rate / block align を読まないので、往復では検査されない。issue 653)
func TestWriteWavHeaderFields(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.wav")
	pcm := make([]byte, 480)
	must(t, writeWav(p, pcm))
	b, err := os.ReadFile(p)
	must(t, err)
	le32 := func(i int) int { return int(binary.LittleEndian.Uint32(b[i : i+4])) }
	le16 := func(i int) int { return int(binary.LittleEndian.Uint16(b[i : i+2])) }
	for _, c := range []struct {
		name      string
		got, want int
	}{
		{"RIFF の大きさ", le32(4), 36 + len(pcm)},
		{"fmt の長さ", le32(16), 16},
		{"形式 (PCM)", le16(20), 1},
		{"チャンネル", le16(22), 1},
		{"サンプリング周波数", le32(24), sampleRate},
		{"byte rate", le32(28), sampleRate * 2},
		{"block align", le16(32), 2},
		{"ビット数", le16(34), 16},
		{"data の大きさ", le32(40), len(pcm)},
	} {
		if c.got != c.want {
			t.Errorf("%s: got %d want %d", c.name, c.got, c.want)
		}
	}
	if string(b[0:4]) != "RIFF" || string(b[8:16]) != "WAVEfmt " || string(b[36:40]) != "data" {
		t.Errorf("チャンクの名前が違う: %q", b[:44])
	}
}
