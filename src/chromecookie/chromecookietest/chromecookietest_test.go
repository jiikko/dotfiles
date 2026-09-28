package chromecookietest_test

import (
	"os"
	"testing"

	"github.com/jiikko/dotfiles/src/chromecookie"
	"github.com/jiikko/dotfiles/src/chromecookie/chromecookietest"
)

// 作った DB と暗号文を、本物の読み取り経路（chromecookie.Workspace.ReadCookies）が元の値に戻せること。
// 暗号化側がずれると、使う側のテストは「復号できない」前提で全部緑になりうる。
func TestWrittenDBRoundTripsThroughReadCookies(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	chromecookietest.WriteCookieDB(t, home, "P", "24", []chromecookietest.Cookie{
		{Host: ".example.com", Name: "enc", Enc: chromecookietest.Encrypt(t, "pw", "SECRET", ".example.com")},
		{Host: ".example.com", Name: "plain", Value: "v"},
	})
	res, err := chromecookie.NewWorkspace("chromecookietest-test").ReadCookies("P", []byte("pw"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, c := range res.Cookies {
		got[c.Name] = c.Value
	}
	if got["enc"] != "SECRET" || got["plain"] != "v" {
		t.Errorf("往復で値が戻らない: %v", got)
	}
	if _, err := os.Stat(chromecookietest.ProfileDir(home, "P")); err != nil {
		t.Errorf("ProfileDir が作った場所を指していない: %v", err)
	}
}
