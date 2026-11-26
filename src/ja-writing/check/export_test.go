package check

import (
	"ja-writing/internal/pyos"
	"ja-writing/internal/pyre"
	"ja-writing/visibility"
)

// main_ext_test.go (package check_test) の TestMain が、子として起こされたかを見る印。
const (
	ChildEnvName = childEnvName
	ChildReal    = childReal
	ChildHang    = childHang
)

// Similarity は _similarity。資料と最終稿の似かた (0 から 1)。
func Similarity(material, final string) float64 {
	m, tm := bigramCounts(material, false)
	f, tf := bigramCounts(final, false)
	return similarityOfCounts(m, tm, f, tf)
}

// toolVersionPattern は _TOOL_VERSION (道具のファイルの __version__ の行)。
var toolVersionPattern = pyre.NewLazy(`^__version__\s*=\s*["']([^"']+)["']\s*$`, pyre.MULTILINE)

// DirFiles は scripts フォルダ (と、その隣の references/lists/) を読む ToolFiles。本家の _HERE と同じ置き場の読み方。
type DirFiles struct{ Scripts string }

// Tool は ToolFiles。
func (d DirFiles) Tool(name string) (string, *string, error) {
	data, err := pyos.ReadFile(visibility.PathJoin(d.Scripts, name))
	if err != nil {
		return "", nil, err
	}
	// 本家は decode("utf-8", "replace") した文字列で探す。バージョンの値に不正なバイトは入らない前提で、バイト列のまま探す
	match, err := toolVersionPattern.Get().Search(pyre.NewText(string(data)), 0, pyre.NoEnd)
	if err != nil {
		return "", nil, err
	}
	var version *string
	if match != nil {
		v, _ := match.Group(1)
		version = &v
	}
	return SHA256(data), version, nil
}

// List は ToolFiles。
func (d DirFiles) List(name string) (string, error) {
	data, err := pyos.ReadFile(visibility.PathJoin(d.Scripts, "../references/lists/"+name+".txt"))
	if err != nil {
		return "", err
	}
	return SHA256(data), nil
}
