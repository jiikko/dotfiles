package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// テストとベンチマークが共有する helper (issue 655 で各ファイルからまとめた)。

func must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// testEnv は本物のエンジン・コンテナ・既定の立ち絵に触れない Env (skill の dir はこの repo のもの)
func testEnv(t testing.TB) *Env {
	t.Helper()
	skill, err := filepath.Abs("../../_claude/skills/zundamon-kaisetsu")
	if err != nil {
		t.Fatal(err)
	}
	e := &Env{Stdout: io.Discard, Stderr: io.Discard}
	e.setSkillDir(skill)
	e.AssetsFaces = filepath.Join(t.TempDir(), "no-assets")
	return e
}

// writeScript は台本を一時 dir の s.json に書いてパスを返す
func writeScript(t testing.TB, raw map[string]any) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.json")
	writeScriptFile(t, path, raw)
	return path
}

// writeScriptFile は台本を path に書く (名前や置き場を決めたいとき)
func writeScriptFile(t testing.TB, path string, raw map[string]any) {
	t.Helper()
	b, err := json.Marshal(raw)
	must(t, err)
	must(t, os.WriteFile(path, b, 0o644))
}

// writeShim は dir に実行できる偽のコマンドを書いてパスを返す
func writeShim(t testing.TB, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	must(t, os.WriteFile(p, []byte(body), 0o755))
	return p
}

// prependPath は dir を PATH の先頭に足す (本物のコマンドも残す)。本物を見せたくないときは t.Setenv("PATH", dir) で置き換える
func prependPath(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
