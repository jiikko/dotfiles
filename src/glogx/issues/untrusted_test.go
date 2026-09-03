package issues

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// issue ファイルは第三者が PR で足せる = 完全に信頼できない入力。この 3 本は
// 「無害化・symlink 拒否が実際にこの経路を通っているか」の配線を固定する回帰テスト
// (無害化そのものの仕様は termsafe のテストが持つ)。

const (
	esc  = "\x1b"
	bel  = "\a"
	osc8 = "\u009d" // 8bit OSC (C1)。7bit の ESC] と同じ制御機能
	st8  = "\u009c" // 8bit ST (C1)
)

// hasTerminalControl は「端末が制御として解釈しうる文字が残っているか」。
//
// 🚨 ESC と BEL だけを見る判定にしないこと: それだと 8bit の CSI (U+009B) / OSC (U+009D) を
// 原理的に見逃し、「ESC と BEL だけ落とす」実装がテストを全部 green で通ってしまう
// (敵対的レビュー 2026-08-05 が実際にこの盲点を突いた)。許可した文字だけが残っているか、の
// allowlist 側で判定する。
func hasTerminalControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return true
		}
	}
	return false
}

// 本文・H1 の端末制御シーケンスは表示に出る前に落ちる。
// 落ちないと、issue 一覧を開いただけで端末のタイトル書き換え・画面消去・OSC52 による
// クリップボード書き込みが発火する。
func TestIssueBodyAndTitleAreSanitized(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "issues")
	body := "# 001 " + esc + "]0;pwned" + bel + "evil title\n\n" +
		"本文に " + esc + "[2J 画面消去と " + esc + "]52;c;aGVsbG8=" + bel + " OSC52\n"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "001-feat-x.md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	list, _ := Scan([]string{dir})
	if len(list) != 1 {
		t.Fatalf("issue が拾えていない: %d 件", len(list))
	}
	iss := list[0]
	if err := iss.LoadMeta(); err != nil {
		t.Fatal(err)
	}
	if hasTerminalControl(iss.Title) {
		t.Errorf("H1 に制御シーケンスが残った: %q", iss.Title)
	}
	if strings.Contains(iss.Display(), "pwned") {
		t.Errorf("OSC の中身が一覧タイトルに残った: %q", iss.Display())
	}
	b, err := iss.ReadBody()
	if err != nil {
		t.Fatal(err)
	}
	lines := b.Lines(60, false)
	// 🚨 行ごとに見る: 連結してから検査すると区切りの改行自体を制御文字として拾ってしまう
	for i, ln := range lines {
		if hasTerminalControl(ln) {
			t.Errorf("本文 %d 行目に制御シーケンスが残った: %q", i, ln)
		}
	}
	// 無害化しても本文が 1 行に潰れない (termsafe は改行も落とすので、分割の後に掛ける契約)
	if len(lines) < 2 {
		t.Errorf("本文が 1 行に潰れた: %q", lines)
	}
}

// 8bit の C1 制御文字 (U+009B = CSI / U+009D = OSC) も落ちる。
//
// 🚨 最初の実装はここが素通しだった。ESC と BEL しか見ない実装・テストの組み合わせだと
// 「無害化している」ように見えて 8bit 版が丸ごと通る (敵対的レビュー 2026-08-05)。
// git のブランチ名は ASCII 制御文字しか禁じられていないので、C1 は実際に外部から入ってくる。
func TestIssueC1ControlCharsAreDropped(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "issues")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "# 001 " + osc8 + "0;pwned" + st8 + "innocent title\n\n本文に " + osc8 + "52;c;aGVsbG8=" + st8 + " 8bit OSC52\n"
	if err := os.WriteFile(filepath.Join(dir, "001-feat-x.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	list, _ := Scan([]string{dir})
	if len(list) != 1 {
		t.Fatalf("issue が拾えていない: %d 件", len(list))
	}
	iss := list[0]
	if err := iss.LoadMeta(); err != nil {
		t.Fatal(err)
	}
	if hasTerminalControl(iss.Title) {
		t.Errorf("H1 に 8bit 制御シーケンスが残った: %q", iss.Title)
	}
	if strings.Contains(iss.Title, "pwned") {
		t.Errorf("8bit OSC の中身が残った: %q", iss.Title)
	}
	b, err := iss.ReadBody()
	if err != nil {
		t.Fatal(err)
	}
	for i, ln := range b.Lines(60, false) {
		if hasTerminalControl(ln) {
			t.Errorf("本文 %d 行目に 8bit 制御シーケンスが残った: %q", i, ln)
		}
	}
}

// 🚨 回帰防止: 無害化はタブの桁揃えを壊してはいけない。
//
// 本文のタブは expandTabs が「タブストップ揃え」(4 の倍数の桁へ送る) で展開する。無害化の側で
// タブを一律 4 スペースへ潰すと、行頭以外のタブで桁がずれる (`ab<TAB>c` が `ab  c` ではなく
// `ab    c` になる)。実際にこの実装を最初に入れたとき壊した箇所なので、経路ごと固定する。
func TestBodyKeepsTabStopAlignment(t *testing.T) {
	got := renderLines("```\nab\tc\n```\n", 40, false)
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "ab  c") {
		t.Errorf("タブストップ揃えが崩れた (一律展開になっている): %q", joined)
	}
	if strings.Contains(joined, "\t") {
		t.Errorf("タブが展開されずに残った (幅計算とずれる): %q", joined)
	}
}

// ファイル名の制御シーケンスも一覧の表示文字列には出ない。ファイル名は POSIX が / と NUL 以外の
// 任意バイトを許すので、ESC 入りの名前で PR を出せる。🚨 同一性 (Path) は実物のまま残す
// (無害化した名前で開こうとするとファイルを見失う)。
func TestIssueFilenameIsSanitizedForDisplayOnly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "issues")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "002-feat-" + esc + "]0;pwned" + bel + "x.md"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("no h1 here\n"), 0o644); err != nil {
		t.Skipf("この環境では制御文字入りのファイル名を作れない: %v", err)
	}
	list, _ := Scan([]string{dir})
	if len(list) != 1 {
		t.Fatalf("issue が拾えていない: %d 件", len(list))
	}
	iss := list[0]
	if hasTerminalControl(iss.Display()) {
		t.Errorf("一覧の表示文字列に制御シーケンスが残った: %q", iss.Display())
	}
	if iss.Path != path {
		t.Errorf("同一性のパスが書き換わった (ファイルを開けなくなる): %q", iss.Path)
	}
	if _, err := os.Stat(iss.Path); err != nil {
		t.Errorf("保持したパスで実ファイルを開けない: %v", err)
	}
}

// symlink は issue として拾わない。拾うと ReadBody がリンク先を辿り、
// `issues/999-innocuous.md -> ~/.ssh/id_rsa` を仕込んだブランチを checkout しただけで
// リンク先の中身が本文として画面に出る。
func TestScanIgnoresSymlinks(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "issues")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(secret, []byte("SECRET-CONTENT\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "999-innocuous.md")); err != nil {
		t.Skipf("この環境では symlink を作れない: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "001-real.md"), []byte("# 001 real\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	list, _ := Scan([]string{dir})
	for _, iss := range list {
		if strings.Contains(iss.Rel, "999") {
			t.Fatalf("symlink を issue として拾った (リンク先の中身が本文として出る): %q", iss.Rel)
		}
	}
	if len(list) != 1 {
		t.Fatalf("実ファイルまで落とした: %d 件", len(list))
	}
	// 走査対象になるかの判定も symlink だけの issues ディレクトリを拾わない
	only := filepath.Join(root, "onlylink", "issues")
	if err := os.MkdirAll(only, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(only, "001-link.md")); err != nil {
		t.Fatal(err)
	}
	for _, d := range FindDirs(root) {
		if d == only {
			t.Fatal("symlink しか無いディレクトリを issue ディレクトリとして拾った")
		}
	}
}

// 「同じ basename が複数の状態ディレクトリにある」警告は一覧のヘッダーへそのまま描かれる
// 表示 sink。警告文へ埋める Rel は同一性用の生パスなので、ここで無害化しないと漏れる
// (無害化した本文・ファイル名の隣で、警告だけが素通しだった)。
func TestScanWarningsAreSanitized(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "issues")
	done := filepath.Join(dir, "done")
	if err := os.MkdirAll(done, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "001-feat-" + esc + "]0;pwned" + bel + "x.md"
	for _, p := range []string{filepath.Join(dir, name), filepath.Join(done, name)} {
		if err := os.WriteFile(p, []byte("# 001 x\n"), 0o644); err != nil {
			t.Skipf("この環境では制御文字入りのファイル名を作れない: %v", err)
		}
	}
	_, warns := Scan([]string{dir})
	if len(warns) == 0 {
		t.Fatal("重複の警告が出ていない (前提が崩れた)")
	}
	for _, w := range warns {
		if hasTerminalControl(w) {
			t.Errorf("警告文に制御シーケンスが残った: %q", w)
		}
		if strings.Contains(w, "pwned") {
			t.Errorf("OSC の中身が警告文に残った: %q", w)
		}
	}
}

// issues ディレクトリ「自体」が symlink なら走査対象にしない。
//
// ファイル単位の symlink 拒否 (isIssueFile) だけでは塞げない: git は mode 120000 で
// ディレクトリ symlink を表現できるので、`issues -> /Users/victim/Documents` を 1 本足した
// PR を checkout すると repo 外の .md が一覧・本文として読める (実機で再現した経路)。
func TestFindDirsIgnoresSymlinkedIssuesDir(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "001-private.md"), []byte("# 秘密\n\nSECRET\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, "issues")); err != nil {
		t.Skipf("この環境では symlink を作れない: %v", err)
	}
	if dirs := FindDirs(repo); len(dirs) != 0 {
		t.Fatalf("symlink の issues ディレクトリを拾った (repo 外が読める): %q", dirs)
	}
	// サブディレクトリ経由 (root/<sub>/issues) も同じ
	sub := filepath.Join(root, "repo2", "app")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(sub, "issues")); err != nil {
		t.Fatal(err)
	}
	if dirs := FindDirs(filepath.Join(root, "repo2")); len(dirs) != 0 {
		t.Fatalf("サブディレクトリ経由で symlink の issues を拾った: %q", dirs)
	}
}

// 本文から拾う URL に制御シーケンスを混ぜられない (URLs は整形経路を通らず生ソースを見るので、
// 本文側の無害化では守れない)。
func TestURLsStopAtControlChars(t *testing.T) {
	b := NewBody("参考: https://example.com/ok" + esc + "]0;pwned" + bel + "tail\n")
	urls := b.URLs()
	if len(urls) != 1 {
		t.Fatalf("URL の数が想定外: %q", urls)
	}
	if hasTerminalControl(urls[0]) {
		t.Errorf("URL に制御シーケンスが混ざった: %q", urls[0])
	}
	if urls[0] != "https://example.com/ok" {
		t.Errorf("URL が制御文字の手前で切れていない: %q", urls[0])
	}
}

// C1 (U+009B CSI / U+009D OSC) も終端にする。端末によっては ESC[ / ESC] と同義に解釈されるので、
// 負クラスから漏れると URL ピッカーの行描画 (url_picker.go) まで生で到達する。
// hasTerminalControl は C1 を制御文字と定義しているので、漏れは自己矛盾でもある (実測 2026-08-21)。
func TestURLsStopAtC1ControlChars(t *testing.T) {
	for _, tc := range []struct {
		name string
		c1   string
	}{
		{"CSI (U+009B)", "\u009b"},
		{"OSC (U+009D)", "\u009d"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBody("参考: https://example.com/ok" + tc.c1 + "0;pwned" + bel + "tail\n")
			urls := b.URLs()
			if len(urls) != 1 {
				t.Fatalf("URL の数が想定外: %q", urls)
			}
			if hasTerminalControl(urls[0]) {
				t.Errorf("URL に C1 が混ざった: %q", urls[0])
			}
			if urls[0] != "https://example.com/ok" {
				t.Errorf("URL が C1 の手前で切れていない: %q", urls[0])
			}
		})
	}
}
