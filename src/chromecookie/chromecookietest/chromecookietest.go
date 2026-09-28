// Package chromecookietest は chromecookie を使う側のテストが、Chrome と同じ形の Cookie DB と
// 暗号文を作るための道具（本物の Chrome・Keychain には触れない）。
//
// 🚨 chromecookie を import しないこと。暗号化をここで独立に実装しているので、chromecookie の
// 復号に対する正解役（オラクル）として働く。chromecookie の関数を呼んで作ると、復号の誤りを
// 暗号化側が同じように写して、テストが何も検出しなくなる（chromecookie 自身の内部テストから
// import されると循環にもなる）。
package chromecookietest

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/sha1"
	"crypto/sha256"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// Cookie は Cookie DB の cookies テーブルの 1 行。Enc が空なら Value が平文の値になる。
type Cookie struct {
	Host, Name, Value string
	Enc               []byte
}

// Encrypt は password（Keychain の "Chrome Safe Storage" に相当）で plain を Chrome と同じ形式
// （v10 + AES-128-CBC + IV=0x20*16 + PKCS7。鍵は PBKDF2-SHA1 1003 回 / saltysalt / 16 バイト）に暗号化する。
// hostKey が空でなければ、meta.version>=24 の形（先頭に SHA256(hostKey) の 32 バイト）にする。
func Encrypt(t testing.TB, password, plain, hostKey string) []byte {
	t.Helper()
	key, err := pbkdf2.Key(sha1.New, password, []byte("saltysalt"), 1003, 16)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte(plain)
	if hostKey != "" {
		h := sha256.Sum256([]byte(hostKey))
		data = append(h[:], data...)
	}
	pad := aes.BlockSize - len(data)%aes.BlockSize
	for range pad {
		data = append(data, byte(pad))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]byte, len(data))
	cipher.NewCBCEncrypter(block, []byte("                ")).CryptBlocks(out, data)
	return append([]byte("v10"), out...)
}

// ProfileDir は home を HOME としたときの Chrome のプロファイルディレクトリを返す（作らない）。
func ProfileDir(home, profile string) string {
	return filepath.Join(home, "Library", "Application Support", "Google", "Chrome", profile)
}

// WriteCookieDB は home 配下の profile に、Chrome と同じ列を持つ Cookie DB（Network/Cookies）を作って
// そのパスを返す。metaVersion は meta テーブルの version（"24" 以上ならホストハッシュ付きの形）。
func WriteCookieDB(t testing.TB, home, profile, metaVersion string, cookies []Cookie) string {
	t.Helper()
	dir := filepath.Join(ProfileDir(home, profile), "Network")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "Cookies")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	for _, q := range []string{
		`CREATE TABLE meta (key TEXT, value TEXT)`,
		`CREATE TABLE cookies (host_key TEXT, name TEXT, value TEXT, encrypted_value BLOB)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO meta VALUES ('version', ?)`, metaVersion); err != nil {
		t.Fatal(err)
	}
	for _, c := range cookies {
		if _, err := db.Exec(`INSERT INTO cookies VALUES (?, ?, ?, ?)`, c.Host, c.Name, c.Value, c.Enc); err != nil {
			t.Fatal(err)
		}
	}
	return path
}
