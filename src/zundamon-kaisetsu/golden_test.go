package main

// Python 版 (dialogue_video.py) を正解役にした golden との突き合わせ (issue 641)。
// testdata/*.json は Python 版の関数を直接呼んで書き出したもので、Python 版を消した後も回帰テストとして残す。
// 生成に使った Python は 3.14.7 (sum の Neumaier の補正は 3.12 以降、Path.stem の規則は 3.14 の振る舞い。版が違うと golden も変わる)。
// Python 版は git の履歴 (issue 641 の 39fe8c02 より前) にあり、`git show <その前の commit>:_claude/skills/zundamon-kaisetsu/scripts/dialogue_video.py`
// で取り出せば、ケースを足すときの正解役として呼べる。

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func readGolden(t *testing.T, name string, v any) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := decodeJSON(b, v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

// testEnv は skill の実素材 (非公開) を見ない Env。テンプレートは repo の skill から読む。

func TestCacheKeyMatchesPython(t *testing.T) {
	var cases []struct {
		Line         map[string]any            `json:"line"`
		ScriptSpeed  any                       `json:"script_speed"`
		CastOverride map[string]map[string]any `json:"cast_override"`
		Serialized   string                    `json:"serialized"`
		Key          string                    `json:"key"`
	}
	readGolden(t, "cachekey.json", &cases)
	if len(cases) < 20 {
		t.Fatalf("golden の件数が少ない: %d", len(cases))
	}
	for i, c := range cases {
		s := &Script{Raw: map[string]any{}, Cast: map[string]map[string]any{}}
		if c.ScriptSpeed != nil {
			s.Raw["speed"] = c.ScriptSpeed
		}
		for _, def := range castOrder {
			m := map[string]any{"style_id": json.Number(strconv.FormatInt(def.StyleID, 10))}
			for k, v := range c.CastOverride[def.Key] {
				m[k] = v
			}
			s.Cast[def.Key] = m
		}
		p, err := lineParams(s, c.Line)
		if err != nil {
			t.Fatalf("case %d (%v): %v", i, c.Line, err)
		}
		if got := cacheSerialize(p); got != c.Serialized {
			t.Errorf("case %d: 直列化が違う\n got  %s\n want %s", i, got, c.Serialized)
		}
		if got := cacheKey(p); got != c.Key {
			t.Errorf("case %d: 鍵が違う got %s want %s", i, got, c.Key)
		}
	}
}

func TestMouthTrackMatchesPython(t *testing.T) {
	var cases []struct {
		Query    string  `json:"query"`
		Duration float64 `json:"duration"`
		Mouth    string  `json:"mouth"`
	}
	readGolden(t, "mouth.json", &cases)
	if len(cases) == 0 {
		t.Fatal("golden が空")
	}
	for _, c := range cases {
		b, err := os.ReadFile(filepath.Join("testdata", "build", "script.work", c.Query))
		if err != nil {
			t.Fatal(err)
		}
		var q map[string]any
		if err := decodeJSON(b, &q); err != nil {
			t.Fatal(err)
		}
		if got := mouthTrack(q, c.Duration); got != c.Mouth {
			t.Errorf("%s (%.4f 秒): 口の開きが違う\n got  %s\n want %s", c.Query, c.Duration, got, c.Mouth)
		}
	}
}

func TestSpokenTextMatchesPython(t *testing.T) {
	var cases []struct {
		Text     string         `json:"text"`
		Read     any            `json:"read"`
		Readings map[string]any `json:"readings"`
		Spoken   string         `json:"spoken"`
	}
	readGolden(t, "spoken.json", &cases)
	if len(cases) < 5 { // 空の golden で比較 0 件のまま通らないように (今は 7 件)
		t.Fatalf("golden の件数が少ない: %d", len(cases))
	}
	for _, c := range cases {
		line := map[string]any{"text": c.Text}
		if c.Read != nil {
			line["read"] = c.Read
		}
		s := &Script{Raw: map[string]any{"readings": c.Readings}}
		if got := spokenText(s, line); got != c.Spoken {
			t.Errorf("%q: got %q want %q", c.Text, got, c.Spoken)
		}
	}
}

func TestRoundMatchesPython(t *testing.T) {
	var cases []struct {
		X      float64 `json:"x"`
		Round3 float64 `json:"round3"`
		Round  int     `json:"round"`
	}
	readGolden(t, "round.json", &cases)
	if len(cases) < 10 { // 空の golden で比較 0 件のまま通らないように (今は 20 件)
		t.Fatalf("golden の件数が少ない: %d", len(cases))
	}
	for _, c := range cases {
		if got := pyRound3(c.X); got != c.Round3 {
			t.Errorf("round(%v, 3): got %v want %v", c.X, got, c.Round3)
		}
		if got := pyRound(c.X); got != c.Round {
			t.Errorf("round(%v): got %v want %v", c.X, got, c.Round)
		}
	}
}

// TestRejectMatchesPython は台本の検証の受け入れ / 拒否と文言が Python 版と同じであることを確かめる。
// Python の例外文をそのまま含む文言 (exact=false) は、括弧の前までを比べる。
func TestRejectMatchesPython(t *testing.T) {
	var cases []struct {
		Name   string `json:"name"`
		Script any    `json:"script"`
		Error  any    `json:"error"`
		Exact  bool   `json:"exact"`
	}
	readGolden(t, "reject.json", &cases)
	if len(cases) < 15 {
		t.Fatalf("golden の件数が少ない: %d", len(cases))
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			dir := t.TempDir()
			copyTree(t, filepath.Join("testdata", "build", "faces"), filepath.Join(dir, "faces"))
			path := filepath.Join(dir, "_reject_"+c.Name+".json")
			body := []byte("{not json")
			if c.Script != nil {
				var err error
				if body, err = json.Marshal(c.Script); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(path, body, 0o644); err != nil {
				t.Fatal(err)
			}
			real := resolvePath(path)
			_, err := loadScript(real, testEnv(t))
			want, _ := c.Error.(string)
			if want == "" {
				if err != nil {
					t.Fatalf("Python は受け入れたのに拒否した: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Python は拒否したのに受け入れた (want %q)", want)
			}
			got := strings.ReplaceAll(strings.ReplaceAll(err.Error(), real, "<SCRIPT>"), resolvePath(dir), "<DIR>")
			if !c.Exact {
				got, _, _ = strings.Cut(got, " (")
				want, _, _ = strings.Cut(want, " (")
			}
			if got != want {
				t.Errorf("文言が違う\n got  %s\n want %s", got, want)
			}
		})
	}
}

// TestBuildDataMatchesPython は合成済みの wav と query から作るプレイヤーのデータと連結した音声が、Python 版と同じであることを確かめる。
// wav の名前は Python 版のキャッシュの鍵なので、Go 版が同じ鍵を作れなければ「合成済みの wav が無い」で落ちる。
func TestBuildDataMatchesPython(t *testing.T) {
	var golden map[string]struct {
		Data   map[string]any `json:"data"`
		PCMSha string         `json:"joined_pcm_sha256"`
	}
	readGolden(t, "build_data.json", &golden)
	for _, name := range []string{"script", "solo"} {
		t.Run(name, func(t *testing.T) {
			g, ok := golden[name]
			if !ok {
				t.Fatalf("golden に %s が無い", name)
			}
			env := testEnv(t)
			s, err := loadScript(resolvePath(filepath.Join("testdata", "build", name+".json")), env)
			if err != nil {
				t.Fatal(err)
			}
			data, pcm, err := assemble(s, env)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(pcm)
			if got := hex.EncodeToString(sum[:]); got != g.PCMSha {
				t.Errorf("連結した音声が違う: got %s want %s", got, g.PCMSha)
			}
			b, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err := decodeJSON(b, &got); err != nil {
				t.Fatal(err)
			}
			normalizeNumbers(got)
			normalizeNumbers(g.Data)
			for _, k := range sortedKeys(g.Data) {
				if !reflect.DeepEqual(got[k], g.Data[k]) {
					gj, _ := json.Marshal(got[k])
					wj, _ := json.Marshal(g.Data[k])
					t.Errorf("%s が違う\n got  %s\n want %s", k, firstBytes(gj, 600), firstBytes(wj, 600))
				}
			}
			for _, k := range sortedKeys(got) {
				// Go に移した後で足したキーは Python 版に無い。名前で挙げて除き、それ以外の余分なキーは今までどおり落とす
				if slices.Contains(keysAddedAfterPort, k) {
					continue
				}
				if _, ok := g.Data[k]; !ok {
					t.Errorf("Python 版に無いキー %s がある", k)
				}
			}
		})
	}
}

// keysAddedAfterPort はプレイヤーのデータのうち、Python 版 (golden) の後で足したキー。中身は TestCreatedDate などが見る。
var keysAddedAfterPort = []string{"date", "overlays"}

func firstBytes(b []byte, n int) []byte {
	if len(b) > n {
		return append(b[:n:n], []byte("…")...)
	}
	return b
}

// normalizeNumbers は json.Number を float64 にする (1 と 1.0 を同じ値として比べる)。
func normalizeNumbers(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			x[k] = normalizeNumbers(e)
		}
		return x
	case []any:
		for i, e := range x {
			x[i] = normalizeNumbers(e)
		}
		return x
	case json.Number:
		f, _ := x.Float64()
		return f
	default:
		return v
	}
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		out := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(out, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestSumMatchesPython は pySum が Python 3.12 以降の sum() (Neumaier の補正つき) と同じ値を返すことを確かめる。
// 口の開きの伸縮率はこの合計から作るので、素直に足すと境目が 1 フレームずれることがある (設計レビュー P1-4)。
func TestSumMatchesPython(t *testing.T) {
	var cases []struct {
		Xs           []float64 `json:"xs"`
		Sum          float64   `json:"sum"`
		NaiveDiffers bool      `json:"naive_differs"`
	}
	readGolden(t, "sum.json", &cases)
	differs := 0
	for i, c := range cases {
		if got := pySum(c.Xs); got != c.Sum {
			t.Errorf("case %d: got %v want %v", i, got, c.Sum)
		}
		if c.NaiveDiffers {
			differs++
		}
	}
	if differs == 0 {
		t.Fatal("素直な加算と値が違う列が golden に無い (補正を外しても赤くならない)")
	}
}
