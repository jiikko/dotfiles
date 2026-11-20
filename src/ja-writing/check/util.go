package check

import (
	"crypto/sha256"
	"fmt"
	"math"
	"math/big"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"ja-writing/internal/pycli"
	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
	"ja-writing/visibility"
)

// 外から渡す実行 ID の形。re.match の $ は末尾の改行の手前にも当たるので、末尾に \n を持つ実行 ID も形の検査を通る (本家と同じ)。
var (
	runIDPattern           = pyre.NewLazy(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`, 0)
	windowsReservedPattern = pyre.NewLazy(`^(?:con|prn|aux|nul|com[1-9]|lpt[1-9])$`, pyre.IGNORECASE)
	sincePattern           = pyre.NewLazy(`^([0-9]{4})-([0-9]{2})-([0-9]{2})[Tt ]([0-9]{2}):([0-9]{2}):([0-9]{2})(?:[.,][0-9]+)?([Zz]|[+-][0-9]{2}:?[0-9]{2})$`, 0)
	// isoPattern は datetime.strptime(text, "%Y-%m-%dT%H:%M:%SZ") の正規表現 (CPython の _strptime が組む形。IGNORECASE)。
	// \d が全角などの数字に当たり、日と時が空白 1 字から始まる形も受ける。
	isoPattern = pyre.NewLazy(`(?P<Y>\d\d\d\d)-(?P<m>1[0-2]|0[1-9]|[1-9])-(?P<d>3[0-1]|[1-2]\d|0[1-9]|[1-9]| [1-9])T`+
		`(?P<H>2[0-3]|[0-1]\d|\d| \d):(?P<M>[0-5]\d|\d):(?P<S>6[0-1]|[0-5]\d|\d)Z`, pyre.IGNORECASE)
)

// SHA256 は _sha256 (16 進の小文字)。
func SHA256(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

// TextSHA256 は _text_sha256。記録の text_sha256 で、decode_source のあとの文字列から末尾の改行を全部外して改行を 1 つ付けた (空なら空の) UTF-8 の SHA-256。
// text は WTF-8 の文字列 (Python の encode("utf-8", "surrogatepass") のバイト列と同じ)。
func TextSHA256(text string) string {
	body := strings.TrimRight(text, "\n")
	if body != "" {
		body += "\n"
	}
	return SHA256([]byte(body))
}

// ISO は _iso (UTC の時刻の %Y-%m-%dT%H:%M:%SZ)。
func ISO(moment time.Time) string {
	return moment.Format("2006-01-02T15:04:05Z")
}

// Stamp は _stamp (ファイル名に使う時刻。コロンを含まない)。
func Stamp(moment time.Time) string {
	return moment.Format("20060102T150405Z")
}

// utcMoment は Python の datetime(year, month, day, hour, minute, second) が受ける値だけ time.Time にする。年は 1〜9999、秒は 0〜59。
func utcMoment(year, month, day, hour, minute, second int, offsetMinutes int) (time.Time, bool) {
	if year < 1 || year > 9999 || month < 1 || month > 12 || day < 1 || hour > 23 || minute > 59 || second > 59 {
		return time.Time{}, false
	}
	if day > time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Day() {
		return time.Time{}, false
	}
	moment := time.Date(year, time.Month(month), day, hour, minute, second, 0, time.UTC).Add(-time.Duration(offsetMinutes) * time.Minute)
	// astimezone(utc) が datetime の範囲 (0001-01-01〜9999-12-31) を出ると OverflowError
	if moment.Year() < 1 || moment.Year() > 9999 {
		return time.Time{}, false
	}
	return moment, true
}

// ParseISO は _parse_iso (形は %Y-%m-%dT%H:%M:%SZ。UTC の時刻)。形か値が合わなければ ok が偽。
func ParseISO(text string) (time.Time, bool) {
	match, err := isoPattern.Get().Match(pyre.NewText(text), 0, pyre.NoEnd)
	if err != nil || match == nil || match.End(0) != pystr.Len(text) {
		return time.Time{}, false
	}
	var parts [6]int
	index := isoPattern.Get().GroupIndex()
	for i, name := range []string{"Y", "m", "d", "H", "M", "S"} {
		raw, _ := match.Group(index[name])
		n, ok := pystr.DecimalInt(strings.TrimLeft(raw, " "))
		if !ok {
			return time.Time{}, false
		}
		parts[i] = n
	}
	return utcMoment(parts[0], parts[1], parts[2], parts[3], parts[4], parts[5], 0)
}

func shown(text string) string {
	if pystr.Strip(text) == "" {
		return emptyShown
	}
	return text
}

// RunIDProblem は _run_id_problem。外から渡す実行 ID の誤りの文 (誤りがなければ空)。実行 ID の形に加えて、Windows で作れないか別の名前になるもの
// (末尾の「.」・CON・NUL・COM1 などの予約された名前。拡張子を付けても同じ) を断る。
func RunIDProblem(runID string) string {
	head, _, _ := strings.Cut(runID, ".")
	if !runIDPattern.MustMatchAt0(runID) || strings.HasSuffix(runID, ".") || windowsReservedPattern.MustMatchAt0(head) {
		return fmt.Sprintf(runIDError, shown(runID))
	}
	return ""
}

// RunIDCaseProblem は _run_id_case_problem。state/runs/ に、大文字と小文字だけが違う実行 ID のフォルダがあるときの誤りの文 (なければ空)。
// 大文字と小文字を区別しないファイルシステムでは同じフォルダに入ってしまう。
func RunIDCaseProblem(state, runID string) string {
	entries, err := os.ReadDir(visibility.PathJoin(state, "runs"))
	if err != nil {
		return ""
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	sort.Strings(names)
	want := pyunicode.Casefold(runID)
	for _, name := range names {
		if name != runID && pyunicode.Casefold(name) == want {
			return fmt.Sprintf(runIDCaseError, runID, name)
		}
	}
	return ""
}

// RecordDigest は _record_digest。記録の record_digest で、record_digest を除いてキーを並べ替え、区切りを詰めた JSON の SHA-256 (先頭に sha256:)。
func RecordDigest(record *pyjson.Object) (string, error) {
	body := pyjson.NewObject()
	for _, key := range record.Keys() {
		if key != "record_digest" {
			v, _ := record.Get(key)
			body.Set(key, v)
		}
	}
	text, err := pyjson.Dumps(body, pyjson.Compact())
	if err != nil {
		return "", err
	}
	return "sha256:" + SHA256([]byte(text)), nil
}

// Status は _status_line の引数。値が nil のものは行に出さない (Expected 以降)。RunID・Final・Attempt が空と Attempt が "0" のときは「-」を出す。
// Attempt は回の番号の十進の字 (本家は int で、int64 に収まらない番号もある)。
type Status struct {
	State, Via  string
	RunID       string
	Attempt     string
	Final       string
	Expected    *string
	Mode        *string
	DocType     *string
	Reader      *string
	Checks      *string
	MeaningRisk *string
	Replay      *string
	Failed      *string
	Reason      *string
}

// StatusLine は _status_line (状態の行。キーの順は固定で、値に空白を入れない)。doc_type と reader は mode の次に、付けたときだけ出す。
func StatusLine(s Status) string {
	dash := func(v string) string {
		if v == "" {
			return "-"
		}
		return v
	}
	attempt := "-"
	if s.Attempt != "" && s.Attempt != "0" {
		attempt = s.Attempt
	}
	var b strings.Builder
	b.WriteString("yomiyasu-check: " + s.State)
	for _, f := range [][2]string{{"via", s.Via}, {"run", dash(s.RunID)}, {"attempt", attempt}, {"final", dash(s.Final)}} {
		b.WriteString(" " + f[0] + "=" + f[1])
	}
	for _, f := range []struct {
		key   string
		value *string
	}{
		{"expected", s.Expected}, {"mode", s.Mode}, {"doc_type", s.DocType}, {"reader", s.Reader},
		{"checks", s.Checks}, {"meaning_risk", s.MeaningRisk}, {"replay", s.Replay}, {"failed", s.Failed}, {"reason", s.Reason},
	} {
		if f.value != nil {
			b.WriteString(" " + f.key + "=" + *f.value)
		}
	}
	return b.String()
}

// HostValue は _host_value。記録に入れる環境の値で、空や、展開されなかった ${…} を含む値は nil にする。
func HostValue(value *string) *string {
	if value == nil || *value == "" || strings.Contains(*value, "${") {
		return nil
	}
	return value
}

// FailOn は _fail_on。--fail-on の値を確かめる。(値の並び, 空) か (nil, 誤りの文)。大文字と小文字、「_」と「-」は区別しない。
func FailOn(values []string) (found []string, problem string) {
	for _, value := range values {
		var pieces []string
		for _, piece := range strings.Split(value, ",") {
			if pystr.Strip(piece) != "" {
				pieces = append(pieces, pycli.FailOnWord(piece))
			}
		}
		if len(pieces) == 0 {
			return nil, fmt.Sprintf(failOnPrefix, emptyShown)
		}
		for _, piece := range pieces {
			if piece != "meaning-risk" && piece != "warn" && piece != "error" {
				return nil, fmt.Sprintf(failOnPrefix, piece)
			}
			if !slices.Contains(found, piece) {
				found = append(found, piece)
			}
		}
	}
	return found, ""
}

// Timeout は _timeout。--timeout の秒数 (nil なら 90)。0 より大きい有限の数だけ受け、整数の値は int、それ以外は float を返す (記録の JSON に書くときの型)。
// 受けない値は (nil, 誤りの文)。
func Timeout(raw *string) (pyjson.Value, string) {
	if raw == nil {
		return int64(DefaultTimeout), ""
	}
	value, ok := pystr.Float(*raw)
	if !ok || !(value > 0 && !math.IsInf(value, 1)) {
		return nil, fmt.Sprintf(timeoutError, shown(*raw))
	}
	if value != math.Floor(value) {
		return value, ""
	}
	if value < math.MaxInt64 {
		return int64(value), ""
	}
	whole, _ := new(big.Float).SetFloat64(value).Int(nil)
	return whole, ""
}

// Since は _since。--since の時刻 (時差つきの ISO 8601) を UTC の時刻にする。raw が nil なら (nil, 空)。受けない値は (nil, 誤りの文)。
// 秒より細かい部分は捨てる (記録の created_at は秒までなので、同じ秒に作った記録を古いとみなさない)。
func Since(raw *string) (*time.Time, string) {
	if raw == nil {
		return nil, ""
	}
	problem := fmt.Sprintf(sinceErrorPrefix, shown(*raw))
	match, err := sincePattern.Get().Match(pyre.NewText(pystr.Strip(*raw)), 0, pyre.NoEnd)
	if err != nil || match == nil {
		return nil, problem
	}
	group := func(n int) string { s, _ := match.Group(n); return s }
	zone := group(7)
	minutes := 0
	if zone != "Z" && zone != "z" {
		digits := strings.ReplaceAll(zone[1:], ":", "")
		tail, _ := strconv.Atoi(digits[2:])
		head, _ := strconv.Atoi(digits[:2])
		if tail >= 60 {
			return nil, problem
		}
		minutes = head*60 + tail
		if zone[0] == '-' {
			minutes = -minutes
		}
	}
	var parts [6]int
	for i := range parts {
		parts[i], _ = strconv.Atoi(group(i + 1))
	}
	// timezone(timedelta(minutes=...)) は -24 時間と 24 時間の間 (両端を含まない) だけ受ける
	if minutes <= -24*60 || minutes >= 24*60 {
		return nil, problem
	}
	moment, ok := utcMoment(parts[0], parts[1], parts[2], parts[3], parts[4], parts[5], minutes)
	if !ok {
		return nil, problem
	}
	return &moment, ""
}
