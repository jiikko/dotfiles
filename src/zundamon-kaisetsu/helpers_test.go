package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"zundamon-kaisetsu/internal/app"
	"zundamon-kaisetsu/internal/build"
)

// テストとベンチマークが共有する helper (issue 655 で各ファイルからまとめた)。

func must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// testEnv は本物のエンジン・コンテナ・既定の立ち絵に触れない Env (skill の dir はこの repo のもの)
func testEnv(t testing.TB) *app.Env {
	t.Helper()
	skill, err := filepath.Abs("../../_claude/skills/zundamon-kaisetsu")
	if err != nil {
		t.Fatal(err)
	}
	e := &app.Env{Stdout: io.Discard, Stderr: io.Discard}
	e.SetSkillDir(skill)
	e.AssetsFaces = filepath.Join(t.TempDir(), "no-assets")
	e.MeasureLabels = fakeLabelMeasure(nil) // 本物の Chrome を起こさない (ラベルの改行は全部問題なしと答える)
	return e
}

// playerDataFromHTML は build が書いた HTML に埋め込んだプレイヤーのデータを取り出す
func playerDataFromHTML(html string) (build.PlayerData, error) {
	const open = `<script type="application/json" id="data">`
	var d build.PlayerData
	start := strings.Index(html, open)
	if start < 0 {
		return d, errors.New("データの script が見つからない")
	}
	end := strings.Index(html[start:], "</script>")
	if end < 0 {
		return d, errors.New("データの script が閉じていない")
	}
	err := json.Unmarshal([]byte(html[start+len(open):start+end]), &d)
	return d, err
}

// fakeLabelMeasure は player.html の計測モードの代わり。測るはずのラベル (build.ExpectedLabels) を全部「問題なし」で返し、
// edit があれば返す前に書き換える (問題のある計測を演じる)
func fakeLabelMeasure(edit func(d *build.PlayerData, ms []build.LabelMeasure) []build.LabelMeasure) func(string) ([]byte, error) {
	return func(page string) ([]byte, error) {
		b, err := os.ReadFile(page)
		if err != nil {
			return nil, err
		}
		d, err := playerDataFromHTML(string(b))
		if err != nil {
			return nil, err
		}
		var ms []build.LabelMeasure
		for _, e := range build.ExpectedLabels(&d) {
			m := build.LabelMeasure{Key: e.Key, Kind: e.Kind, Text: e.Text, Clip: []int{0, 0}}
			if e.Kind != "box" {
				m.Lines, m.Glen, m.Breaks = []string{e.Text}, []int{utf8.RuneCountInString(e.Text)}, []bool{}
			}
			ms = append(ms, m)
		}
		if edit != nil {
			ms = edit(&d, ms)
		}
		return json.Marshal(map[string]any{"labels": ms})
	}
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
