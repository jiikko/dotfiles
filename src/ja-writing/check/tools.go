package check

import (
	"errors"
	"io/fs"
	"os"
	"syscall"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/visibility"
)

// 記録の tools の欄。互換の決め方 (issue 1077 の (a)): ja-writing の記録を本家の verify が読め、本家の記録を ja-writing の verify が読めるように、
// 欄には「答えを合わせた本家 3.1.0 のファイル」の SHA-256 を書く。Go の実行ファイルの値ではない。
// 失うもの: ja-writing の挙動を直しても tools の値は変わらないので、古い記録が tool_changed にならない (本家は .py を直せば古い記録を弾けた)。
// ja-writing の挙動のバージョンは記録の python.implementation の文字列に入れる。

// ToolNames は記録の tools の道具 4 本 (本家の _TOOL_NAMES)。
var ToolNames = []string{"yomiyasu_check.py", "yomiyasu_lint.py", "yomiyasu_diff.py", "markdown_visibility.py"}

// ListNames は記録の tools に足す語の一覧 7 本 (本家の _LIST_NAMES)。
var ListNames = []string{"nominalization", "intensifiers", "feelings", "hedges", "katakana-terms", "thin-evaluation", "renyo-nouns"}

// FactGapsList は --fact-gaps を付けた run の記録の tools にだけ入る一覧の名前、FactGapsKey はそのキー。
const (
	FactGapsList = "fact-gaps"
	FactGapsKey  = "references/lists/" + FactGapsList + ".txt"
)

// releasedVersion は答えを合わせた本家のバージョン (道具 4 本の __version__)。
const releasedVersion = "3.1.0"

// releasedToolSHA256・releasedListSHA256 は本家 db78b9bc の道具と一覧の SHA-256 (oracle/upstream.sha256 の値。同じであることはテストが見る)。
var (
	releasedToolSHA256 = map[string]string{
		"yomiyasu_check.py":      "4355847c6cc8b8f08e7541c7f04add5eb4e0ef7b9e0176e6a069bfa8b9679ca8",
		"yomiyasu_lint.py":       "bde745c46242ab0a923503bbe5488178f2b503e972498112e46df2ec79559767",
		"yomiyasu_diff.py":       "25e5e1db015297a9cbf0642482091b8c6f47b5c8b5f03dcc8b565a0ab5aecaba",
		"markdown_visibility.py": "c8d410c5379c00c4bb94fc06d3a5a3fee118ac6b0cf7e6b81b4d452d5e912e60",
	}
	releasedListSHA256 = map[string]string{
		"nominalization":  "cfe31060efc6d165c30b1619ab574ec405312a80bfb00387efc6557911614be0",
		"intensifiers":    "65d9189ebebaf8f9cb2aad2c04be3d0ccf53c4d6782affe16bcd8078054d3c83",
		"feelings":        "37b6779b3ef8cf91260379b21d6b721bda362542a5702affd99d550879539e82",
		"hedges":          "ed666036f63f4705d2c129d3fd30e971018d50732bbef2c18081412a61ccd771",
		"katakana-terms":  "47c686e6a7a94590a97fa9e052b3dee9171dfc8a5f25da727111428cda978e2d",
		"thin-evaluation": "b2cf6a8e1fc424d1bcac15e2aa2045a98ec77a002e2830692922a8f97ee406e4",
		"renyo-nouns":     "0642b6ba40e4707af4715463d7433c0215087a8e068b00e7894b73e89365997a",
		"fact-gaps":       "c78da500dcddc76978fecd1a7a9f3318d9feabf0db78387a41662a4545e6964f",
	}
)

// ToolFiles は記録の tools の元になる読み出し。本家は道具の .py と一覧のファイルを開いてバイト列から SHA-256 とバージョンを作る。
// ja-writing は道具を持たないので、記録を作るときは Released (答えを合わせた本家の値) を使う。DirFiles は本家と同じ置き場の木を読む (突き合わせ用)。
type ToolFiles interface {
	// Tool は道具のファイルの SHA-256 と __version__ の値 (行が無ければ nil)。開けなければ error。
	Tool(name string) (sha256 string, version *string, err error)
	// List は一覧のファイル (名前に .txt を付けない) の SHA-256。無ければ fs.ErrNotExist か ENOTDIR、開けなければそれ以外の error。
	List(name string) (sha256 string, err error)
}

// Released は答えを合わせた本家 3.1.0 の値を返す ToolFiles。
type Released struct{}

// Tool は ToolFiles。
func (Released) Tool(name string) (string, *string, error) {
	sum, ok := releasedToolSHA256[name]
	if !ok {
		return "", nil, fs.ErrNotExist
	}
	version := releasedVersion
	return sum, &version, nil
}

// List は ToolFiles。
func (Released) List(name string) (string, error) {
	sum, ok := releasedListSHA256[name]
	if !ok {
		return "", fs.ErrNotExist
	}
	return sum, nil
}

// toolVersionPattern は _TOOL_VERSION (道具のファイルの __version__ の行)。
var toolVersionPattern = pyre.NewLazy(`^__version__\s*=\s*["']([^"']+)["']\s*$`, pyre.MULTILINE)

// DirFiles は scripts フォルダ (と、その隣の references/lists/) を読む ToolFiles。本家の _HERE と同じ置き場の読み方。
type DirFiles struct{ Scripts string }

// Tool は ToolFiles。
func (d DirFiles) Tool(name string) (string, *string, error) {
	data, err := os.ReadFile(visibility.PathJoin(d.Scripts, name))
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
	data, err := os.ReadFile(visibility.PathJoin(d.Scripts, "../references/lists/"+name+".txt"))
	if err != nil {
		return "", err
	}
	return SHA256(data), nil
}

// ToolVersions は _tool_versions。記録の tools (道具 4 本のバージョンの文字列と SHA-256)。
// 一覧のファイルがあれば "references/lists/<名前>.txt" のキーで {"version": null, "sha256"} を足す (開けないときは sha256 も null。ないときはキーを作らない)。
// factGaps なら一覧に fact-gaps も入れる。
func ToolVersions(files ToolFiles, factGaps bool) *pyjson.Object {
	tools := pyjson.NewObject()
	entry := func(version pyjson.Value, sha pyjson.Value) *pyjson.Object {
		o := pyjson.NewObject()
		o.Set("version", version)
		o.Set("sha256", sha)
		return o
	}
	for _, name := range ToolNames {
		sha, version, err := files.Tool(name)
		if err != nil {
			tools.Set(name, entry(nil, nil))
			continue
		}
		tools.Set(name, entry(pyjson.OptionalString(version), sha))
	}
	names := ListNames
	if factGaps {
		names = append(append([]string(nil), names...), FactGapsList)
	}
	for _, name := range names {
		key := "references/lists/" + name + ".txt"
		sha, err := files.List(name)
		switch {
		case err == nil:
			tools.Set(key, entry(nil, sha))
		case errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR):
			// 一覧がないとみなす誤り (FileNotFoundError・NotADirectoryError)。キーを作らない
		default:
			tools.Set(key, entry(nil, nil))
		}
	}
	return tools
}

// ListSHA256 は _list_sha256。tools の一覧のファイルの SHA-256 (キーがなければ "absent"。開けなかったときは nil)。
func ListSHA256(tools *pyjson.Object, key string) pyjson.Value {
	entry, ok := tools.Get(key)
	if !ok {
		return "absent"
	}
	o, isDict := entry.(*pyjson.Object)
	if !isDict {
		return "absent"
	}
	sha, _ := o.Get("sha256")
	return sha
}

// SkillInfo は _skill_info。SKILL.md の情報で、ja-writing は SKILL.md を持たないので常に found が偽。
// 本家の記録を ja-writing の verify が読むと、SKILL.md の SHA-256 を出せないので skill_changed の注記が付く。
func SkillInfo() *pyjson.Object {
	o := pyjson.NewObject()
	o.Set("found", false)
	o.Set("dir", nil)
	o.Set("skill_md_sha256", nil)
	o.Set("version", nil)
	return o
}
