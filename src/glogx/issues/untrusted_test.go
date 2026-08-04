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
	esc = "\x1b"
	bel = "\a"
)

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
	if strings.ContainsAny(iss.Title, esc+bel) {
		t.Errorf("H1 に制御シーケンスが残った: %q", iss.Title)
	}
	if strings.Contains(iss.Display(), "pwned") {
		t.Errorf("OSC の中身が一覧タイトルに残った: %q", iss.Display())
	}
	b, err := iss.ReadBody()
	if err != nil {
		t.Fatal(err)
	}
	rendered := strings.Join(b.Lines(60, false), "\n")
	if strings.ContainsAny(rendered, esc+bel) {
		t.Errorf("本文に制御シーケンスが残った: %q", rendered)
	}
	// 無害化しても本文が 1 行に潰れない (termsafe は改行も落とすので、分割の後に掛ける契約)
	if len(b.Lines(60, false)) < 2 {
		t.Errorf("本文が 1 行に潰れた: %q", rendered)
	}
}

// ファイル名の制御シーケンスも一覧の表示文字列には出ない。ファイル名は POSIX が / と NUL 以外の
// 任意バイトを許すので、ESC 入りの名前で PR を出せる。⚠️ 同一性 (Path) は実物のまま残す
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
	if strings.ContainsAny(iss.Display(), esc+bel) {
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

// 本文から拾う URL に制御シーケンスを混ぜられない (URLs は整形経路を通らず生ソースを見るので、
// 本文側の無害化では守れない)。
func TestURLsStopAtControlChars(t *testing.T) {
	b := NewBody("参考: https://example.com/ok" + esc + "]0;pwned" + bel + "tail\n")
	urls := b.URLs()
	if len(urls) != 1 {
		t.Fatalf("URL の数が想定外: %q", urls)
	}
	if strings.ContainsAny(urls[0], esc+bel) {
		t.Errorf("URL に制御シーケンスが混ざった: %q", urls[0])
	}
	if urls[0] != "https://example.com/ok" {
		t.Errorf("URL が制御文字の手前で切れていない: %q", urls[0])
	}
}
