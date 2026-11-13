package visibility

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
)

// 語の一覧のフォルダと、その中のファイルの名前と上限 (本家の WORDS_* と PATTERNS_FILE と同じ値)。
const (
	WordsFolder   = ".yomiyasu-words"
	WordsMaxLines = 10000
	PatternsFile  = "flag-patterns.txt"
)

// WordsFiles は (キーの先頭に付ける語, ファイルの名前)。context.words のキーと出力の行の順もこの順 (本家の WORDS_FILES)。
var WordsFiles = [][2]string{{"allow", "allow.txt"}, {"flag", "flag.txt"}}

func wordsError(format string, args ...any) *Error {
	return newError(TypeWordsError, fmt.Sprintf(format, args...))
}

// wordsHome は _words_home (--words の値の頭の ~ をホームフォルダに置き換える。check と hook の _home_path と同じ決まり)。
func wordsHome(value string) string {
	if value != "~" && !strings.HasPrefix(value, "~/") {
		return value
	}
	configured, set := os.LookupEnv("HOME")
	if set && strings.Trim(stripPy(configured), "/") == "" {
		return value
	}
	home, ok := expandUser()
	if !ok {
		return value
	}
	// どちらも pathlib の Path が整えた字面 (空の部品と . を落とす)。
	home = parsePure(home).String()
	expanded := expandedPure(value, home)
	if !strings.HasPrefix(home, "~") && strings.Trim(stripPy(home), "/") != "" && !strings.HasPrefix(expanded, "~") {
		return expanded
	}
	return value
}

// expandUser は os.path.expanduser("~")。HOME が無いときはパスワードデータベース。取れなければ ok が偽 (pathlib は RuntimeError にし、本家はそれを握って値をそのまま返す)。
func expandUser() (string, bool) {
	home, set := os.LookupEnv("HOME")
	if !set {
		var err error
		if home, err = lookupHomeByUID(); err != nil {
			return "", false
		}
	}
	if home = strings.TrimRight(home, "/"); home != "" {
		return home, true
	}
	return "/", true
}

// expandedPure は str(Path(value).expanduser()): ~ の部品を (整えた) ホームに置き換え、残りの部品は pathlib の整え方 (空と . を落とす) で並べる。
func expandedPure(value, home string) string {
	pv := parsePure(value)
	ph := parsePure(home)
	ph.parts = append(append([]string(nil), ph.parts...), pv.parts[1:]...)
	return ph.String()
}

// FindWordsFolder は find_words_folder。語の一覧のフォルダの絶対パス (フォルダが無ければ ok が偽)。
//
//  1. option (--words) があれば、そのフォルダ (相対パスは cwd から。無いかフォルダでなければ WordsError)。
//  2. cwd から上へたどり、.git (フォルダでもファイルでもよい) がある最初のフォルダの .yomiyasu-words/。
//     .git がファイルで一覧のフォルダが無ければ、gitdir と commondir から元の checkout の一覧を探す。
//  3. git の外では cwd の .yomiyasu-words/。2 と 3 は、フォルダが無ければ ok が偽。
//
// cwd が nil (作業ディレクトリを読めない) ときは、2 と 3 を探さずに ok が偽、相対パスの option は WordsError。
func FindWordsFolder(cwd *string, option *string) (string, bool, error) {
	if option != nil {
		value := wordsHome(*option)
		if stripPy(value) == "" {
			return "", false, wordsError("--words に空の値は使えません")
		}
		if !strings.HasPrefix(value, "/") {
			if cwd == nil {
				return "", false, wordsError("Error opening folder %s: 作業ディレクトリを読めないため、相対パスを決められません", *option)
			}
			value = pureJoin(*cwd, value)
		}
		folder, err := AbsolutePath(value)
		if err != nil {
			return "", false, err
		}
		if !pathIsDir(folder) {
			reason := "フォルダがありません"
			if pathExists(folder) {
				reason = "フォルダではありません"
			}
			return "", false, wordsError("Error opening folder %s: --words の%s", *option, reason)
		}
		return folder, true, nil
	}
	if cwd == nil {
		return "", false, nil
	}
	originPath, err := AbsolutePath(*cwd)
	if err != nil {
		return "", false, err
	}
	origin := parsePure(originPath).String()
	current := origin
	var gitPath, folder string
	for {
		gitPath = pureJoin(current, ".git")
		if pathExists(gitPath) {
			folder = pureJoin(current, WordsFolder)
			break
		}
		parent := pureParent(current)
		if parent == current {
			folder = pureJoin(origin, WordsFolder)
			break
		}
		current = parent
	}
	if pathIsDir(folder) {
		return folder, true, nil
	}
	// .git がふつうのファイルのときだけ中を読む (フォルダなら linked worktree でない。名前付きパイプなどは開かない)。
	if pathIsFile(gitPath) {
		if f, ok := worktreeWordsFolder(gitPath); ok {
			return f, true, nil
		}
	}
	return "", false, nil
}

// worktreeWordsFolder は _worktree_words_folder。linked worktree の .git ファイルの gitdir と commondir から、元の checkout の語の一覧を探す。
// .git ファイルや commondir を読めないとき、checkout が決まらないとき (bare リポジトリなど) は ok が偽。
func worktreeWordsFolder(gitFile string) (string, bool) {
	data, err := os.ReadFile(gitFile)
	if err != nil || !pyunicode.ValidUTF8(string(data)) {
		return "", false
	}
	marker := stripPy(universalNewlines(string(data)))
	if !strings.HasPrefix(marker, "gitdir:") {
		return "", false
	}
	target := stripPy(strings.TrimPrefix(marker, "gitdir:"))
	if target == "" {
		return "", false
	}
	gitDir := pureJoin(pureParent(gitFile), target)
	commondirPath := pureJoin(gitDir, "commondir")
	if !pathIsFile(commondirPath) {
		return "", false
	}
	cd, err := os.ReadFile(commondirPath)
	if err != nil || !pyunicode.ValidUTF8(string(cd)) {
		return "", false
	}
	common := stripPy(universalNewlines(string(cd)))
	if common == "" {
		return "", false
	}
	abs, err := AbsolutePath(pureJoin(gitDir, common))
	if err != nil {
		return "", false
	}
	checkout, ok := sharedCheckout(parsePure(abs).String())
	if !ok {
		return "", false
	}
	folder := pureJoin(checkout, WordsFolder)
	if pathIsDir(folder) {
		return folder, true
	}
	return "", false
}

// sharedCheckout は _shared_checkout。共有の git フォルダから元の checkout を決める。config と config.worktree (extensions.worktreeConfig のとき、
// git は core.worktree をこちらに移す) の [core] を読み、あとに書いた値を使う。worktree があればその先 (相対の値は git フォルダから数える)。
// なければ、bare でなく、フォルダの名前が .git のときだけその親。どちらでもなければ ok が偽。
// config が無いときと開けないときは、書いていないものとして扱う。UTF-8 でない字は置き換えて読み、ほかの行の値は使う。
// 読むのは git が書く形 (1 行に「キー = 値」) だけ。
func sharedCheckout(commonDir string) (string, bool) {
	worktree, haveWorktree, bare := "", false, false
	for _, name := range []string{"config", "config.worktree"} {
		path := pureJoin(commonDir, name)
		if !pathIsFile(path) {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		text := strings.TrimLeft(decodeUTF8Replace(data), utf8BOM)
		section := ""
		for _, row := range pystr.SplitLines(text, false) {
			row = stripPy(row)
			if strings.HasPrefix(row, "[") {
				before, _, _ := strings.Cut(row[1:], "]")
				section = pyLowerASCII(stripPy(before))
			} else if section == "core" {
				key, value, sep := strings.Cut(row, "=")
				key, value = pyLowerASCII(stripPy(key)), strings.Trim(stripPy(value), `"`)
				if sep && key == "worktree" && value != "" {
					abs, err := AbsolutePath(pureJoin(commonDir, value))
					if err == nil {
						worktree, haveWorktree = parsePure(abs).String(), true
					}
				} else if sep && key == "bare" {
					switch pyLowerASCII(value) {
					case "true", "yes", "on", "1":
						bare = true
					default:
						bare = false
					}
				}
			}
		}
	}
	if haveWorktree {
		return worktree, true
	}
	if pureName(commonDir) == ".git" && !bare {
		return pureParent(commonDir), true
	}
	return "", false
}

// wordsTextRows は _words_text_rows。1 つのファイルのバイト列を行の並びにする (BOM を外し、改行をそろえる)。
// UTF-8 でないときと 1 万行を超えるときは WordsError (行番号つき)。
func wordsTextRows(path string, data []byte) ([]string, error) {
	if start := utf8ErrorStart(data); start >= 0 {
		before := data[:start]
		line := strings.Count(string(before), "\n") + strings.Count(string(before), "\r") - strings.Count(string(before), "\r\n") + 1
		return nil, wordsError("Error opening file %s: UTF-8 として読めません（%d行目）", path, line)
	}
	text, _ := DecodeSource(string(data))
	rows := strings.Split(text, "\n")
	if len(rows) > 0 && rows[len(rows)-1] == "" {
		rows = rows[:len(rows)-1]
	}
	if len(rows) > WordsMaxLines {
		return nil, wordsError("Error opening file %s: 行が多すぎます（%d行目。%s行までです）", path, WordsMaxLines+1, commaInt(WordsMaxLines))
	}
	return rows, nil
}

// wordsLines は _words_lines。語の並び (空行と # で始まる行を除き、前後の空白を外す)。
func wordsLines(path string, data []byte) ([]string, error) {
	rows, err := wordsTextRows(path, data)
	if err != nil {
		return nil, err
	}
	words := []string{}
	for _, row := range rows {
		word := stripPy(row)
		if word != "" && !strings.HasPrefix(word, "#") {
			words = append(words, word)
		}
	}
	return words, nil
}

type flagEntry struct{ word, replacement string }

// flagEntries は _flag_entries。flag.txt を (使わない語, 使う語) の並びにする。行の前後の空白 (TAB も) を除いてから最初の TAB で分け、左と右の前後の空白を除く。
// TAB が無ければ使う語は ""。空行とコメントは除く。行頭が TAB の行 (字下げした語) は、2.5.0 までと同じく 1 語として読む。
// 使う語の中に TAB が残る行 (3 列の行) と、使う語が FlagReplacementMax 字を超える行は WordsError。使う語が使わない語と同じ行は、使う語の無い行として読む。
func flagEntries(path string, data []byte) ([]flagEntry, error) {
	rows, err := wordsTextRows(path, data)
	if err != nil {
		return nil, err
	}
	entries := []flagEntry{}
	for i, row := range rows {
		number := i + 1
		row = stripPy(row)
		if row == "" || strings.HasPrefix(row, "#") {
			continue
		}
		word, replacement, _ := strings.Cut(row, "\t")
		word, replacement = stripPy(word), stripPy(replacement)
		if strings.Contains(replacement, "\t") {
			return nil, wordsError("Error opening file %s: %d行目の使う語の中にタブがあります（1行に書けるのは「使わない語<TAB>使う語」の2つまでです）", path, number)
		}
		if pystr.Len(replacement) > FlagReplacementMax {
			return nil, wordsError("Error opening file %s: %d行目の使う語は %d 字までです", path, number, FlagReplacementMax)
		}
		if replacement == word {
			replacement = ""
		}
		entries = append(entries, flagEntry{word, replacement})
	}
	return entries, nil
}

// FlagPattern は flag-patterns.txt の 1 行 (名前・正規表現の文字列・組んだ正規表現・必ず当たる字の並び)。
type FlagPattern struct {
	Name     string
	Pattern  string
	Compiled *CheckedPattern
}

// patternLines は _patterns_lines。flag-patterns.txt のバイト列を [(名前, 正規表現の文字列, 組んだ正規表現, 必ず当たる字の並び)] にする。
// 使えない行は WordsError (ファイル名と行番号つき)。
func patternLines(path string, data []byte) ([]FlagPattern, error) {
	rows, err := wordsTextRows(path, data)
	if err != nil {
		return nil, err
	}
	patterns := []FlagPattern{}
	total := 0
	for i, row := range rows {
		number := i + 1
		if stripPy(row) == "" || strings.HasPrefix(strings.TrimLeftFunc(row, isPySpace), "#") {
			continue
		}
		if !strings.Contains(row, "\t") {
			return nil, wordsError("Error opening file %s: %d行目に、名前と正規表現を分けるタブがありません", path, number)
		}
		name, pattern, _ := strings.Cut(row, "\t")
		// 正規表現の前後は半角の空白とタブだけを除く (全角の空白は正規表現の字として残す。空白で終わる形は [ ] と書く)。
		name, pattern = stripPy(name), str(stripSpaceTab(runesOf(pattern)))
		if name == "" {
			return nil, wordsError("Error opening file %s: %d行目の名前が空です", path, number)
		}
		if pystr.Len(name) > PatternNameMax {
			return nil, wordsError("Error opening file %s: %d行目の名前は %d 字までです", path, number, PatternNameMax)
		}
		if len(patterns) >= PatternsMax {
			return nil, wordsError("Error opening file %s: パターンが多すぎます（%d行目。%s個までです）", path, number, commaInt(PatternsMax))
		}
		checked, cerr := CheckPattern(pattern)
		if cerr != nil {
			var pe *Error
			if errors.As(cerr, &pe) && pe.Type == TypePatternError {
				return nil, wordsError("Error opening file %s: %d行目の正規表現を使えません（%s）", path, number, pe.Message)
			}
			return nil, cerr
		}
		total += checked.Cost
		if total > PatternsMaxCost {
			return nil, wordsError("Error opening file %s: %d行目までで、試す組み合わせの合計が多すぎます（%s までです）", path, number, commaInt(PatternsMaxCost))
		}
		patterns = append(patterns, FlagPattern{name, pattern, checked})
	}
	return patterns, nil
}

func isPySpace(r rune) bool { return pyunicode.IsSpace(r) }

// Words は load_words の戻り値 (語の一覧)。SHA-256 の欄は、ファイルが無いとき nil (本家の None)。
type Words struct {
	Path             string
	Allow            []string
	Flag             []string
	AllowLines       int
	FlagLines        int
	AllowSHA256      *string
	FlagSHA256       *string
	FlagReplacements map[string]string
	// Patterns は flag-patterns.txt があるときだけ持つ (本家は無いとき、flag_patterns・flag_patterns_lines・flag_patterns_sha256 のキーを足さない)。
	Patterns *WordsPatterns
}

// WordsPatterns は flag-patterns.txt の読み結果。
type WordsPatterns struct {
	Entries []FlagPattern
	Lines   int
	SHA256  string
}

// wordsFile は読んだファイルの中身と、その SHA-256 (16 進)。
type wordsFile struct {
	data   []byte
	sha256 string
}

// readWordsFile はファイルを読む。無い (ENOENT・ENOTDIR) ときは nil、ほかの失敗は WordsError (Python の strerror と同じ文)。
func readWordsFile(path string) (*wordsFile, error) {
	data, rerr := os.ReadFile(path)
	if rerr == nil {
		sum := sha256.Sum256(data)
		return &wordsFile{data, hex.EncodeToString(sum[:])}, nil
	}
	if isAbsentError(rerr) {
		return nil, nil
	}
	return nil, wordsError("Error opening file %s: ファイルを開けません（%s）", path, OSErrorReason(path, rerr))
}

// LoadWords は load_words。フォルダの allow.txt と flag.txt (と flag-patterns.txt) を読む。どれも無いときは ok が偽 (一覧なし)。
// flag は TAB の左だけ。無いほうのファイルは、語なし、数 0、SHA-256 は nil。flag-patterns.txt だけがあっても一覧ありとみなす。読めないファイルと使えないパターンは WordsError。
func LoadWords(folder string) (*Words, bool, error) {
	w := &Words{Path: folder, FlagReplacements: map[string]string{}, Allow: []string{}, Flag: []string{}}
	found := false
	for _, f := range append(append([][2]string(nil), WordsFiles...), [2]string{"flag_patterns", PatternsFile}) {
		key, name := f[0], f[1]
		path := pureJoin(folder, name)
		file, err := readWordsFile(path)
		if err != nil {
			return nil, false, err
		}
		var digest *string
		var words []string
		if file == nil {
			if key == "flag_patterns" {
				continue
			}
			words = []string{}
		} else {
			found = true
			switch key {
			case "flag":
				entries, err := flagEntries(path, file.data)
				if err != nil {
					return nil, false, err
				}
				words = make([]string, 0, len(entries))
				for _, e := range entries {
					if e.replacement != "" {
						w.FlagReplacements[e.word] = e.replacement
					}
					words = append(words, e.word)
				}
			case "flag_patterns":
				entries, err := patternLines(path, file.data)
				if err != nil {
					return nil, false, err
				}
				w.Patterns = &WordsPatterns{Entries: entries, Lines: len(entries), SHA256: file.sha256}
				continue
			default:
				if words, err = wordsLines(path, file.data); err != nil {
					return nil, false, err
				}
			}
			digest = &file.sha256
		}
		switch key {
		case "allow":
			w.Allow, w.AllowLines, w.AllowSHA256 = words, len(words), digest
		case "flag":
			w.Flag, w.FlagLines, w.FlagSHA256 = words, len(words), digest
		}
	}
	if !found {
		return nil, false, nil
	}
	return w, true, nil
}

// ResolveWords は resolve_words。--words と --no-words から語の一覧を決める。使うときは LoadWords の値、使わないときは ok が偽。
// disabled (--no-words) なら探さない。cwd は作業ディレクトリ (読めなければ nil)。読めない一覧は WordsError。
func ResolveWords(cwd, option *string, disabled bool) (*Words, bool, error) {
	if disabled {
		return nil, false, nil
	}
	folder, found, err := FindWordsFolder(cwd, option)
	if err != nil || !found {
		return nil, false, err
	}
	return LoadWords(folder)
}

// CurrentDirectory は current_directory。作業ディレクトリか、読めない (消えた) ときは ok が偽。
func CurrentDirectory() (string, bool) { return getcwd() }

// WordsContext は words_context の戻り値 (JSON の context.words と check の記録の options.words に入れる 5 つのキー。flag-patterns.txt があれば 7 つ)。
type WordsContext struct {
	Path        string
	AllowLines  int
	FlagLines   int
	AllowSHA256 *string
	FlagSHA256  *string
	// PatternsLines と PatternsSHA256 は flag-patterns.txt があるときだけ持つ。
	PatternsLines  int
	PatternsSHA256 *string
}

// WordsContextOf は words_context。
func WordsContextOf(w *Words) WordsContext {
	c := WordsContext{Path: w.Path, AllowLines: w.AllowLines, FlagLines: w.FlagLines, AllowSHA256: w.AllowSHA256, FlagSHA256: w.FlagSHA256}
	if w.Patterns != nil {
		d := w.Patterns.SHA256
		c.PatternsLines, c.PatternsSHA256 = w.Patterns.Lines, &d
	}
	return c
}

// WordsLabel は words_label (人が読む出力の「語の一覧: <パス>（allow N語、flag N語）。」。flag-patterns.txt があれば「、flag-patterns N行」を足す)。
func WordsLabel(c WordsContext) string {
	patterns := ""
	if c.PatternsSHA256 != nil {
		patterns = fmt.Sprintf("、flag-patterns %d行", c.PatternsLines)
	}
	return fmt.Sprintf("語の一覧: %s（allow %d語、flag %d語%s）。", c.Path, c.AllowLines, c.FlagLines, patterns)
}
