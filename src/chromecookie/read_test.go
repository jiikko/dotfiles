package chromecookie

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// ReadCookies のエラーの分類（即停止 / 黙って skip / 記録して skip）を、実物の sqlite の
// Cookie DB を HOME の下に作って固定する。Keychain は通さない（password を直接渡す）。
// 分類の意味は package doc（cookie.go）を参照。

const testPassword = "testpassword"

type fakeCookie struct {
	host, name, value string
	enc               []byte
}

// writeCookieDB は Chrome と同じ列を持つ Cookie DB を profile の Network/Cookies に作る。
func writeCookieDB(t *testing.T, home, profile, version string, cookies []fakeCookie) string {
	t.Helper()
	dir := filepath.Join(chromeProfileForTest(t, home, profile), "Network")
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
		`INSERT INTO meta VALUES ('version', '` + version + `')`,
		`CREATE TABLE cookies (host_key TEXT, name TEXT, value TEXT, encrypted_value BLOB)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range cookies {
		if _, err := db.Exec(`INSERT INTO cookies VALUES (?, ?, ?, ?)`, c.host, c.name, c.value, c.enc); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func encWithPassword(t *testing.T, password, plain, hostKey string) []byte {
	t.Helper()
	key, err := deriveKey([]byte(password))
	if err != nil {
		t.Fatal(err)
	}
	return encryptForTest(t, key, "v10", plain, hostKey)
}

// assertNoLeftovers は作業領域に一時コピーが残っていないことを見る（成功・失敗どちらの経路の後でも）。
func assertNoLeftovers(t *testing.T) {
	t.Helper()
	entries, err := os.ReadDir(testWS.tempRoot())
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("一時コピーが残っている: %d 件", len(entries))
	}
}

func TestReadCookiesClassification(t *testing.T) {
	t.Run("復号して返し、names で絞れる（v24 のホストハッシュも照合する）", func(t *testing.T) {
		isolateTemp(t)
		home := os.Getenv("HOME")
		writeCookieDB(t, home, "P", "24", []fakeCookie{
			{host: ".example.com", name: "sid", enc: encWithPassword(t, testPassword, "SECRET", ".example.com")},
			{host: ".example.com", name: "other", value: "plain"},
		})
		res, err := testWS.ReadCookies("P", []byte(testPassword), "sid")
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Cookies) != 1 || res.Cookies[0].Name != "sid" || res.Cookies[0].Value != "SECRET" {
			t.Errorf("sid だけを復号して返すべき: %+v", res.Cookies)
		}
		all, err := testWS.ReadCookies("P", []byte(testPassword))
		if err != nil || len(all.Cookies) != 2 {
			t.Errorf("names 無しは全件: %+v %v", all.Cookies, err)
		}
		assertNoLeftovers(t)
	})

	t.Run("Cookie DB が無いのは IsMissing（黙って skip）", func(t *testing.T) {
		isolateTemp(t)
		_, err := testWS.ReadCookies("NoSuch", []byte(testPassword))
		if !IsMissing(err) || IsEnvError(err) {
			t.Errorf("DB 無しが Missing にならない: %T %v", err, err)
		}
	})

	t.Run("壊れた DB は ReadBroken（記録して skip。止めない）", func(t *testing.T) {
		isolateTemp(t)
		home := os.Getenv("HOME")
		dir := filepath.Join(chromeProfileForTest(t, home, "P"), "Network")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "Cookies"), []byte("this is not sqlite"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := testWS.ReadCookies("P", []byte(testPassword))
		if !isReadKind(err, ReadBroken) || IsEnvError(err) {
			t.Errorf("壊れた DB が ReadBroken にならない: %T %v", err, err)
		}
		assertNoLeftovers(t)
	})

	t.Run("鍵違いで全件復号に失敗したら Diagnose が DecryptFailed", func(t *testing.T) {
		isolateTemp(t)
		home := os.Getenv("HOME")
		writeCookieDB(t, home, "P", "23", []fakeCookie{
			{host: ".example.com", name: "sid", enc: encWithPassword(t, "another-password", "SECRET", "")},
		})
		res, err := testWS.ReadCookies("P", []byte(testPassword))
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Cookies) != 0 {
			t.Errorf("復号できなかった Cookie を返している: %+v", res.Cookies)
		}
		if err := res.Diagnose("sid "); !isReadKind(err, DecryptFailed) {
			t.Errorf("全件の復号失敗が「無い」に化けた: %v", err)
		}
	})

	t.Run("Cookie が 0 件なら Diagnose は nil（黙って skip）", func(t *testing.T) {
		isolateTemp(t)
		home := os.Getenv("HOME")
		writeCookieDB(t, home, "P", "23", nil)
		res, err := testWS.ReadCookies("P", []byte(testPassword))
		if err != nil || len(res.Cookies) != 0 {
			t.Fatalf("空の DB: %+v %v", res.Cookies, err)
		}
		if err := res.Diagnose("sid "); err != nil {
			t.Errorf("手がかりが無いのに理由を付けた: %v", err)
		}
	})

	t.Run("-wal を読めず目的の Cookie が無ければ Diagnose が ReadIncomplete", func(t *testing.T) {
		isolateTemp(t)
		home := os.Getenv("HOME")
		path := writeCookieDB(t, home, "P", "23", nil)
		if err := os.WriteFile(path+"-wal", []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		lockForTest(t, path+"-wal")
		res, err := testWS.ReadCookies("P", []byte(testPassword))
		if err != nil {
			t.Fatalf("-wal だけ読めないのはプロファイル全体の失敗にしない: %v", err)
		}
		if err := res.Diagnose("sid "); !isReadKind(err, ReadIncomplete) {
			t.Errorf("読めなかった -wal を理由に添えていない: %v", err)
		}
		assertNoLeftovers(t)
	})

	t.Run("作業領域の異常は EnvError（即停止）", func(t *testing.T) {
		parent := isolateTemp(t)
		home := os.Getenv("HOME")
		writeCookieDB(t, home, "P", "23", nil)
		// 作業領域を別の場所へのシンボリックリンクにする。
		if err := os.Symlink(t.TempDir(), filepath.Join(parent, tempRootName)); err != nil {
			t.Fatal(err)
		}
		_, err := testWS.ReadCookies("P", []byte(testPassword))
		if !IsEnvError(err) {
			t.Errorf("作業領域の異常が EnvError にならない（プロファイルごとに skip されてしまう）: %T %v", err, err)
		}
	})
}
