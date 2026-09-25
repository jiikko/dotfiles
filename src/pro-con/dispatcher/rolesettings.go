package dispatcher

// 役割ごとに claude へ --settings で渡す JSON (431)。どの役割も --setting-sources project,local と組で使う
// (ユーザーの settings.json の hook・許可・~/.claude/rules はそれで外れる。2.1.282 の /context で実測)。
// ここで外すのは --setting-sources では外れない残り:
//   - auto memory (~/.claude/projects/<repo>/memory/MEMORY.md)。どの役割も外す
//   - ~/.claude/CLAUDE.md。cwd の祖先 (家) の .claude/CLAUDE.md として Project 扱いで拾われる。
//     PG・PM は残す (git の禁止操作・レビュー方針が要る)。haiku (要約役・btw) は外す
// 🚨 hook・許可をここへ足さない (--setting-sources で外した意味が崩れる)。PG に ~/.claude/rules の一部を戻すかは別の判断 (431)

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// sessionSettings は PG・PM の --settings。language はユーザーの settings.json (userSettings) から写す (461)。
func sessionSettings(userSettings string) string {
	s := map[string]any{"autoMemoryEnabled": false}
	if lang := userLanguage(userSettings); lang != "" {
		s["language"] = lang
	}
	return mustJSON(s)
}

// HaikuSettings は haiku (要約役・btw) の --settings。home は ~/.claude/CLAUDE.md を指すための家 ("" なら CLAUDE.md は外せない)。
func HaikuSettings(home string) string {
	s := map[string]any{"autoMemoryEnabled": false}
	if home != "" {
		s["claudeMdExcludes"] = []string{filepath.Join(home, ".claude", "CLAUDE.md")}
	}
	return mustJSON(s)
}

// userLanguage は path の settings.json の language。読めない・壊れている・無い / 文字列でない / 空なら "" (起動は止めない)。
func userLanguage(path string) string {
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var s struct {
		Language any `json:"language"`
	}
	if json.Unmarshal(b, &s) != nil {
		return ""
	}
	lang, ok := s.Language.(string)
	if !ok || strings.TrimSpace(lang) == "" {
		return ""
	}
	return lang
}

// mustJSON は map[string]any を JSON にする (中身は文字列・真偽値・文字列の配列だけなので失敗しない)。
func mustJSON(v map[string]any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
