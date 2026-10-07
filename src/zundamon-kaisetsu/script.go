package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

// castDef はキャラクターの固定の設定。名前・既定の声・色・立ち位置の正本はここ。
// mirror は立ち絵を左右反転して表示する。2 人が向き合うよう、素材の向き (めたんは画面の左向き、
// ずんだもんは左寄り向き) と立ち位置から決めている。素材を替えて向きが変わったら見直す
type castDef struct {
	Key, Name, Color, Side string
	StyleID                int64
	Mirror                 bool
}

// castOrder はキャラクターの並び (Python 版の CAST の順。クレジットの順もこれ)。
var castOrder = []castDef{
	{Key: "metan", Name: "四国めたん", StyleID: 2, Color: "#d9418c", Side: "left", Mirror: true},
	{Key: "zundamon", Name: "ずんだもん", StyleID: 3, Color: "#2e9e3a", Side: "right", Mirror: false},
}

// castOptions は台本の cast.<キャラ> で変えてよいもの。
var castOptions = []string{"faces", "style_id", "speed", "pitch", "intonation", "volume"}

// faceVocab は表情の語彙。faces/*.json (psd_faces.py の定義) はキャラごとにこの全部を定義する。
var faceVocab = []string{"通常", "笑顔", "説明", "驚き", "困り", "考え中", "怒り", "悲しみ"}

const defaultFace = "通常"

func castByKey(key string) (castDef, bool) {
	for _, c := range castOrder {
		if c.Key == key {
			return c, true
		}
	}
	return castDef{}, false
}

func castKeys() []string {
	keys := make([]string, len(castOrder))
	for i, c := range castOrder {
		keys[i] = c.Key
	}
	return keys
}

// Script は読み込んで検証した台本。値は JSON から読んだまま (any) で持ち、Python 版の dict.get と同じ順に引く。
type Script struct {
	Path  string                    // 実パス (symlink を解決した絶対パス)
	Raw   map[string]any            // 台本全体
	Cast  map[string]map[string]any // キャラごとに、固定の設定へ台本の上書きを重ねたもの (style_id は既定を入れる)
	Lines []map[string]any
}

// errExit は利用者に見せる失敗 (main が "error: " を付けて rc=1 で終える)。
type errExit struct{ msg string }

func (e *errExit) Error() string { return e.msg }

func fail(format string, a ...any) error { return &errExit{fmt.Sprintf(format, a...)} }

// decodeJSON は数を json.Number のまま読む (Python の int / float の区別と repr を保つため)。
func decodeJSON(b []byte, v any) error {
	// 値の後ろの余分なデータ (閉じ括弧を含む) を拒否する。Decoder だけでは閉じ括弧の手前で止まって気づかないので、先に全体を検査する
	if !json.Valid(b) {
		var probe any
		return json.Unmarshal(b, &probe) // Valid が偽なら必ず誤りを返す (その文言を使う)
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	return d.Decode(v)
}

// loadScript は Python 版の load_script と同じ順で検証する (受け入れ / 拒否と文言を golden で突き合わせる)。
func loadScript(path string, env *Env) (*Script, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fail("%s: 読めない (%v)", path, err)
	}
	if !utf8.Valid(b) {
		// Go の JSON は不正なバイトを U+FFFD に置き換えて読んでしまう (Python 版は読めずに止まる)
		return nil, fail("%s: 読めない (UTF-8 として読めないバイトがある)", path)
	}
	var raw map[string]any
	if err := decodeJSON(b, &raw); err != nil {
		return nil, fail("%s: 読めない (%v)", path, err)
	}
	if raw == nil {
		return nil, fail("%s: 読めない (台本の最上位が JSON のオブジェクトでない)", path)
	}
	castRaw := raw["cast"]
	if !pyTruthy(castRaw) {
		castRaw = map[string]any{}
	}
	cast, ok := castRaw.(map[string]any)
	if !ok || !subsetOf(sortedKeys(castMapOrEmpty(castRaw)), castKeys()) {
		return nil, fail("%s: cast に書けるのは %s だけ (キャラクターは固定)", path, strings.Join(castKeys(), "/"))
	}
	linesRaw, ok := raw["lines"].([]any)
	if !ok || len(linesRaw) == 0 {
		return nil, fail("%s: lines が空", path)
	}
	for _, key := range []string{"lead_in", "gap"} {
		if v, ok := raw[key]; ok {
			if err := nonneg(path, key, v); err != nil {
				return nil, err
			}
		}
	}
	if d, ok := raw["date"]; ok {
		if ds, isStr := d.(string); !isStr || strings.TrimSpace(ds) == "" {
			return nil, fail("%s: date は空でない文字列で書く (実際: %s)", path, pyRepr(d))
		}
	}
	if rd, ok := raw["readings"]; ok && !validReadings(rd) {
		return nil, fail(`%s: readings は {"字幕の語": "読ませたい語"} の形 (キーも値も空でない文字列)`, path)
	}
	for _, name := range sortedKeysInCastOrder(cast) {
		c, ok := cast[name].(map[string]any)
		if !ok {
			return nil, fail("%s: cast.%s に書けるのは %s だけ (実際: %s)", path, name, strings.Join(castOptions, "/"), pyRepr(cast[name]))
		}
		var extra []string
		for k := range c {
			if !slices.Contains(castOptions, k) {
				extra = append(extra, k)
			}
		}
		if len(extra) > 0 {
			sort.Strings(extra)
			return nil, fail("%s: cast.%s に書けるのは %s だけ (実際: %s)", path, name, strings.Join(castOptions, "/"), pyRepr(extra))
		}
	}
	s := &Script{Path: path, Raw: raw, Cast: map[string]map[string]any{}}
	for _, def := range castOrder {
		m := map[string]any{"style_id": json.Number(fmt.Sprint(def.StyleID))}
		if over, ok := cast[def.Key].(map[string]any); ok {
			for k, v := range over {
				m[k] = v
			}
		}
		s.Cast[def.Key] = m
	}
	faces := map[string][]string{}
	for _, def := range castOrder {
		list, err := faceList(s, def.Key, env)
		if err != nil {
			return nil, err
		}
		faces[def.Key] = list
	}
	for i, lr := range linesRaw {
		line, ok := lr.(map[string]any)
		if !ok {
			return nil, fail("%s: lines[%d] が JSON のオブジェクトでない", path, i)
		}
		s.Lines = append(s.Lines, line)
		who, _ := line["who"].(string)
		if _, ok := castByKey(who); !ok {
			return nil, fail("%s: lines[%d].who は %s のどれか (実際: %s)", path, i, strings.Join(castKeys(), "/"), pyRepr(line["who"]))
		}
		face := lineFace(line)
		fs, _ := face.(string)
		if !slices.Contains(faceVocab, fs) || !isString(face) {
			return nil, fail("%s: lines[%d].face は %s のどれか (実際: %s)", path, i, strings.Join(faceVocab, "/"), pyRepr(face))
		}
		if faces[who] != nil && !slices.Contains(faces[who], fs) {
			return nil, fail("%s: lines[%d].face=%s が cast.%s.faces の書き出しに無い (psd_faces.py の定義に足す)", path, i, pyStrRepr(fs), who)
		}
		// text / read は文字列だけを受ける。Python 版は null や数も str() で通し、キャッシュの鍵には文字列でないまま
		// 入れていた (Go 版で同じ鍵を作れない)。黙って別の鍵にするより、ここで止める (issue 641 の設計レビュー P2-5)
		for _, key := range []string{"text", "read"} {
			if v, ok := line[key]; ok && !isString(v) {
				return nil, fail("%s: lines[%d].%s は文字列で書く (実際: %s)", path, i, key, pyRepr(v))
			}
		}
		if pyStrip(pyStr(lineGet(line, "text", ""))) == "" {
			return nil, fail("%s: lines[%d].text が空", path, i)
		}
		if r, ok := line["read"]; ok && pyStrip(pyStr(r)) == "" {
			return nil, fail("%s: lines[%d].read が空 (読みを直さないなら read を書かない)", path, i)
		}
		if v, ok := line["pause_after"]; ok {
			if err := nonneg(path, fmt.Sprintf("lines[%d].pause_after", i), v); err != nil {
				return nil, err
			}
		}
		p, err := lineParams(s, line)
		if err != nil {
			return nil, fail("%s: lines[%d] の数値が不正 (%v)", path, i, err)
		}
		if p.Speed <= 0 {
			return nil, fail("%s: lines[%d] の speed は正の数", path, i)
		}
	}
	// 図解の中身は形だけを見る (画像を足すときも、ファイルの有無は assemble で見る。synth / kana を止めないため)
	if _, _, err := lineShows(path, s.Lines); err != nil {
		return nil, err
	}
	return s, nil
}

func castMapOrEmpty(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func subsetOf(keys, allowed []string) bool {
	for _, k := range keys {
		if !slices.Contains(allowed, k) {
			return false
		}
	}
	return true
}

// sortedKeysInCastOrder は cast のキーを Python の dict の順 (= 台本に書いた順) の代わりに、キャラの並び順で返す。
// どのキーも castKeys に含まれることは検査済みなので、並びが変わってもどのキャラのエラーを先に出すかだけの差。
func sortedKeysInCastOrder(cast map[string]any) []string {
	var out []string
	for _, k := range castKeys() {
		if _, ok := cast[k]; ok {
			out = append(out, k)
		}
	}
	return out
}

func isString(v any) bool { _, ok := v.(string); return ok }

func lineGet(line map[string]any, key string, def any) any {
	if v, ok := line[key]; ok {
		return v
	}
	return def
}

func lineFace(line map[string]any) any { return lineGet(line, "face", defaultFace) }

func validReadings(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	for k, val := range m {
		s, ok := val.(string)
		if k == "" || !ok || pyStrip(s) == "" {
			return false
		}
	}
	return true
}

// nonneg は Python 版の nonneg (int か float で、bool でなく、0 以上)。
func nonneg(path, key string, v any) error {
	if n, ok := v.(json.Number); ok {
		if f, err := pyFloat(n); err == nil && f >= 0 {
			return nil
		}
	}
	return fail("%s: %s は 0 以上の数 (実際: %s)", path, key, pyRepr(v))
}

// resolveScriptRelative は台本に書いたパス (~ と相対パス可) を、台本の場所から解いた実際のパスにする。
// 立ち絵の cast.*.faces と図解の image.src が同じ規則で解く
func resolveScriptRelative(scriptPath, p string) string {
	p = expandUser(p)
	if !filepath.IsAbs(p) {
		p = filepath.Join(filepath.Dir(scriptPath), p)
	}
	return resolvePath(p)
}

// facesDir は立ち絵の dir。台本の cast.<キャラ>.faces が無ければ、skill の assets/ にあれば使う。
func facesDir(s *Script, key string, env *Env) (string, bool) {
	c := s.Cast[key]
	if v, ok := c["faces"]; ok && pyTruthy(v) {
		return resolveScriptRelative(s.Path, pyStr(v)), true
	}
	if env.AssetsFaces == "" {
		return "", false
	}
	d := filepath.Join(env.AssetsFaces, key)
	if st, err := os.Stat(filepath.Join(d, "faces.json")); err == nil && st.Mode().IsRegular() {
		return d, true
	}
	return "", false
}

// readFacesMeta は立ち絵の dir の faces.json (psd_faces.py の出力: faces / credit) を読む。
func readFacesMeta(dir string) (map[string]any, error) {
	b, err := os.ReadFile(filepath.Join(dir, "faces.json"))
	if err != nil {
		return nil, err
	}
	var meta map[string]any
	if err := decodeJSON(b, &meta); err != nil {
		return nil, err
	}
	return meta, nil
}

// faceList は cast.<キャラ>.faces (psd_faces.py の出力 dir) にある表情の一覧。dir が無ければ nil (丸アバターで出す)。
func faceList(s *Script, key string, env *Env) ([]string, error) {
	d, ok := facesDir(s, key, env)
	if !ok {
		return nil, nil
	}
	fj := filepath.Join(d, "faces.json")
	meta, err := readFacesMeta(d)
	if err == nil {
		list, ok := meta["faces"].([]any)
		if !ok {
			err = errors.New("'faces'")
		} else {
			out := make([]string, 0, len(list))
			for _, f := range list {
				out = append(out, pyStr(f))
			}
			return out, nil
		}
	}
	return nil, fail("cast.%s.faces: %s が読めない (%v)。psd_faces.py で書き出した dir を指定する", key, fj, err)
}

// Params は合成結果を決める入力のすべて。キャッシュの鍵もここから作るので、合成に効く値を足したらここに足す。
type Params struct {
	Text       string
	StyleID    int64
	Speed      float64
	Pitch      float64
	Intonation float64
	Volume     float64
}

// lineParams は Python 版の line_params (行 → cast → 台本全体 → 既定 の順に引く)。
func lineParams(s *Script, line map[string]any) (Params, error) {
	cast := s.Cast[pyStr(line["who"])]
	get := func(key string, def any) any {
		if v, ok := line[key]; ok {
			return v
		}
		if v, ok := cast[key]; ok {
			return v
		}
		return def
	}
	var p Params
	var err error
	p.Text = spokenText(s, line)
	if p.StyleID, err = pyInt(get("style_id", nil)); err != nil {
		return p, err
	}
	speedDef := any(json.Number("1.0"))
	if v, ok := s.Raw["speed"]; ok {
		speedDef = v
	}
	if p.Speed, err = pyFloat(get("speed", speedDef)); err != nil {
		return p, err
	}
	if p.Pitch, err = pyFloat(get("pitch", json.Number("0.0"))); err != nil {
		return p, err
	}
	if p.Intonation, err = pyFloat(get("intonation", json.Number("1.0"))); err != nil {
		return p, err
	}
	if p.Volume, err = pyFloat(get("volume", json.Number("1.0"))); err != nil {
		return p, err
	}
	return p, nil
}

// spokenText は音声にする文。字幕は text のまま。行の read があればそれを使い、無ければ text に台本全体の readings を当てる。
//
// readings は 1 回の走査で置き換える。同じ位置では長いキーを優先し (「Hash」と「HashMap」の両方があれば HashMap)、
// 置き換えた結果をもう一度置き換えない (語ごとに replace を重ねると、結果に別のキーが含まれたとき二重に変わる)。
// Go の regexp の選択は Python と同じく左優先 (leftmost-first) なので、長い順に並べれば同じ結果になる。
func spokenText(s *Script, line map[string]any) string {
	if r, ok := line["read"]; ok {
		return pyStr(r)
	}
	text := pyStr(lineGet(line, "text", ""))
	rd, _ := s.Raw["readings"].(map[string]any)
	if len(rd) == 0 {
		return text
	}
	keys := sortedKeys(rd)
	sort.SliceStable(keys, func(i, j int) bool { return len([]rune(keys[i])) > len([]rune(keys[j])) })
	quoted := make([]string, len(keys))
	for i, k := range keys {
		quoted[i] = regexp.QuoteMeta(k)
	}
	re := regexp.MustCompile(strings.Join(quoted, "|"))
	return re.ReplaceAllStringFunc(text, func(m string) string { return pyStr(rd[m]) })
}

// cacheSerialize は Python 版の json.dumps(params, sort_keys=True, ensure_ascii=False)。
func cacheSerialize(p Params) string {
	return fmt.Sprintf(`{"intonation": %s, "pitch": %s, "speed": %s, "style_id": %d, "text": %s, "volume": %s}`,
		pyFloatRepr(p.Intonation), pyFloatRepr(p.Pitch), pyFloatRepr(p.Speed), p.StyleID, pyJSONString(p.Text), pyFloatRepr(p.Volume))
}

// cacheKey は合成のキャッシュの鍵。鍵は合成入力だけで決める (行番号を含めない)。行を挿入・削除しても、
// 他の行は合成し直さずに済む。エンジンの版・ユーザー辞書は鍵に入らない (synth --force で作り直す)。
func cacheKey(p Params) string {
	sum := sha256.Sum256([]byte(cacheSerialize(p)))
	return hex.EncodeToString(sum[:])[:16]
}

// workDir は <台本名>.work/ (Python の Path.stem と同じく、最後の拡張子だけを外す)。
func workDir(scriptPath string) string {
	return filepath.Join(filepath.Dir(scriptPath), pyStem(filepath.Base(scriptPath))+".work")
}

func cachePaths(wd string, p Params) (wav, query string) {
	k := cacheKey(p)
	return filepath.Join(wd, k+".wav"), filepath.Join(wd, k+".query.json")
}

// pyStem は Python 3.14 の PurePath.stem (最後の . より前。ただしそれが . だけか空なら名前全体)。
// 例: "foo." → "foo"、"..json" → "..json"、".json" → ".json"。golden は testdata/stem.json
func pyStem(name string) string {
	i := strings.LastIndex(name, ".")
	if i == -1 {
		return name
	}
	if stem := name[:i]; strings.TrimLeft(stem, ".") != "" {
		return stem
	}
	return name
}

func expandUser(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// resolvePath は Python の Path.resolve() (strict でない)。相対パスは物理的な作業ディレクトリ (symlink を解決したもの) から
// たどり、要素を 1 つずつ進めて、symlink ならリンク先へ置き換える。".." は解決した後の親へ戻る (先に字面で畳むと、
// ".." の直前が symlink のとき別のファイルを指す。敵対的レビュー P2-3)。リンク先が存在しなくても (dangling) リンク先へ
// 進む。存在しない要素から先は字面のまま足す。手順は Python の posixpath.realpath と同じで、解決中・解決済みのリンクを
// seen に記録する: 解決中のリンクに戻ってきたらループとみなしてリンクのまま進め、解決済みのリンクは結果を使い回す
// (深さだけで打ち切ると、ループと長い連鎖で結果が Python と違い、a -> a/../a/../a で指数的に時間を食う。3 周目 P3)。
func resolvePath(p string) string {
	seen := map[string]*string{}
	start := "/"
	if !filepath.IsAbs(p) {
		wd, err := os.Getwd()
		if err != nil {
			return p
		}
		start = resolveFrom("/", wd, seen)
	}
	return resolveFrom(start, p, seen)
}

// resolveFrom は解決済みの dir cur から p の要素をたどる。seen の値が nil のリンクは解決中。
func resolveFrom(cur, p string, seen map[string]*string) string {
	if filepath.IsAbs(p) {
		cur = "/"
	}
	for _, comp := range strings.Split(p, "/") {
		switch comp {
		case "", ".":
			continue
		case "..":
			cur = filepath.Dir(cur)
			continue
		}
		next := filepath.Join(cur, comp)
		if r, ok := seen[next]; ok {
			if r != nil {
				cur = *r // 解決済み
			} else {
				cur = next // 解決中のリンクに戻ってきた (ループ)。Python の strict でない realpath と同じくリンクのまま進む
			}
			continue
		}
		fi, err := os.Lstat(next)
		if err != nil || fi.Mode()&os.ModeSymlink == 0 {
			cur = next // 通常のファイル・dir か、存在しない (字面のまま進む)
			continue
		}
		target, err := os.Readlink(next)
		if err != nil {
			cur = next
			continue
		}
		seen[next] = nil
		cur = resolveFrom(cur, target, seen) // 相対のリンク先はリンクのある dir から
		resolved := cur
		seen[next] = &resolved
	}
	return cur
}
