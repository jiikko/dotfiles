package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	sampleRate = 24000 // VOICEVOX の既定出力。全セリフをこの値に揃えて連結する
	mouthFPS   = 30
)

// mouthLevels は口の開きの段階の数 (立ち絵は表情ごとに <表情>_0〜_<mouthLevels-1>.webp)。
// 🚨 player.html のまとめ撮りの正規表現 (#sheet= の口の欄 [012]) と setMouth の段階も同じ数で書いている。
// 変えたら両方を直す (TestSheetFragmentMatchesPlayer が食い違いを落とす。issue 650)
const mouthLevels = 3

// 母音ごとの口の開き (0 閉じ / 1 半開き / 2 開き)。大文字 (無声化母音) は表に無いので 0 = ほぼ音が出ない
var vowelMouth = map[string]int{"a": 2, "o": 2, "i": 1, "u": 1, "e": 1, "N": 0, "cl": 0, "pau": 0}

var lipClosedConsonants = map[string]bool{"m": true, "my": true, "b": true, "by": true, "p": true, "py": true}

// --- wav (mono / 16bit / 24kHz の PCM だけを扱う) ---

// ksdataformatSubtypePCM は拡張形式の wav の subformat で PCM を表す GUID (00000001-0000-0010-8000-00aa00389b71)。
var ksdataformatSubtypePCM = []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}

// --- 口の開き ---

// numOr は数に読めればその値、キーが無い・null・数でなければ既定値 (エンジンは null を返さないので、Python 版の
// query.get(key, default) の「null なら None で落ちる」は再現しない)。
func numOr(v any, def float64) float64 {
	if v == nil {
		return def
	}
	if f, err := pyFloat(v); err == nil {
		return f
	}
	return def
}

// mouthTrack は audio_query のモーラ長から、mouthFPS ごとの口の開き (0/1/2) を並べた文字列を作る。
//
// モーラ長は speedScale で割った値が実際の発話長になる。前後の無音 (pre/postPhonemeLength) を含めた
// 合計が wav の長さと食い違う分は、全体を線形に伸縮して吸収する (エンジンの版で前後無音の扱いが違っても
// 口が音より先行・遅延し続けないようにするため)。
func mouthTrack(query map[string]any, duration float64) string {
	speed := numOr(query["speedScale"], 1.0)
	if speed == 0 {
		speed = 1.0
	}
	type seg struct {
		d  float64
		lv int
	}
	segs := []seg{{numOr(query["prePhonemeLength"], 0.1), 0}}
	phrases, _ := query["accent_phrases"].([]any)
	for _, apRaw := range phrases {
		ap, _ := apRaw.(map[string]any)
		moras, _ := ap["moras"].([]any)
		for _, mRaw := range moras {
			m, _ := mRaw.(map[string]any)
			consLen := numOr(m["consonant_length"], 0) / speed
			if consLen != 0 {
				lv := 1
				if c, ok := m["consonant"].(string); ok && lipClosedConsonants[c] {
					lv = 0
				}
				segs = append(segs, seg{consLen, lv})
			}
			v, _ := m["vowel"].(string)
			segs = append(segs, seg{numOr(m["vowel_length"], 0) / speed, vowelMouth[v]})
		}
		if pm, ok := ap["pause_mora"].(map[string]any); ok && len(pm) > 0 {
			segs = append(segs, seg{numOr(pm["vowel_length"], 0) / speed, 0})
		}
	}
	segs = append(segs, seg{numOr(query["postPhonemeLength"], 0.1), 0})
	ds := make([]float64, len(segs))
	for i, s := range segs {
		ds[i] = s.d
	}
	nominal := pySum(ds)
	if nominal == 0 {
		nominal = duration
	}
	ratio := duration / nominal
	type edge struct {
		t  float64
		lv int
	}
	edges := make([]edge, 0, len(segs))
	acc := 0.0
	for _, s := range segs {
		// float64() で積を一度丸める。無いと arm64 では掛け算と足し算が FMA (丸め 1 回) にまとめられ、最後の桁が
		// Python と違う。境目がフレームの中点とちょうど重なると口の開きが 1 フレーム反転する (Go の仕様は、明示的な
		// 変換でこのまとめを止められると定めている。golden の mouth_synth.json の devoiced 0.95 秒で再現)
		acc += float64(s.d * ratio)
		edges = append(edges, edge{acc, s.lv})
	}
	n := max(1, pyRound(duration*mouthFPS))
	var b strings.Builder
	j := 0
	for k := range n {
		t := (float64(k) + 0.5) / mouthFPS
		for j < len(edges)-1 && edges[j].t <= t {
			j++
		}
		b.WriteByte(byte('0' + edges[j].lv))
	}
	return b.String()
}

// pySum は Python 3.12 以降の sum() (浮動小数は Neumaier の補正つきで足す)。素直に足すと最後の桁がずれ、
// 口の開きの境目が Python 版と 1 フレームずれることがある。
func pySum(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	f, c := xs[0], 0.0
	for _, x := range xs[1:] {
		t := f + x
		if math.Abs(f) >= math.Abs(x) {
			c += (f - t) + x
		} else {
			c += (x - t) + f
		}
		f = t
	}
	if c != 0 && !math.IsInf(c, 0) && !math.IsNaN(c) {
		f += c
	}
	return f
}

// appendSilence は pcm の後ろに n バイトの無音 (0) を足す (無音の一時的な領域を作らない)
func appendSilence(pcm []byte, n int) []byte {
	l := len(pcm)
	pcm = slices.Grow(pcm, n)[:l+n]
	clear(pcm[l:])
	return pcm
}

// --- 表示の状態 ---

// timelineLine はプレイヤーに渡す 1 行 (Python 版の timeline の要素)。mouth はデータに入れない。
type timelineLine struct {
	Who     string            `json:"who"`
	Text    string            `json:"text"`
	Faces   map[string]string `json:"faces"`
	Chapter int               `json:"chapter"`
	Show    *int              `json:"show,omitempty"`
	Step    *int              `json:"step,omitempty"` // 段のある図解の何段目まで見せるか (0 始まり)。段の無い図解・図解の無い行では載せない
	Start   float64           `json:"start"`
	End     float64           `json:"end"`
	mouth   string
}

// frameRuns は mouthFPS ごとの見た目の状態を、変わり目だけの列 [開始フレーム, 字幕の行, 話し中か, 口の開き] にする。
//
// HTML プレイヤーの再生と mp4 の画像は、どちらもこの列から描く (状態の判定をここ 1 か所に置く)。
// 字幕は、次の行が始まるまで直前の行を出し続ける。口が動くのはその行の発話中 (start <= t < end) だけ。
func frameRuns(timeline []timelineLine, duration float64) [][4]int {
	var runs [][4]int
	li := -1
	n := max(1, int(math.Ceil(duration*mouthFPS)))
	for k := range n {
		t := (float64(k) + 0.5) / mouthFPS
		for li+1 < len(timeline) && timeline[li+1].Start <= t {
			li++
		}
		speaking, level := 0, 0
		if li >= 0 && t < timeline[li].End {
			mouth := timeline[li].mouth
			idx := int((t - timeline[li].Start) * mouthFPS)
			speaking = 1
			if idx < len(mouth) {
				level = int(mouth[idx] - '0')
			}
		}
		if len(runs) == 0 || runs[len(runs)-1][1] != li || runs[len(runs)-1][2] != speaking || runs[len(runs)-1][3] != level {
			runs = append(runs, [4]int{k, li, speaking, level})
		}
	}
	return runs
}

// lineFaces は各行の時点で、それぞれのキャラが見せる表情。話す行は face (省略は通常)、話さない側は最後に話したときの表情のまま。
func lineFaces(lines []map[string]any) []map[string]string {
	cur := map[string]string{}
	for _, k := range castKeys() {
		cur[k] = defaultFace
	}
	out := make([]map[string]string, 0, len(lines))
	for _, line := range lines {
		cur[pyStr(line["who"])] = pyStr(lineFace(line))
		snap := make(map[string]string, len(cur))
		for k, v := range cur {
			snap[k] = v
		}
		out = append(out, snap)
	}
	return out
}

func dataURI(path, mime string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b), nil
}

// faceSet は台本で使う表情の立ち絵 ({キャラ: {表情: [閉じ, 半開き, 開き] の data URI}})。blinks は同じ形の閉じ目の版で、
// faces.json の blink に載る表情だけが入る (まばたきできる表情の一覧を兼ねる)。
type faceSet struct {
	images map[string]map[string][]string
	blinks map[string]map[string][]string
}

// blinkable は blinks を {キャラ: {表情: true}} にする (blinkRuns が引く形)。
func (fs faceSet) blinkable() map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for key, faces := range fs.blinks {
		out[key] = map[string]bool{}
		for face := range faces {
			out[key][face] = true
		}
	}
	return out
}

// loadFaces は台本で使う表情の立ち絵 (と閉じ目の版) を data URI で読む。
//
// 音声の圧縮より前に呼び、ファイルの欠けで長い処理を無駄にしない。使わない表情は埋め込まない (HTML が太るため)。
// 閉じ目の版は faces.json の blink に載る表情だけを読む。載っていなければまばたかない (blink の無い古い書き出しもそのまま使える)。
func loadFaces(s *Script, env *Env) (faceSet, error) {
	used := map[string]map[string]bool{}
	for _, k := range castKeys() {
		used[k] = map[string]bool{defaultFace: true}
	}
	for _, line := range s.Lines {
		used[pyStr(line["who"])][pyStr(lineFace(line))] = true
	}
	fs := faceSet{images: map[string]map[string][]string{}, blinks: map[string]map[string][]string{}}
	for _, key := range castKeys() {
		imgs, blinks := map[string][]string{}, map[string][]string{}
		if d, ok := facesDir(s, key, env); ok {
			canBlink, err := blinkFaces(d)
			if err != nil {
				return faceSet{}, fail("cast.%s.faces: %s/faces.json の blink が読めない (%v)", key, d, err)
			}
			for _, face := range sortedKeys(used[key]) {
				if imgs[face], err = loadMouthImages(key, d, face+"_"); err != nil {
					return faceSet{}, err
				}
				if canBlink[face] {
					if blinks[face], err = loadMouthImages(key, d, face+"_blink_"); err != nil {
						return faceSet{}, err
					}
				}
			}
		}
		fs.images[key] = imgs
		if len(blinks) > 0 {
			fs.blinks[key] = blinks
		}
	}
	return fs, nil
}

// blinkFaces は faces.json の blink (閉じ目の版を書き出した表情の一覧。psd_faces.py が書く) を読む。キーが無ければ空。
func blinkFaces(dir string) (map[string]bool, error) {
	meta, err := readFacesMeta(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	v, ok := meta["blink"]
	if !ok {
		return out, nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("表情の名前のリストでない: %s", pyRepr(v))
	}
	for _, f := range list {
		out[pyStr(f)] = true
	}
	return out, nil
}

// loadMouthImages は <prefix>0〜<prefix>2.webp (口の 閉じ / 半開き / 開き) を data URI で読む。欠けていれば全部を挙げて止まる。
func loadMouthImages(key, dir, prefix string) ([]string, error) {
	var files, missing []string
	for lv := range mouthLevels {
		f := filepath.Join(dir, fmt.Sprintf("%s%d.webp", prefix, lv))
		files = append(files, f)
		if st, err := os.Stat(f); err != nil || !st.Mode().IsRegular() {
			missing = append(missing, filepath.Base(f))
		}
	}
	if len(missing) > 0 {
		return nil, fail("cast.%s.faces: %s に %s が無い (psd_faces.py で書き出し直す)", key, dir, strings.Join(missing, ", "))
	}
	uris := make([]string, 0, mouthLevels)
	for _, f := range files {
		u, err := dataURI(f, "image/webp")
		if err != nil {
			return nil, fail("cast.%s.faces: %s が読めない (%v)", key, f, err)
		}
		uris = append(uris, u)
	}
	return uris, nil
}

func faceCredits(s *Script, env *Env) []string {
	var out []string
	for _, key := range castKeys() {
		d, ok := facesDir(s, key, env)
		if !ok {
			continue
		}
		meta, err := readFacesMeta(d)
		if err != nil {
			continue // loadScript (faceList) が faces.json を読めることを確かめ済み
		}
		if c, ok := meta["credit"]; ok && pyTruthy(c) && !slices.Contains(out, pyStr(c)) {
			out = append(out, pyStr(c))
		}
	}
	return out
}

// --- プレイヤーのデータ ---

type chapterData struct {
	Title string  `json:"title"`
	Start float64 `json:"start"`
}

type castData struct {
	Name   string              `json:"name"`
	Color  string              `json:"color"`
	Side   string              `json:"side"`
	Mirror bool                `json:"mirror"`
	Images map[string][]string `json:"images"`
	// まばたき (Python 版の後で足した)。Blink は images と同じ形の閉じ目の版、BlinkBit は PlayerData.Blinks のビットで、
	// 閉じ目の版が 1 つも無いキャラでは両方とも載せない
	Blink    map[string][]string `json:"blink,omitempty"`
	BlinkBit int                 `json:"blinkBit,omitempty"`
}

// PlayerData はプレイヤー (player.html) に埋め込むデータ。HTML の再生と mp4 の撮影が同じものを使う。
type PlayerData struct {
	Title       string              `json:"title"`
	Date        string              `json:"date"`
	Overlays    []string            `json:"overlays"` // 舞台に重ねて出すメタデータ (overlayKeys のうち台本が選んだもの)
	Description string              `json:"description"`
	Credits     []string            `json:"credits"`
	Cast        map[string]castData `json:"cast"`
	DefaultFace string              `json:"defaultFace"`
	Chapters    []chapterData       `json:"chapters"`
	Shows       []showData          `json:"shows,omitempty"`
	Lines       []timelineLine      `json:"lines"`
	Frames      [][4]int            `json:"frames"`
	Blinks      [][2]int            `json:"blinks,omitempty"` // [開始フレーム, 目を閉じているキャラのビット] (blinkRuns)。誰もまばたかなければ載せない
	Duration    float64             `json:"duration"`
	FPS         int                 `json:"fps"`
	Audio       string              `json:"audio"`
}

// createdDate は舞台の右上に出す作成日。台本の date を優先し、無ければ build した日 (手元の時刻) にする。
func createdDate(s *Script, env *Env) string {
	if v, ok := s.Raw["date"]; ok {
		return pyStr(v)
	}
	now := time.Now
	if env.Now != nil {
		now = env.Now
	}
	return now().Format("2006-01-02")
}

// overlayKeys は舞台に重ねて出せるメタデータ: 左上のタイトル・右上の作成日・上部中央のトピック (チャプター) 名。
var overlayKeys = []string{"title", "date", "topic"}

// parseOverlays は台本の overlays (出すものの名前の配列。空なら何も出さない) を読む。
func parseOverlays(v any) ([]string, error) {
	items, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("overlays は %s から出すものを選んだ配列で書く (実際: %s)", strings.Join(overlayKeys, "/"), pyRepr(v))
	}
	out := []string{}
	for _, it := range items {
		k, ok := it.(string)
		if !ok || !slices.Contains(overlayKeys, k) || slices.Contains(out, k) {
			return nil, fmt.Errorf("overlays に書けるのは %s (重複なし) だけ (実際: %s)", strings.Join(overlayKeys, "/"), pyRepr(v))
		}
		out = append(out, k)
	}
	return out, nil
}

// screenOverlays は舞台に出すメタデータ。台本に overlays が無ければ全部出す。
func screenOverlays(s *Script) []string {
	if v, ok := s.Raw["overlays"]; ok {
		if out, err := parseOverlays(v); err == nil {
			return out
		}
	}
	return slices.Clone(overlayKeys)
}

// assemble は合成済みの wav を連結し、プレイヤーに渡すデータ (音声以外) と連結した PCM を返す。
func assemble(s *Script, env *Env) (*PlayerData, []byte, error) {
	wd := workDir(s.Path)
	leadIn := numOr(lineGet(s.Raw, "lead_in", nil), 0.4)
	defaultGap := numOr(lineGet(s.Raw, "gap", nil), 0.35)
	fs, err := loadFaces(s, env)
	if err != nil {
		return nil, nil, err
	}
	faces := lineFaces(s.Lines)
	showIdx, showSteps, shows, err := lineShows(s.Path, s.Lines)
	if err != nil {
		return nil, nil, err
	}
	if err := embedShowImages(s, shows); err != nil {
		return nil, nil, err
	}

	pcm := make([]byte, 2*pyRound(leadIn*sampleRate))
	// 連結後の大きさを wav のファイルの大きさと間の無音から見積もって先に確保する (行ごとに伸ばすと、20 分の台本で
	// 400MB を超える確保になっていた。issue 655)。見積もりは確保の量だけに効き、外れても結果は変わらない
	est := len(pcm)
	for _, line := range s.Lines {
		if p, err := lineParams(s, line); err == nil {
			if w, _ := cachePaths(wd, p); w != "" {
				if st, err := os.Stat(w); err == nil {
					est += int(st.Size())
				}
			}
		}
		est += 2 * pyRound(numOr(lineGet(line, "pause_after", nil), defaultGap)*sampleRate)
	}
	pcm = slices.Grow(pcm, est-len(pcm))
	timeline := []timelineLine{}
	chapters := []chapterData{}
	var missing []int
	for i, line := range s.Lines {
		p, err := lineParams(s, line)
		if err != nil {
			return nil, nil, err
		}
		wavPath, queryPath := cachePaths(wd, p)
		if !isFile(wavPath) || !isFile(queryPath) {
			missing = append(missing, i)
			continue
		}
		frames, dur, err := readWav(wavPath)
		if err != nil {
			return nil, nil, err
		}
		qb, err := os.ReadFile(queryPath)
		if err != nil {
			return nil, nil, fail("%s: 読めない (%v)", queryPath, err)
		}
		var query map[string]any
		if err := decodeJSON(qb, &query); err != nil {
			return nil, nil, fail("%s: 読めない (%v)", queryPath, err)
		}
		start := float64(len(pcm)) / 2 / sampleRate
		pcm = append(pcm, frames...)
		if c, ok := line["chapter"]; ok && pyTruthy(c) {
			chapters = append(chapters, chapterData{Title: pyStr(c), Start: pyRound3(start)})
		}
		timeline = append(timeline, timelineLine{
			Who: pyStr(line["who"]), Text: pyStr(line["text"]), Faces: faces[i],
			// 行が属するチャプターの番号 (最初のチャプターより前の行は -1)。画面の上部中央のトピック名はこれで決める。
			// mp4 は行ごとに絵を撮るので、ここで決めておけば、チャプターが変わる行で HTML と同じく表示が切り替わる
			Chapter: len(chapters) - 1,
			Show:    showIdx[i],
			Step:    showSteps[i],
			Start:   pyRound3(start), End: pyRound3(start + dur),
			mouth: mouthTrack(query, dur),
		})
		pause := numOr(lineGet(line, "pause_after", nil), defaultGap)
		pcm = appendSilence(pcm, 2*pyRound(pause*sampleRate))
	}
	if len(missing) > 0 {
		return nil, nil, fail("合成済みの wav が無い行がある (台本を変えた後に synth していない?): lines %s", pyIntList(missing[:min(10, len(missing))]))
	}

	duration := float64(len(pcm)) / 2 / sampleRate
	var credits []string
	add := func(c string) {
		if !slices.Contains(credits, c) {
			credits = append(credits, c)
		}
	}
	// 声のクレジット (利用規約で必須) と立ち絵のクレジットは自動で入れる。台本の credits はその後に足す
	for _, def := range castOrder {
		if slices.ContainsFunc(s.Lines, func(l map[string]any) bool { return pyStr(l["who"]) == def.Key }) {
			add("VOICEVOX:" + def.Name)
		}
	}
	for _, c := range faceCredits(s, env) {
		add(c)
	}
	creditsRaw := lineGet(s.Raw, "credits", []any{})
	switch creditsRaw.(type) {
	case []any, string, map[string]any: // Python の for c in credits が回せる形 (文字列は 1 文字ずつになるのも Python と同じ)
	default:
		return nil, nil, fail("%s: credits は文字列のリストで書く (実際: %s)", s.Path, pyRepr(creditsRaw))
	}
	for _, c := range pyIter(creditsRaw) {
		add(pyStr(c))
	}
	title := pyStem(filepath.Base(s.Path))
	if v, ok := s.Raw["title"]; ok {
		title = pyStr(v)
	}
	cast := map[string]castData{}
	for _, def := range castOrder {
		c := castData{Name: def.Name, Color: def.Color, Side: def.Side, Mirror: def.Mirror, Images: fs.images[def.Key]}
		if b := fs.blinks[def.Key]; len(b) > 0 {
			c.Blink, c.BlinkBit = b, blinkBit(def.Key)
		}
		cast[def.Key] = c
	}
	frames := frameRuns(timeline, duration)
	data := &PlayerData{
		Title:       title,
		Date:        createdDate(s, env),
		Overlays:    screenOverlays(s),
		Description: pyStr(lineGet(s.Raw, "description", "")),
		Credits:     credits,
		Cast:        cast,
		DefaultFace: defaultFace,
		Chapters:    chapters,
		Shows:       shows,
		Lines:       timeline,
		Frames:      frames,
		Blinks:      blinkRuns(frames, timeline, fs.blinkable(), duration),
		Duration:    pyRound3(duration),
		FPS:         mouthFPS,
		Audio:       "",
	}
	return data, pcm, nil
}

// pyIter は Python で for c in v と回したときの要素 (文字列は 1 文字ずつ)。
func pyIter(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case string:
		out := make([]any, 0, len(x))
		for _, r := range x {
			out = append(out, string(r))
		}
		return out
	case map[string]any:
		out := make([]any, 0, len(x))
		for _, k := range sortedKeys(x) {
			out = append(out, k)
		}
		return out
	default:
		return nil
	}
}

func pyIntList(xs []int) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = fmt.Sprint(x)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

var templateMarks = regexp.MustCompile(`__DATA_JSON__|__TITLE__`)

// renderHTML はデータをプレイヤー (skill の templates/player.html) に差し込む。
//
// テンプレートは go:embed で焼き込まない: bin/lib/go_autobuild.zsh の再ビルドの判定は .go と go.mod / go.sum しか
// 見ないので、焼き込むとテンプレートだけを直したときに古いバイナリが古い見た目で作り続ける (issue 641 の設計レビュー P1)。
func renderHTML(data *PlayerData, env *Env) (string, error) {
	tb, err := os.ReadFile(env.Template())
	if err != nil {
		return "", fail("%s: 読めない (%v)", env.Template(), err)
	}
	playerTemplate := string(tb)
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(data); err != nil {
		return "", err
	}
	// </script> で JSON が途切れないよう "<" をエスケープする (JSON としては同じ値)
	payload := strings.ReplaceAll(strings.TrimSuffix(b.String(), "\n"), "<", "\x5cu003c") // \x5c = バックスラッシュ (JSON の \u003c エスケープを作る)
	title := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(data.Title)
	if strings.Count(playerTemplate, "__DATA_JSON__") != 1 || strings.Count(playerTemplate, "__TITLE__") != 1 {
		return "", fail("%s: 差し込み位置 (__DATA_JSON__ / __TITLE__) がそれぞれ 1 つではない", env.Template())
	}
	// 1 回の走査で両方を差し込む (順に置換すると、先に入れた値の中の印まで置換してしまう)
	return templateMarks.ReplaceAllStringFunc(playerTemplate, func(m string) string {
		if m == "__DATA_JSON__" {
			return payload
		}
		return title
	}), nil
}

// cmdBuild は合成済みの wav を連結し、HTML プレイヤーか mp4 (か両方) を書き出す。
func cmdBuild(env *Env, scriptArg, output, format string, jobs, kbps int, allowFastCaptions bool) error {
	s, err := loadScript(resolvePath(scriptArg), env)
	if err != nil {
		return err
	}
	// filepath.Abs を通さない (論理パスの $PWD で ".." を字面で畳んでしまう。敵対的レビュー 2 周目 P2-3)
	base := resolvePath(output)
	for _, ext := range []string{".html", ".mp4"} { // 付いていれば外す。v1.2 の ".2" のような拡張子でない部分は残す
		if strings.HasSuffix(strings.ToLower(base), ext) {
			base = base[:len(base)-len(ext)]
		}
	}
	formats := []string{format}
	if format == "both" {
		formats = []string{"html", "mp4"}
	}
	// 出力先に書けるかを最初に確かめる (確かめないと、音声の圧縮と Chrome の撮影 (CPU の空き待ちで最大 20 分) を
	// 全部終えてから「一時ファイルを作れない」で止まる。issue 652)
	for _, f := range formats {
		dst := resolvePath(base + "." + f)
		if err := checkReplaceable(dst); err != nil {
			return fail("%s: 書けない (%v)", base+"."+f, err)
		}
		p, err := partPath(dst)
		if err != nil {
			return fail("%s: 書けない (%v)", base+"."+f, err)
		}
		_ = os.Remove(p)
	}
	td, err := os.MkdirTemp("", "zundamon-kaisetsu-")
	if err != nil {
		return fail("一時ディレクトリを作れない (%v)", err)
	}
	defer func() { _ = os.RemoveAll(td) }() // Ctrl-C でも appCtx の取り消しで子が止まってから走る

	data, pcm, err := assemble(s, env)
	if err != nil {
		return err
	}
	// 字幕が速すぎる行があれば、音声の圧縮と書き出し (mp4 の撮影) の前に止める。pause_after は合成の鍵に入らないので、直して build し直すだけで済む
	if n := warnFastCaptions(env.Stderr, data.Lines, data.Duration); n > 0 && !allowFastCaptions {
		return fail("字幕が速すぎる行が %d 行あるので、書き出す前に止めた (行を分けるか pause_after で間を足す。このまま書き出すなら --allow-fast-captions)", n)
	}
	joined := filepath.Join(td, "joined.wav")
	if err := writeWav(joined, pcm); err != nil {
		return fail("%s: 書けない (%v)", joined, err)
	}
	m4a := filepath.Join(td, "audio.m4a")
	if err := encodeAudio(joined, m4a, kbps); err != nil {
		return err
	}
	for _, f := range formats {
		out := base + "." + f
		dst := resolvePath(out) // 出力先が symlink ならリンク先に書く (Python 版の write_text と同じ)
		if f == "html" {
			// mime は容器の型を明示する (推定に任せると audio/mp4a-latm 等になり、ブラウザが再生できない)
			audio, err := dataURI(m4a, "audio/mp4")
			if err != nil {
				return fail("%s: 読めない (%v)", m4a, err)
			}
			d := *data
			d.Audio = audio
			html, err := renderHTML(&d, env)
			if err != nil {
				return err
			}
			// 出力先の隣の一時ファイルに書いてから置き換える (中断で前回の正常な出力を壊さない)
			if err := writeOutput(dst, []byte(html)); err != nil {
				return fail("%s: 書けない (%v)", out, err)
			}
		} else if err := writeMP4(env, data, m4a, dst, td, jobs); err != nil {
			return err
		}
		st, err := os.Stat(dst)
		if err != nil {
			return fail("%s: 書き出されていない (%v)", out, err)
		}
		fmt.Fprintf(env.Stderr, "build: %s / %d 行 / %.1f 秒 / %.1f MB → %s\n", f, len(data.Lines), data.Duration, float64(st.Size())/1e6, out)
	}
	return nil
}
