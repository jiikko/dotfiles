// Package script は台本の読み込みと検証・キャラの設定・合成のキャッシュの鍵 (Params)・読み替え。
package script

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
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"zundamon-kaisetsu/internal/app"
	"zundamon-kaisetsu/internal/paths"
	"zundamon-kaisetsu/internal/pyjson"
	"zundamon-kaisetsu/internal/show"
)

// castDef はキャラクターの固定の設定。名前・既定の声・色・立ち位置の正本はここ。
// mirror は立ち絵を左右反転して表示する。2 人が向き合うよう、素材の向き (めたんは画面の左向き、
// ずんだもんは左寄り向き) と立ち位置から決めている。素材を替えて向きが変わったら見直す
type castDef struct {
	Key, Name, Color, Side string
	StyleID                int64
	Mirror                 bool
}

// CastOrder はキャラクターの並び (Python 版の CAST の順。クレジットの順もこれ)。
var CastOrder = []castDef{
	{Key: "metan", Name: "四国めたん", StyleID: 2, Color: "#d9418c", Side: "left", Mirror: true},
	{Key: "zundamon", Name: "ずんだもん", StyleID: 3, Color: "#2e9e3a", Side: "right", Mirror: false},
}

// castOptions は台本の cast.<キャラ> で変えてよいもの。
var castOptions = []string{"faces", "style_id", "speed", "pitch", "intonation", "volume"}

// FaceVocab は表情の語彙。faces/*.json (psd_faces.py の定義) はキャラごとにこの全部を定義する。
var FaceVocab = []string{"通常", "笑顔", "説明", "驚き", "困り", "考え中", "怒り", "悲しみ"}

const DefaultFace = "通常"

func CastByKey(key string) (castDef, bool) {
	for _, c := range CastOrder {
		if c.Key == key {
			return c, true
		}
	}
	return castDef{}, false
}

func CastKeys() []string {
	keys := make([]string, len(CastOrder))
	for i, c := range CastOrder {
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

	readingsOnce sync.Once      // readings の正規表現は台本ごとに 1 回だけ作る (行ごとに作っていた。issue 655)
	readingsRe   *regexp.Regexp // readings が空なら nil
}

// DecodeJSON は数を json.Number のまま読む (Python の int / float の区別と repr を保つため)。
func DecodeJSON(b []byte, v any) error {
	// 値の後ろの余分なデータ (閉じ括弧を含む) を拒否する。Decoder だけでは閉じ括弧の手前で止まって気づかないので、先に全体を検査する
	if !json.Valid(b) {
		var probe any
		return json.Unmarshal(b, &probe) // Valid が偽なら必ず誤りを返す (その文言を使う)
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	return d.Decode(v)
}

// LoadScript は Python 版の load_script と同じ順で検証する (受け入れ / 拒否と文言を golden で突き合わせる)。
func LoadScript(path string, env *app.Env) (*Script, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, app.Fail("%s: 読めない (%v)", path, err)
	}
	if !utf8.Valid(b) {
		// Go の JSON は不正なバイトを U+FFFD に置き換えて読んでしまう (Python 版は読めずに止まる)
		return nil, app.Fail("%s: 読めない (UTF-8 として読めないバイトがある)", path)
	}
	var raw map[string]any
	if err := DecodeJSON(b, &raw); err != nil {
		return nil, app.Fail("%s: 読めない (%v)", path, err)
	}
	if raw == nil {
		return nil, app.Fail("%s: 読めない (台本の最上位が JSON のオブジェクトでない)", path)
	}
	castRaw := raw["cast"]
	if !pyjson.Truthy(castRaw) {
		castRaw = map[string]any{}
	}
	cast, ok := castRaw.(map[string]any)
	if !ok || !subsetOf(pyjson.SortedKeys(castMapOrEmpty(castRaw)), CastKeys()) {
		return nil, app.Fail("%s: cast に書けるのは %s だけ (キャラクターは固定)", path, strings.Join(CastKeys(), "/"))
	}
	linesRaw, ok := raw["lines"].([]any)
	if !ok || len(linesRaw) == 0 {
		return nil, app.Fail("%s: lines が空", path)
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
			return nil, app.Fail("%s: date は空でない文字列で書く (実際: %s)", path, pyjson.Repr(d))
		}
	}
	// 資料の語り方のモード (SKILL.md の「資料の語り方」)。プレイヤーのデータには載せない (見た目は変わらない。lint が見る)
	if m, ok := raw["source_mode"]; ok && m != "public" && m != "internal" {
		return nil, app.Fail(`%s: source_mode は "public" (独自調査。不特定多数に公開) か "internal" (資料を明かす。社内向け。省略時) (実際: %s)`, path, pyjson.Repr(m))
	}
	if ov, ok := raw["overlays"]; ok {
		if _, err := ParseOverlays(ov); err != nil {
			return nil, app.Fail("%s: %v", path, err)
		}
	}
	if rd, ok := raw["readings"]; ok && !validReadings(rd) {
		return nil, app.Fail(`%s: readings は {"字幕の語": "読ませたい語"} の形 (キーも値も空でない文字列)`, path)
	}
	if ac, ok := raw["acronyms"]; ok && !validAcronyms(ac) {
		return nil, app.Fail(`%s: acronyms は ["QRO", …] の形 (空でない文字列の配列)`, path)
	}
	for _, name := range sortedKeysInCastOrder(cast) {
		c, ok := cast[name].(map[string]any)
		if !ok {
			return nil, app.Fail("%s: cast.%s に書けるのは %s だけ (実際: %s)", path, name, strings.Join(castOptions, "/"), pyjson.Repr(cast[name]))
		}
		var extra []string
		for k := range c {
			if !slices.Contains(castOptions, k) {
				extra = append(extra, k)
			}
		}
		if len(extra) > 0 {
			sort.Strings(extra)
			return nil, app.Fail("%s: cast.%s に書けるのは %s だけ (実際: %s)", path, name, strings.Join(castOptions, "/"), pyjson.Repr(extra))
		}
	}
	s := &Script{Path: path, Raw: raw, Cast: map[string]map[string]any{}}
	for _, def := range CastOrder {
		m := map[string]any{"style_id": json.Number(strconv.FormatInt(def.StyleID, 10))}
		if over, ok := cast[def.Key].(map[string]any); ok {
			for k, v := range over {
				m[k] = v
			}
		}
		s.Cast[def.Key] = m
	}
	faces := map[string][]string{}
	for _, def := range CastOrder {
		list, err := faceList(s, def.Key, env)
		if err != nil {
			return nil, err
		}
		faces[def.Key] = list
	}
	for i, lr := range linesRaw {
		line, ok := lr.(map[string]any)
		if !ok {
			return nil, app.Fail("%s: lines[%d] が JSON のオブジェクトでない", path, i)
		}
		s.Lines = append(s.Lines, line)
		who, _ := line["who"].(string)
		if _, ok := CastByKey(who); !ok {
			return nil, app.Fail("%s: lines[%d].who は %s のどれか (実際: %s)", path, i, strings.Join(CastKeys(), "/"), pyjson.Repr(line["who"]))
		}
		face := LineFace(line)
		fs, _ := face.(string)
		if !slices.Contains(FaceVocab, fs) || !isString(face) {
			return nil, app.Fail("%s: lines[%d].face は %s のどれか (実際: %s)", path, i, strings.Join(FaceVocab, "/"), pyjson.Repr(face))
		}
		if faces[who] != nil && !slices.Contains(faces[who], fs) {
			return nil, app.Fail("%s: lines[%d].face=%s が cast.%s.faces の書き出しに無い (psd_faces.py の定義に足す)", path, i, pyjson.StrRepr(fs), who)
		}
		// text / read は文字列だけを受ける。Python 版は null や数も str() で通し、キャッシュの鍵には文字列でないまま
		// 入れていた (Go 版で同じ鍵を作れない)。黙って別の鍵にするより、ここで止める
		for _, key := range []string{"text", "read"} {
			if v, ok := line[key]; ok && !isString(v) {
				return nil, app.Fail("%s: lines[%d].%s は文字列で書く (実際: %s)", path, i, key, pyjson.Repr(v))
			}
		}
		if pyjson.Strip(pyjson.Str(LineGet(line, "text", ""))) == "" {
			return nil, app.Fail("%s: lines[%d].text が空", path, i)
		}
		if r, ok := line["read"]; ok && pyjson.Strip(pyjson.Str(r)) == "" {
			return nil, app.Fail("%s: lines[%d].read が空 (読みを直さないなら read を書かない)", path, i)
		}
		if v, ok := line["pause_after"]; ok {
			if err := nonneg(path, fmt.Sprintf("lines[%d].pause_after", i), v); err != nil {
				return nil, err
			}
		}
		p, err := LineParams(s, line)
		if err != nil {
			return nil, app.Fail("%s: lines[%d] の数値が不正 (%v)", path, i, err)
		}
		if p.Speed <= 0 {
			return nil, app.Fail("%s: lines[%d] の speed は正の数", path, i)
		}
	}
	// 図解の中身は形だけを見る (画像を足すときも、ファイルの有無は build.Assemble で見る。synth / kana を止めないため)
	if _, _, _, err := show.LineShows(path, s.Lines); err != nil {
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
// どのキーも CastKeys に含まれることは検査済みなので、並びが変わってもどのキャラのエラーを先に出すかだけの差。
func sortedKeysInCastOrder(cast map[string]any) []string {
	var out []string
	for _, k := range CastKeys() {
		if _, ok := cast[k]; ok {
			out = append(out, k)
		}
	}
	return out
}

func isString(v any) bool { _, ok := v.(string); return ok }

func LineGet(line map[string]any, key string, def any) any {
	if v, ok := line[key]; ok {
		return v
	}
	return def
}

func LineFace(line map[string]any) any { return LineGet(line, "face", DefaultFace) }

func validReadings(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	for k, val := range m {
		s, ok := val.(string)
		if k == "" || !ok || pyjson.Strip(s) == "" {
			return false
		}
	}
	return true
}

func validAcronyms(v any) bool {
	list, ok := v.([]any)
	if !ok {
		return false
	}
	for _, x := range list {
		if s, ok := x.(string); !ok || pyjson.Strip(s) == "" {
			return false
		}
	}
	return true
}

// Acronyms は台本の acronyms (1 文字ずつ読むのが正しい英字の語。kana --check が警告しない)。skill の acronyms.json に置けない
// 語 (社内の略語) を台本ごとに足すためのもの。形は LoadScript が検査済み。
func Acronyms(s *Script) []string {
	var out []string
	for _, x := range pyjson.Iter(LineGet(s.Raw, "acronyms", []any{})) {
		out = append(out, pyjson.Str(x))
	}
	return out
}

// nonneg は Python 版の nonneg (int か float で、bool でなく、0 以上)。
func nonneg(path, key string, v any) error {
	if n, ok := v.(json.Number); ok {
		if f, err := pyjson.Float(n); err == nil && f >= 0 {
			return nil
		}
	}
	return app.Fail("%s: %s は 0 以上の数 (実際: %s)", path, key, pyjson.Repr(v))
}

// FacesDir は立ち絵の dir。台本の cast.<キャラ>.faces が無ければ、skill の assets/ にあれば使う。
func FacesDir(s *Script, key string, env *app.Env) (string, bool) {
	c := s.Cast[key]
	if v, ok := c["faces"]; ok && pyjson.Truthy(v) {
		return paths.ResolveScriptRelative(s.Path, pyjson.Str(v)), true
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

// ReadFacesMeta は立ち絵の dir の faces.json (psd_faces.py の出力: faces / credit) を読む。
func ReadFacesMeta(dir string) (map[string]any, error) {
	b, err := os.ReadFile(filepath.Join(dir, "faces.json"))
	if err != nil {
		return nil, err
	}
	var meta map[string]any
	if err := DecodeJSON(b, &meta); err != nil {
		return nil, err
	}
	return meta, nil
}

// faceList は cast.<キャラ>.faces (psd_faces.py の出力 dir) にある表情の一覧。dir が無ければ nil (丸アバターで出す)。
func faceList(s *Script, key string, env *app.Env) ([]string, error) {
	d, ok := FacesDir(s, key, env)
	if !ok {
		return nil, nil
	}
	fj := filepath.Join(d, "faces.json")
	meta, err := ReadFacesMeta(d)
	if err == nil {
		list, ok := meta["faces"].([]any)
		if !ok {
			err = errors.New("'faces'")
		} else {
			out := make([]string, 0, len(list))
			for _, f := range list {
				out = append(out, pyjson.Str(f))
			}
			return out, nil
		}
	}
	return nil, app.Fail("cast.%s.faces: %s が読めない (%v)。psd_faces.py で書き出した dir を指定する", key, fj, err)
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

// LineParams は Python 版の line_params (行 → cast → 台本全体 → 既定 の順に引く)。
func LineParams(s *Script, line map[string]any) (Params, error) {
	cast := s.Cast[pyjson.Str(line["who"])]
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
	p.Text = SpokenText(s, line)
	if p.StyleID, err = pyjson.Int(get("style_id", nil)); err != nil {
		return p, err
	}
	speedDef := any(json.Number("1.0"))
	if v, ok := s.Raw["speed"]; ok {
		speedDef = v
	}
	if p.Speed, err = pyjson.Float(get("speed", speedDef)); err != nil {
		return p, err
	}
	if p.Pitch, err = pyjson.Float(get("pitch", json.Number("0.0"))); err != nil {
		return p, err
	}
	if p.Intonation, err = pyjson.Float(get("intonation", json.Number("1.0"))); err != nil {
		return p, err
	}
	if p.Volume, err = pyjson.Float(get("volume", json.Number("1.0"))); err != nil {
		return p, err
	}
	return p, nil
}

// SpokenText は音声にする文。字幕は text のまま。行の read があればそれを使い、無ければ text に台本全体の readings を当てる。
//
// readings は 1 回の走査で置き換える。同じ位置では長いキーを優先し (「Hash」と「HashMap」の両方があれば HashMap)、
// 置き換えた結果をもう一度置き換えない (語ごとに replace を重ねると、結果に別のキーが含まれたとき二重に変わる)。
// Go の regexp の選択は Python と同じく左優先 (leftmost-first) なので、長い順に並べれば同じ結果になる。
func SpokenText(s *Script, line map[string]any) string {
	if r, ok := line["read"]; ok {
		return pyjson.Str(r)
	}
	text := pyjson.Str(LineGet(line, "text", ""))
	rd, _ := s.Raw["readings"].(map[string]any)
	if len(rd) == 0 {
		return text
	}
	// 🚨 台本の Raw["readings"] を読み込んだ後に書き換える呼び出し元は無い前提 (書き換えるなら readingsRe も作り直す)
	s.readingsOnce.Do(func() {
		keys := pyjson.SortedKeys(rd)
		sort.SliceStable(keys, func(i, j int) bool { return len([]rune(keys[i])) > len([]rune(keys[j])) })
		quoted := make([]string, len(keys))
		for i, k := range keys {
			quoted[i] = regexp.QuoteMeta(k)
		}
		s.readingsRe = regexp.MustCompile(strings.Join(quoted, "|"))
	})
	return s.readingsRe.ReplaceAllStringFunc(text, func(m string) string { return pyjson.Str(rd[m]) })
}

// CacheSerialize は Python 版の json.dumps(params, sort_keys=True, ensure_ascii=False)。
func CacheSerialize(p Params) string {
	return fmt.Sprintf(`{"intonation": %s, "pitch": %s, "speed": %s, "style_id": %d, "text": %s, "volume": %s}`,
		pyjson.FloatRepr(p.Intonation), pyjson.FloatRepr(p.Pitch), pyjson.FloatRepr(p.Speed), p.StyleID, pyjson.JSONString(p.Text), pyjson.FloatRepr(p.Volume))
}

// CacheKey は合成のキャッシュの鍵。鍵は合成入力だけで決める (行番号を含めない)。行を挿入・削除しても、
// 他の行は合成し直さずに済む。エンジンの版・ユーザー辞書は鍵に入らない (synth --force で作り直す)。
func CacheKey(p Params) string {
	sum := sha256.Sum256([]byte(CacheSerialize(p)))
	return hex.EncodeToString(sum[:])[:16]
}

func CachePaths(wd string, p Params) (wav, query string) {
	k := CacheKey(p)
	return filepath.Join(wd, k+".wav"), filepath.Join(wd, k+".query.json")
}

// DisplayTitle は舞台の左上と HTML の見出しに出るタイトル (title を省くと台本のファイル名)。
func DisplayTitle(s *Script) string {
	if v, ok := s.Raw["title"]; ok {
		return pyjson.Str(v)
	}
	return pyjson.Stem(filepath.Base(s.Path))
}

// OverlayKeys は舞台に重ねて出せるメタデータ: 左上のタイトル・右上の作成日・上部中央のトピック (チャプター) 名。
var OverlayKeys = []string{"title", "date", "topic"}

// ParseOverlays は台本の overlays (出すものの名前の配列。空なら何も出さない) を読む。
func ParseOverlays(v any) ([]string, error) {
	items, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("overlays は %s から出すものを選んだ配列で書く (実際: %s)", strings.Join(OverlayKeys, "/"), pyjson.Repr(v))
	}
	out := []string{}
	for _, it := range items {
		k, ok := it.(string)
		if !ok || !slices.Contains(OverlayKeys, k) || slices.Contains(out, k) {
			return nil, fmt.Errorf("overlays に書けるのは %s (重複なし) だけ (実際: %s)", strings.Join(OverlayKeys, "/"), pyjson.Repr(v))
		}
		out = append(out, k)
	}
	return out, nil
}
