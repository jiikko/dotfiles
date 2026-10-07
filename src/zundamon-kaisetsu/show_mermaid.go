package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// mermaid の図 (show の mermaid) は、build のときに mermaid-cli で 2 倍の PNG にしてから、画像の図 (image) と同じ検査に通す。
// 検査が見るのは描いた図の大きさと形 (小さい・細長い・大きすぎる) だけで、要素の数は見ない (12 個の格子は通り、3 個の
// 横一列は細長いので止まる。issue 645 のレビューの実測)。複雑すぎないかは SKILL.md の基準と目での確認に任せる。
const (
	// mermaidCLI は npx で取る版。mmdc が PATH にあっても使わない (版がキャッシュの鍵と食い違うため)。固定できるのは
	// mermaid-cli 本体の版までで、puppeteer などの依存・Chrome の版・フォントは鍵に入らない
	mermaidCLI      = "@mermaid-js/mermaid-cli@11.17.0"
	mermaidScale    = 2
	mermaidLinesMax = 15
)

// mermaidTimeout は 1 枚を描く時間の上限。npx の初回は mermaid-cli の取得を含む (実測 26 秒)。テストが短くする
var mermaidTimeout = 180 * time.Second

func parseMermaid(path, at string, m map[string]any) (*showData, error) {
	if err := onlyKeys(path, at+" (mermaid)", m, "type", "code", "alt"); err != nil {
		return nil, err
	}
	raw, ok := m["code"].([]any)
	if !ok || len(raw) == 0 || len(raw) > mermaidLinesMax {
		return nil, fail("%s: %s.code は 1〜%d 行の文字列のリストで書く (実際: %s。長い図は分けるか、図を使わずにセリフで説明する)",
			path, at, mermaidLinesMax, pyRepr(m["code"]))
	}
	code := make([]string, len(raw))
	blank := true
	for j, v := range raw {
		line, ok := v.(string)
		if !ok {
			return nil, fail("%s: %s.code[%d] は文字列で書く (実際: %s)", path, at, j, pyRepr(v))
		}
		code[j] = line
		blank = blank && pyStrip(line) == ""
	}
	if blank {
		return nil, fail("%s: %s.code が空行だけ", path, at)
	}
	alt, err := showField(path, at, m, "alt", true, keywordSubMax)
	if err != nil {
		return nil, err
	}
	return &showData{Type: "mermaid", Code: code, Alt: alt}, nil
}

// mermaidCachePath は図の PNG の置き場。鍵は mermaid-cli の版と記法と倍率で、wav と同じく台本の作業ディレクトリに置く。
func mermaidCachePath(scriptPath string, code []string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%s", mermaidCLI, mermaidScale, strings.Join(code, "\n"))))
	return filepath.Join(workDir(scriptPath), "mermaid", hex.EncodeToString(h[:8])+".png")
}

// embedMermaid は mermaid の図を PNG にし (キャッシュがあれば使う)、画像の図と同じ検査を通して data URI の image に置き換える。
func embedMermaid(scriptPath string, sd *showData) error {
	name := "mermaid の図 (" + sd.Alt + ")"
	png := mermaidCachePath(scriptPath, sd.Code)
	if !isFile(png) {
		if err := renderMermaid(scriptPath, name, sd.Code, png); err != nil {
			return err
		}
	}
	// 大きさの検査で止まったら、画像ファイル向けの案内 (書き出す大きさ) ではなく、図の形に合わせて案内する
	// (PNG として読めないときは 0x0 で invalid になるので、ここでは扱わず loadShowImage の読み込みのエラーに任せる)
	w, h := pngSize(png)
	if problem, _, _ := imageScaleProblem(w, h); problem != "" && problem != imageInvalid {
		hint := map[imageProblem]string{
			imageSmall: "小さい図で、表示しても読みにくい。図にせずセリフで説明する",
			imageThin:  "細長い図で、表示すると小さくなる。向き (flowchart の LR / TD) を変えるか、図にせずセリフで説明する",
			imageLarge: "大きすぎる図で、縮めると文字が読めない。要素を減らすか図を分ける",
		}[problem]
		return fail("%s: %s は描くと %dx%d px で、%s (図の箱は %dx%d)", scriptPath, name, w, h, hint, imageBoxW, imageBoxH)
	}
	uri, err := loadShowImage(scriptPath, name, png)
	if err != nil {
		return fail("%s (描いた PNG: %s。消すと次の build で描き直す)", err.Error(), png)
	}
	*sd = showData{Type: "image", Src: uri, Alt: sd.Alt}
	return nil
}

func renderMermaid(scriptPath, name string, code []string, out string) error {
	chrome := findChrome()
	if chrome == "" {
		return fail("%s: %s を描くのに Chrome が要る (環境変数 CHROME で指定できる)", scriptPath, name)
	}
	if _, err := exec.LookPath("npx"); err != nil {
		return fail("%s: %s を描くのに npx (Node) が要る (brew install node)", scriptPath, name)
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return fail("%s: %v", filepath.Dir(out), err)
	}
	td, err := os.MkdirTemp(filepath.Dir(out), ".render-")
	if err != nil {
		return fail("%s: %v", filepath.Dir(out), err)
	}
	defer func() { _ = os.RemoveAll(td) }()
	in, tmpOut, pp := filepath.Join(td, "in.mmd"), filepath.Join(td, "out.png"), filepath.Join(td, "puppeteer.json")
	ppb, err := json.Marshal(map[string]any{"executablePath": chrome})
	if err != nil {
		return err
	}
	for p, b := range map[string][]byte{in: []byte(strings.Join(code, "\n") + "\n"), pp: ppb} {
		if err := os.WriteFile(p, b, 0o644); err != nil {
			return fail("%s: %v", p, err)
		}
	}
	args := []string{"-y", mermaidCLI, "-p", pp, "-i", in, "-o", tmpOut, "-s", fmt.Sprint(mermaidScale), "-b", "white", "-q"}
	ctx, cancel := context.WithTimeout(appCtx, mermaidTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "npx", args...)
	// puppeteer に Chrome を別に取らせない (手元の Chrome を executablePath で渡す)
	cmd.Env = append(os.Environ(), "PUPPETEER_SKIP_DOWNLOAD=1")
	// npx → node (mmdc) → Chrome と孫まで起こすので、プロセスグループを分けて、止めるときはグループごと止める
	// (CommandContext の既定は直接の子だけを殺し、孫が親を失って残る。issue 645 のレビューの実測)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	var outb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &outb, &outb
	// Cancel は npx がまだ回収されていない (番号が他に使われていない) ときにだけ走る。描き終えた後にグループを止める処理は
	// 置かない: npx の回収後は同じ番号が別のプロセスグループに使われうる (mmdc は描き終えると Chrome を自分で閉じる)
	err = cmd.Run()
	if e := interruptedErr(); e != nil {
		return e
	}
	if ctx.Err() != nil {
		return fail("%s: %s を描くのが %.0f 秒で終わらない", scriptPath, name, mermaidTimeout.Seconds())
	}
	if err != nil || !isFile(tmpOut) {
		return fail("%s: %s を描けない (npx, %v): %s", scriptPath, name, err, lastRunes(strings.TrimSpace(outb.String()), 400))
	}
	// できあがった PNG だけをキャッシュの名前で公開する (途中で止まった絵をキャッシュとして読まない)
	if err := os.Rename(tmpOut, out); err != nil {
		return fail("%s: %v", out, err)
	}
	return nil
}
