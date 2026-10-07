package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeMermaid は PATH の先頭に偽の npx を置く。偽物は引数と PUPPETEER_SKIP_DOWNLOAD を log に 1 行ずつ書き、mode に従って動く:
// ok なら -o の先へ pngPath を写す / fail なら Parse error で落ちる / hang なら孫 (sleep) を起こして待ち続ける (孫の pid を
// log と同じ場所の grandchild.pid に書く)。🚨 孫は setsid で別のセッション (= 別のプロセスグループ) に置く: 本物の puppeteer は
// Chrome を detached で起こす (@puppeteer/browsers の launch.js: `detached ??= platform !== 'win32'`)。同じグループの孫では、
// グループ宛ての kill だけで止まって見えてしまう (issue 649)。CHROME も偽の実行ファイルにする (本物の Chrome は起こさない)。返り値は log のパス。
func fakeMermaid(t *testing.T, pngPath, mode string) string {
	t.Helper()
	bin := t.TempDir()
	log := filepath.Join(bin, "calls.log")
	script := `#!/bin/sh
echo "$* skip=$PUPPETEER_SKIP_DOWNLOAD" >> "` + log + `"
case "` + mode + `" in
fail) echo "Parse error on line 2" >&2; exit 1 ;;
hang) perl -e 'use POSIX; POSIX::setsid(); open(my $f, ">", $ARGV[0]); print $f "$$\n"; close $f; exec "sleep", "300"' "` + filepath.Join(bin, "grandchild.pid") + `" & wait; exit 0 ;;
esac
while [ $# -gt 0 ]; do
  if [ "$1" = "-o" ]; then cp "` + pngPath + `" "$2"; exit $?; fi
  shift
done
exit 2
`
	must(t, os.WriteFile(filepath.Join(bin, "npx"), []byte(script), 0o755))
	chrome := filepath.Join(bin, "chrome")
	must(t, os.WriteFile(chrome, []byte("#!/bin/sh\nexit 0\n"), 0o755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CHROME", chrome)
	return log
}

func mermaidShow(code ...any) map[string]any {
	return map[string]any{"type": "mermaid", "code": code, "alt": "B は A を待つ"}
}

func calls(t *testing.T, log string) []string {
	t.Helper()
	b, err := os.ReadFile(log)
	if os.IsNotExist(err) {
		return nil
	}
	must(t, err)
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func TestRejectBadMermaidShow(t *testing.T) {
	many := make([]any, mermaidLinesMax+1)
	for i := range many {
		many[i] = "A --> B"
	}
	for _, tc := range []struct {
		show any
		want string
	}{
		{map[string]any{"type": "mermaid", "code": "A --> B", "alt": "a"}, "show.code は 1〜15 行"},
		{mermaidShow(), "show.code は 1〜15 行"},
		{mermaidShow(many...), "show.code は 1〜15 行"},
		{mermaidShow("flowchart LR", 1), "show.code[1] は文字列"},
		{mermaidShow("", " "), "show.code が空行だけ"},
		{map[string]any{"type": "mermaid", "code": []any{"A --> B"}}, "show.alt は空でない文字列"},
		{withKey(mermaidShow("A --> B"), "alt", strings.Repeat("説", 41)), "show.alt は 40 字まで"},
		{withKey(mermaidShow("A --> B"), "theme", "dark"), "show (mermaid) に書けるのは type/code/alt だけ"},
	} {
		path := writeScript(t, map[string]any{"lines": []any{
			map[string]any{"who": "metan", "text": "a"},
			map[string]any{"who": "metan", "text": "b", "show": tc.show},
		}})
		_, err := loadScript(path, testEnv(t))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: %q を含む拒否にならなかった (%v)", tc.show, tc.want, err)
		}
	}
}

func TestEmbedMermaid(t *testing.T) {
	src := t.TempDir()
	writeImage(t, src, "ok.png", 800, 500, false)
	log := fakeMermaid(t, filepath.Join(src, "ok.png"), "ok")
	var scriptDir string
	build := func(code ...any) (*PlayerData, error) {
		return buildWithShows(t, func(dir string, lines []any) {
			scriptDir = dir
			lines[1].(map[string]any)["show"] = mermaidShow(code...)
		})
	}
	data, err := build("flowchart LR", "  A --> B")
	if err != nil {
		t.Fatal(err)
	}
	if sd := data.Shows[0]; sd.Type != "image" || !strings.HasPrefix(sd.Src, "data:image/png;base64,") || sd.Code != nil {
		t.Errorf("mermaid の図が data URI の image になっていない: type=%s code=%v src=%.40s", sd.Type, sd.Code, sd.Src)
	}
	c := calls(t, log)
	// 版は定数から作らずに形で見る (定数ごと版の固定を外す退行を、期待値が一緒に動いて見逃さない)
	if len(c) != 1 || !regexp.MustCompile(`^-y @mermaid-js/mermaid-cli@\d+\.\d+\.\d+ `).MatchString(c[0]) || !strings.Contains(c[0], " -s 2 ") ||
		!strings.Contains(c[0], "skip=1") || !strings.Contains(c[0], " -p ") || !strings.Contains(c[0], " -i ") {
		t.Errorf("npx の呼び方が違う (版を固定した mermaid-cli・倍率 2・puppeteer の設定・入力・Chrome を取らせない): %v", c)
	}
	cache := mermaidCachePath(filepath.Join(scriptDir, "script.json"), []string{"flowchart LR", "  A --> B"})
	if !isFile(cache) {
		t.Errorf("描いた PNG がキャッシュに無い: %s", cache)
	}
	if ents, _ := os.ReadDir(filepath.Dir(cache)); len(ents) != 1 {
		t.Errorf("キャッシュのディレクトリに PNG 以外が残っている: %v", ents)
	}
}

// TestMermaidCacheReused は、同じ記法の図は描き直さず、記法が変われば描き直すことを確かめる (同じ台本のディレクトリで 2 回 build する)。
func TestMermaidCacheReused(t *testing.T) {
	src := t.TempDir()
	writeImage(t, src, "ok.png", 800, 500, false)
	log := fakeMermaid(t, filepath.Join(src, "ok.png"), "ok")
	env := testEnv(t)
	dir := t.TempDir()
	copyTree(t, filepath.Join("testdata", "build"), dir)
	path := resolvePath(filepath.Join(dir, "script.json"))
	assembleWith := func(code ...string) {
		t.Helper()
		s, err := loadScript(path, env)
		must(t, err)
		list := make([]any, len(code))
		for i, c := range code {
			list[i] = c
		}
		s.Lines[1]["show"] = mermaidShow(list...)
		_, _, err = assemble(s, env)
		must(t, err)
	}
	assembleWith("flowchart LR", "  A --> B")
	assembleWith("flowchart LR", "  A --> B")
	if n := len(calls(t, log)); n != 1 {
		t.Errorf("同じ記法の図を %d 回描いた (キャッシュを使っていない)", n)
	}
	assembleWith("flowchart LR", "  A --> C")
	if n := len(calls(t, log)); n != 2 {
		t.Errorf("記法を変えた後の mmdc の呼び出しが合計 %d 回 (want 2)", n)
	}
}

func TestEmbedMermaidFailures(t *testing.T) {
	src := t.TempDir()
	writeImage(t, src, "small.png", 300, 200, false)
	writeImage(t, src, "thin.png", 1000, 60, false)
	writeImage(t, src, "large.png", 2000, 600, false)
	writeImage(t, src, "ok.png", 800, 500, false)
	for _, tc := range []struct {
		name  string
		setup func()
		want  string
	}{
		{"小さい図", func() { fakeMermaid(t, filepath.Join(src, "small.png"), "ok") }, "小さい図で"},
		{"細長い図", func() { fakeMermaid(t, filepath.Join(src, "thin.png"), "ok") }, "向き (flowchart の LR / TD) を変える"},
		{"大きすぎる図", func() { fakeMermaid(t, filepath.Join(src, "large.png"), "ok") }, "要素を減らすか図を分ける"},
		{"記法の誤り", func() { fakeMermaid(t, filepath.Join(src, "ok.png"), "fail") }, "Parse error on line 2"},
		{"道具が無い", func() {
			empty := t.TempDir()
			t.Setenv("PATH", empty)
			chrome := filepath.Join(empty, "chrome")
			must(t, os.WriteFile(chrome, []byte("#!/bin/sh\n"), 0o755))
			t.Setenv("CHROME", chrome)
		}, "npx (Node) が要る"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup()
			var scriptDir string
			_, err := buildWithShows(t, func(dir string, lines []any) {
				scriptDir = dir
				lines[1].(map[string]any)["show"] = mermaidShow("flowchart LR", "  A --> B")
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%q で止まるはず (%v)", tc.want, err)
			}
			if tc.name == "記法の誤り" {
				ents, _ := os.ReadDir(filepath.Join(scriptDir, "script.work", "mermaid"))
				if len(ents) != 0 {
					t.Errorf("描けなかったのにキャッシュに残っている: %v", ents)
				}
			}
		})
	}
}

// TestMermaidCancelKillsGrandchild は、描いている途中で中断したとき、npx の先で起きた孫のプロセスまで止めることを確かめる。
// 時間切れも同じ cmd.Cancel を通る。偽物が孫の pid を書いたのを見てから中断するので、時間に頼らない。
func TestMermaidCancelKillsGrandchild(t *testing.T) {
	log := fakeMermaid(t, "", "hang")
	cancel := withAppCtx(t)
	env := testEnv(t)
	dir := t.TempDir()
	copyTree(t, filepath.Join("testdata", "build"), dir)
	s, err := loadScript(resolvePath(filepath.Join(dir, "script.json")), env)
	must(t, err)
	s.Lines[1]["show"] = mermaidShow("flowchart LR", "  A --> B")
	done := make(chan error, 1)
	go func() { _, _, err := assemble(s, env); done <- err }()
	pidFile := filepath.Join(filepath.Dir(log), "grandchild.pid")
	waitUntil(t, "偽物が孫を起こす", func() bool {
		b, err := os.ReadFile(pidFile)
		return err == nil && strings.TrimSpace(string(b)) != ""
	})
	b, err := os.ReadFile(pidFile)
	must(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	must(t, err)
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	cancel()
	if err := <-done; err == nil {
		t.Fatal("中断したのに組み立てが成功した")
	}
	waitUntil(t, "孫のプロセスが居なくなる", func() bool { return syscall.Kill(pid, 0) != nil })
}

// TestMermaidTimeoutMessage は、描くのが時間切れになったら、その旨で止まることを確かめる。
func TestMermaidTimeoutMessage(t *testing.T) {
	log := fakeMermaid(t, "", "hang")
	old := mermaidTimeout
	mermaidTimeout = 2 * time.Second // 偽物が孫の pid を書き終えるまでの余裕 (書く前に止まると、残らないことを確かめられない)
	t.Cleanup(func() { mermaidTimeout = old })
	done := make(chan error, 1)
	go func() {
		_, err := buildWithShows(t, func(_ string, lines []any) {
			lines[1].(map[string]any)["show"] = mermaidShow("flowchart LR", "  A --> B")
		})
		done <- err
	}()
	// 時間切れの前に孫が起きたことを確かめる (起きる前に止まると、残らないことを確かめられない)
	pidFile := filepath.Join(filepath.Dir(log), "grandchild.pid")
	waitUntil(t, "偽物が孫を起こす", func() bool {
		b, err := os.ReadFile(pidFile)
		return err == nil && strings.HasSuffix(string(b), "\n")
	})
	if err := <-done; err == nil || !strings.Contains(err.Error(), "終わらない") {
		t.Fatalf("時間切れで止まるはず (%v)", err)
	}
	// 時間切れでも、別のグループに居る孫 (本物では puppeteer の Chrome) が残らない (issue 649)
	b, err := os.ReadFile(pidFile)
	must(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	must(t, err)
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	waitUntil(t, "時間切れの後に孫のプロセスが居なくなる", func() bool { return syscall.Kill(pid, 0) != nil })
}

// TestMermaidBrokenCacheHint は、キャッシュに壊れた PNG があるとき、そのパスと描き直し方を案内することを確かめる。
func TestMermaidBrokenCacheHint(t *testing.T) {
	fakeMermaid(t, "", "fail") // 描き直しには進まない (キャッシュがある) ので呼ばれない
	_, err := buildWithShows(t, func(dir string, lines []any) {
		code := []string{"flowchart LR", "  A --> B"}
		cache := mermaidCachePath(filepath.Join(dir, "script.json"), code)
		must(t, os.MkdirAll(filepath.Dir(cache), 0o755))
		must(t, os.WriteFile(cache, []byte("\x89PNG broken"), 0o644))
		lines[1].(map[string]any)["show"] = mermaidShow(code[0], code[1])
	})
	if err == nil || !strings.Contains(err.Error(), "消すと次の build で描き直す") {
		t.Fatalf("壊れたキャッシュの案内が無い: %v", err)
	}
}
