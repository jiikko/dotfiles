package check

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"unicode/utf8"

	"ja-writing/internal/pycli"
	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyops"
	"ja-writing/internal/pyos"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
	"ja-writing/visibility"
)

// Input は _Input (原文・資料・最終稿の 1 つ)。バイト列を 1 回だけ読み、ハッシュと decode_source でそろえた本文を持つ。
type Input struct {
	Label      *string
	Role       string // "source"・"material"・"final"
	Index      int    // 資料は 1 から、ほかは 0
	Path       *string
	Via        string // "file"・"stdin"
	Data       []byte
	SHA256     string
	Text       string
	BOM        bool
	EOL        string
	TextSHA256 string
	// 資料だけ: 最終稿との似かた (新規作成のとき)・原文とみなしたか・その決め方
	Similarity      *float64
	TreatedAsSource bool
	TreatedBy       *string
}

// makeInput は _make_input。UTF-8 として読めなければ誤りの文を返す。
func makeInput(role string, index int, path *string, data []byte) (*Input, string) {
	item, err := newInput(role, index, path, data)
	if err != nil {
		if path == nil {
			return nil, pycli.NotUTF8Stdin
		}
		return nil, pycli.NotUTF8File(*path)
	}
	return item, ""
}

// newInput は _Input(role, index, path, data)。UTF-8 として読めなければ、本家の UnicodeDecodeError と同じ文の誤り (*visibility.Error)。
func newInput(role string, index int, path *string, data []byte) (*Input, error) {
	text, info, err := visibility.DecodeSourceBytes(data)
	if err != nil {
		return nil, err
	}
	via := "file"
	if path == nil {
		via = "stdin"
	}
	return &Input{
		Role: role, Index: index, Path: path, Via: via, Data: data, SHA256: SHA256(data),
		Text: text, BOM: info.BOM, EOL: info.EOL, TextSHA256: TextSHA256(text),
	}, nil
}

// Snapshot は状態フォルダの中の写しの場所。
func (in *Input) Snapshot() string { return "inputs/" + in.SHA256 + ".bin" }

// Token は状態の行の final=。標準入力は本文のハッシュで照らすので text-sha256: を付ける。
func (in *Input) Token() string {
	if in.Via == "stdin" {
		return "text-sha256:" + in.TextSHA256[:12]
	}
	return "sha256:" + in.SHA256[:12]
}

// Record は記録の inputs の 1 つ。資料だけ label・treated_as_source・similarity (小数第 3 位まで)・treated_by を足す。
func (in *Input) Record() (*pyjson.Object, error) {
	o := pyjson.NewObject()
	o.Set("role", in.Role)
	o.Set("index", in.Index)
	o.Set("path", pyjson.OptionalString(in.Path))
	o.Set("via", in.Via)
	o.Set("bytes", len(in.Data))
	o.Set("sha256", in.SHA256)
	o.Set("text_sha256", in.TextSHA256)
	o.Set("bom", in.BOM)
	o.Set("eol", in.EOL)
	o.Set("snapshot", in.Snapshot())
	if in.Role == "material" {
		o.Set("label", pyjson.OptionalString(in.Label))
		o.Set("treated_as_source", in.TreatedAsSource)
		var similarity pyjson.Value
		if in.Similarity != nil {
			rounded, err := pyjson.Round(*in.Similarity, 3)
			if err != nil {
				return nil, err
			}
			similarity = rounded
		}
		o.Set("similarity", similarity)
		o.Set("treated_by", pyjson.OptionalString(in.TreatedBy))
	}
	return o, nil
}

// ReadStdinBytes は _read_stdin_bytes。標準入力が無い (nil) ときも誤りの文にする。
// 本家の「文字列しか読めない標準入力」の枝 (StringIO) は、Go の標準入力が常にバイト列なので写さない。
func ReadStdinBytes(r io.Reader) ([]byte, string) {
	if r == nil {
		return nil, noStdinError
	}
	return pycli.ReadStdinBytes(r)
}

// readNamed は _read_named。given が "-" なら stdin のバイト列を使う。最終稿は検査のあとで読み直すので、通常のファイルでないもの
// (/dev/stdin や <(…) のパイプ) は誤りにして --final - を案内する (読み直すと空になり、変わっていない最終稿を「途中で変わった」と報告するため)。
// 作業フォルダが消えているときは *NoWorkingDirectoryError。
func readNamed(role string, index int, given string, stdin []byte) (*Input, string, error) {
	if given == "-" {
		item, problem := makeInput(role, index, nil, stdin)
		return item, problem, nil
	}
	data, problem := readGiven(given, role == "final")
	if problem != "" {
		return nil, problem, nil
	}
	abs, err := AbsPath(given)
	if err != nil {
		return nil, "", err
	}
	item, problem := makeInput(role, index, &abs, data)
	if problem != "" {
		return nil, pycli.NotUTF8File(given), nil
	}
	return item, "", nil
}

// readGiven は渡された綴りのまま開いて全部読む ('x.md/' を本家と同じく開けない誤りにするため、パスを整えない)。
// Python の open はフォルダを開くと IsADirectoryError になるので、開いた直後にフォルダを断る (最終稿の通常のファイルの検査より先)。
func readGiven(given string, regularOnly bool) ([]byte, string) {
	handle, err := pyos.Open(given)
	if err != nil {
		return nil, pycli.OpenProblem(given, err)
	}
	defer func() { _ = handle.Close() }()
	info, err := handle.Stat()
	if err != nil {
		return nil, pycli.OpenProblem(given, err)
	}
	if info.IsDir() {
		return nil, pycli.OpenProblem(given, syscall.EISDIR)
	}
	if regularOnly && !info.Mode().IsRegular() {
		return nil, fmt.Sprintf(finalNotRegular, given)
	}
	data, err := io.ReadAll(handle)
	if err != nil {
		return nil, pycli.OpenProblem(given, err)
	}
	return data, ""
}

var materialMarkerPattern = regexp.MustCompile(`^<<<yomiyasu:material(?:[ \t]+(.+?))?>>>[ \t]*$`)

// materialMarker は _material_marker。資料の区切りの行なら (出典 (無ければ nil), 真)。出典は 200 字までで、>>> を含まない。
func materialMarker(bare []byte) (*string, bool) {
	if !utf8.Valid(bare) {
		return nil, false
	}
	m := materialMarkerPattern.FindSubmatchIndex(bare)
	if m == nil {
		return nil, false
	}
	if m[2] < 0 {
		return nil, true
	}
	label := string(bare[m[2]:m[3]])
	if utf8.RuneCountInString(label) > 200 || strings.Contains(label, ">>>") {
		return nil, false
	}
	return &label, true
}

// splitLinesKeep は bytes.splitlines(True) (区切りは \n・\r・\r\n だけ。区切りを行に残す)。
func splitLinesKeep(data []byte) [][]byte {
	var rows [][]byte
	start := 0
	for i := 0; i < len(data); i++ {
		switch data[i] {
		case '\n':
			rows = append(rows, data[start:i+1])
			start = i + 1
		case '\r':
			end := i + 1
			if end < len(data) && data[end] == '\n' {
				end++
			}
			rows = append(rows, data[start:end])
			start = end
			i = end - 1
		}
	}
	if start < len(data) {
		rows = append(rows, data[start:])
	}
	return rows
}

// asciiSpace は bytes.strip() が落とす字。
const asciiSpace = " \t\n\r\x0b\x0c"

// firstChars は s の先頭 n 字 (コードポイントで数える)。
func firstChars(s string, n int) string {
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}

// looksLikeMarker は区切りの行の書き損じか (前後の空白を落として小文字にすると <<<yomiyasu: で始まる)。
func looksLikeMarker(text string) bool {
	return strings.HasPrefix(pyunicode.Lower(pystr.Strip(text)), markerHead)
}

// splitMaterial は _split_material。--material のファイルを <<<yomiyasu:material 出典>>> の行で資料に分ける。区切りは資料の本文に含めない。
// 区切りの無い入力は、バイト列をそのまま 1 つの資料にする。
func splitMaterial(item *Input, firstIndex int) ([]*Input, string) {
	data := bytes.TrimPrefix(item.Data, []byte(utf8BOMBytes))
	rows := splitLinesKeep(data)
	markers := map[int]*string{}
	first := -1
	for i, row := range rows {
		if first < 0 && len(bytes.Trim(row, asciiSpace)) > 0 {
			first = i
		}
	}
	for i, row := range rows {
		bare := bytes.TrimRight(row, "\r\n")
		if label, ok := materialMarker(bare); ok {
			markers[i] = label
		} else if looksLikeMarker(string(bare)) {
			return nil, fmt.Sprintf(badMarker, i+1, firstChars(string(bare), markerShown))
		}
	}
	if _, ok := markers[first]; !ok || first < 0 {
		item.Index = firstIndex
		return []*Input{item}, ""
	}
	starts := make([]int, 0, len(markers))
	for i := range markers {
		starts = append(starts, i)
	}
	slices.Sort(starts)
	blocks := make([]*Input, 0, len(starts))
	for n, start := range starts {
		end := len(rows)
		if n+1 < len(starts) {
			end = starts[n+1]
		}
		body := bytes.Join(rows[start+1:end], nil)
		block, problem := makeInput("material", firstIndex+n, item.Path, body)
		if problem != "" {
			return nil, problem
		}
		block.Label = markers[start]
		blocks = append(blocks, block)
	}
	return blocks, ""
}

const utf8BOMBytes = "\xef\xbb\xbf"

// stdinPart は --stdin の区切りで分けた 1 つ (役割・バイト列・資料の出典)。
type stdinPart struct {
	role  string
	data  []byte
	label *string
}

var stdinMarkers = map[string]string{
	"<<<yomiyasu:original>>>": "source", "<<<yomiyasu:material>>>": "material", "<<<yomiyasu:final>>>": "final",
}

// lineEnd は data[from:] の最初の行の終わり (\r\n・\r・\n) の始まりと終わり。無ければ (-1, -1)。
func lineEnd(data []byte, from int) (int, int) {
	i := bytes.IndexAny(data[from:], "\r\n")
	if i < 0 {
		return -1, -1
	}
	i += from
	if data[i] == '\r' && i+1 < len(data) && data[i+1] == '\n' {
		return i, i + 2
	}
	return i, i + 1
}

// splitStdin は _split_stdin (with_labels の形)。各部分は、区切りの行の次のバイトから、次の区切りの行の先頭まで (末尾の改行を含む)。
// 先頭の BOM は 1 つ外す。各部分のバイト列は書き換えない。
func splitStdin(data []byte) ([]stdinPart, string) {
	data = bytes.TrimPrefix(data, []byte(utf8BOMBytes))
	type span struct {
		role       string
		start, end int
		label      *string
	}
	var parts []span
	var current *span
	position, number := 0, 0
	for position < len(data) {
		bareEnd, following := lineEnd(data, position)
		if bareEnd < 0 {
			bareEnd, following = len(data), len(data)
		}
		number++
		bare := data[position:bareEnd]
		role := stdinMarkers[string(bare)]
		var label *string
		if l, ok := materialMarker(bare); ok {
			role, label = "material", l
		}
		if role == "" && bytes.Contains(bare, []byte("<<<")) {
			text := visibility.DecodeReplace(bare)
			if looksLikeMarker(text) {
				shown := text
				if utf8.RuneCountInString(text) > markerShown {
					shown = firstChars(text, markerShown) + "…"
				}
				return nil, fmt.Sprintf(badMarker, number, shown)
			}
		}
		switch {
		case role != "":
			if current != nil {
				parts = append(parts, *current)
			}
			current = &span{role: role, start: following, end: following, label: label}
		case current == nil:
			if len(bytes.Trim(bare, asciiSpace)) > 0 {
				return nil, "空白以外の最初の行が区切りの行ではありません"
			}
		default:
			current.end = following
		}
		position = following
	}
	if current != nil {
		parts = append(parts, *current)
	}
	count := map[string]int{}
	for _, p := range parts {
		count[p.role]++
	}
	switch {
	case count["final"] == 0:
		return nil, "<<<yomiyasu:final>>> の行がありません"
	case count["final"] > 1:
		return nil, "<<<yomiyasu:final>>> の行が2つ以上あります"
	case count["source"] > 1:
		return nil, "<<<yomiyasu:original>>> の行が2つ以上あります"
	case count["source"] == 0 && count["material"] == 0:
		return nil, "<<<yomiyasu:original>>> の行も <<<yomiyasu:material>>> の行もありません"
	}
	out := make([]stdinPart, len(parts))
	for i, p := range parts {
		out[i] = stdinPart{role: p.role, data: data[p.start:p.end], label: p.label}
	}
	return out, ""
}

// PathProblem は _path_problem。コマンドラインで渡されたファイル (nil は渡されていない) のうち、読まないものがあれば誤りの文。
// どのファイルも読む前、状態フォルダに何かを書く前に呼ぶ。作業フォルダが消えているときは *NoWorkingDirectoryError。
func PathProblem(command string, paths []*string) (string, error) {
	if slices.ContainsFunc(paths, func(p *string) bool { return p != nil && *p != "-" }) {
		if _, err := Getcwd(); err != nil {
			return "", err
		}
	}
	for _, path := range paths {
		if path == nil {
			continue
		}
		if reason, ok := visibility.InputPathProblem(*path); !ok {
			return fmt.Sprintf(pathProblemError, *path, reason, pathHints[command]), nil
		}
	}
	return "", nil
}

// InputArgs は _read_inputs が読む引数 (--stdin・--final・--source・--material の並び)。
type InputArgs struct {
	Stdin     bool
	Final     string
	Source    *string
	Materials []string
}

// Inputs は _read_inputs の結果 (原文は無ければ nil)。
type Inputs struct {
	Source    *Input
	Materials []*Input
	Final     *Input
}

// ReadInputs は _read_inputs。原文・資料・最終稿を 1 回ずつ読む。(結果, "") か (nil, 誤りの文)。作業フォルダが消えているときは *NoWorkingDirectoryError。
func ReadInputs(args InputArgs, stdin io.Reader) (*Inputs, string, error) {
	if args.Stdin {
		return readStdinInputs(stdin)
	}
	given := append([]string{args.Final}, args.Materials...)
	if args.Source != nil {
		given = append(given, *args.Source)
	}
	var stdinData []byte
	if slices.Contains(given, "-") {
		data, problem := ReadStdinBytes(stdin)
		if problem != "" {
			return nil, problem, nil
		}
		stdinData = data
	}
	out := &Inputs{Materials: []*Input{}}
	if args.Source != nil {
		source, problem, err := readNamed("source", 0, *args.Source, stdinData)
		if err != nil || problem != "" {
			return nil, problem, err
		}
		out.Source = source
	}
	for i, path := range args.Materials {
		item, problem, err := readNamed("material", i+1, path, stdinData)
		if err != nil || problem != "" {
			return nil, problem, err
		}
		blocks, problem := splitMaterial(item, len(out.Materials)+1)
		if problem != "" {
			return nil, problem, nil
		}
		out.Materials = append(out.Materials, blocks...)
	}
	final, problem, err := readNamed("final", 0, args.Final, stdinData)
	if err != nil || problem != "" {
		return nil, problem, err
	}
	out.Final = final
	return out, "", nil
}

func readStdinInputs(stdin io.Reader) (*Inputs, string, error) {
	data, problem := ReadStdinBytes(stdin)
	if problem != "" {
		return nil, problem, nil
	}
	pieces, problem := splitStdin(data)
	if problem != "" {
		message := fmt.Sprintf(stdinSplitError, problem, stdinForm)
		if strings.Contains(problem, badMarkerKey) {
			message += "\n" + badMarkerTail
		}
		return nil, message, nil
	}
	out := &Inputs{Materials: []*Input{}}
	for _, piece := range pieces {
		index := 0
		if piece.role == "material" {
			index = len(out.Materials) + 1
		}
		item, problem := makeInput(piece.role, index, nil, piece.data)
		if problem != "" {
			return nil, problem, nil
		}
		item.Label = piece.label
		switch piece.role {
		case "source":
			out.Source = item
		case "material":
			out.Materials = append(out.Materials, item)
		default:
			out.Final = item
		}
	}
	return out, "", nil
}

// Target は _Target (verify で照らす最終稿と --source)。バイト列のハッシュと、UTF-8 として読めれば本文のハッシュ。
type Target struct {
	Path       *string
	Via        string
	Data       []byte
	SHA256     string
	TextSHA256 *string
}

func newTarget(path *string, data []byte) *Target {
	via := "file"
	if path == nil {
		via = "stdin"
	}
	t := &Target{Path: path, Via: via, Data: data, SHA256: SHA256(data)}
	if text, _, err := visibility.DecodeSourceBytes(data); err == nil {
		sum := TextSHA256(text)
		t.TextSHA256 = &sum
	}
	return t
}

// Token は状態の行の final=。ファイルはバイト列の SHA-256、標準入力は本文のハッシュ。本文のハッシュが無い (UTF-8 として読めない) 標準入力は、
// 本家の assert と同じ AssertionError (verify_quick に渡したバイト列でだけ起きる。コマンドの標準入力は読む所で断る)。
func (t *Target) Token() string {
	if t.Via == "stdin" {
		if t.TextSHA256 == nil {
			pyops.Raise("AssertionError", "")
		}
		return "text-sha256:" + (*t.TextSHA256)[:12]
	}
	return "sha256:" + t.SHA256[:12]
}

// JSON は as_json。
func (t *Target) JSON() *pyjson.Object {
	o := pyjson.NewObject()
	o.Set("path", pyjson.OptionalString(t.Path))
	o.Set("via", t.Via)
	o.Set("sha256", t.SHA256)
	o.Set("text_sha256", pyjson.OptionalString(t.TextSHA256))
	return o
}

// ReadTarget は _read_target。verify の --final と --source を読む。(Target, "") か (nil, 誤りの文)。作業フォルダが消えているときは *NoWorkingDirectoryError。
func ReadTarget(given string, stdin []byte) (*Target, string, error) {
	if given == "-" {
		t := newTarget(nil, stdin)
		if t.TextSHA256 == nil {
			return nil, pycli.NotUTF8Stdin, nil
		}
		return t, "", nil
	}
	data, problem := readGiven(given, false)
	if problem != "" {
		return nil, problem, nil
	}
	abs, err := AbsPath(given)
	if err != nil {
		return nil, "", err
	}
	return newTarget(&abs, data), "", nil
}
