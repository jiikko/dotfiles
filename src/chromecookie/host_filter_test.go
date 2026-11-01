package chromecookie

import (
	"os"
	"slices"
	"sort"
	"testing"
)

// hostKeyCandidates が作る集合（SQL の IN で絞る host_key）は、HostMatches が真になる host_key と
// 完全に一致しなければならない。足りないと送るべき Cookie を落とし、多いと他サイトの Cookie を復号する。
// HostMatches を oracle にして、reqHost の文字単位の suffix・接頭辞を足した紛らわしい host_key を総当たりで比べる。
func TestHostKeyCandidatesAgreeWithHostMatches(t *testing.T) {
	reqHosts := []string{"team.esa.io", "esa.io", "a.b.c.d", "localhost", "x.example.co.jp"}
	for _, req := range reqHosts {
		cands := hostKeyCandidates(req)
		var keys []string
		for i := range len(req) + 1 {
			suf := req[i:]
			keys = append(keys, suf, "."+suf, "not"+suf, ".not"+suf, suf+".evil", "."+suf+".evil")
		}
		keys = append(keys, "", ".", "..", ".slack.com", "slack.com")
		checked := 0
		for _, hk := range keys {
			want := HostMatches(hk, req)
			if got := slices.Contains(cands, hk); got != want {
				t.Errorf("reqHost=%q host_key=%q: 候補に含む=%v, HostMatches=%v", req, hk, got, want)
			}
			if want {
				checked++
			}
		}
		// 逆向き: 候補はどれも HostMatches が真（keys に無い余分な値を返しても上のループでは落ちない）。
		for _, c := range cands {
			if !HostMatches(c, req) {
				t.Errorf("reqHost=%q: 候補 %q は HostMatches が偽（送り先でない host_key を読む）", req, c)
			}
		}
		// 真になる側を 1 件も試していなければ、一致は「両方とも空」でも成り立ってしまう。
		if checked < 2 {
			t.Errorf("reqHost=%q: HostMatches が真の host_key を %d 件しか試していない", req, checked)
		}
	}
}

func TestReadCookiesForHost(t *testing.T) {
	type hn struct{ host, name string }
	got := func(cs []Cookie) []hn {
		var out []hn
		for _, c := range cs {
			out = append(out, hn{c.Host, c.Name})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].host+"\x00"+out[i].name < out[j].host+"\x00"+out[j].name })
		return out
	}

	t.Run("送信先に送られうる host_key の行だけを返し、他サイトの Cookie は返さない", func(t *testing.T) {
		isolateTemp(t)
		home := os.Getenv("HOME")
		enc := func(plain, host string) []byte { return encWithPassword(t, testPassword, plain, host) }
		// 除外される行も正しい鍵で暗号化しておく（絞らなければ復号に成功して結果に現れる）。
		writeCookieDB(t, home, "P", "24", []fakeCookie{
			{host: ".esa.io", name: "_session", enc: enc("S", ".esa.io")},
			{host: "team.esa.io", name: "host_only", value: "h"},
			{host: ".team.esa.io", name: "sub", enc: enc("T", ".team.esa.io")},
			{host: "other.esa.io", name: "other_team", enc: enc("O", "other.esa.io")},
			{host: "esa.io", name: "apex_host_only", enc: enc("A", "esa.io")},
			{host: "notesa.io", name: "lookalike", enc: enc("N", "notesa.io")},
			{host: ".notesa.io", name: "lookalike_dom", enc: enc("N2", ".notesa.io")},
			{host: ".slack.com", name: "d", enc: enc("SLACK", ".slack.com")},
		})
		res, err := testWS.ReadCookiesForHost("P", []byte(testPassword), "team.esa.io")
		if err != nil {
			t.Fatal(err)
		}
		want := []hn{{".esa.io", "_session"}, {".team.esa.io", "sub"}, {"team.esa.io", "host_only"}}
		if g := got(res.Cookies); !slices.Equal(g, want) {
			t.Errorf("返った Cookie:\n got  %v\n want %v", g, want)
		}
		for _, c := range res.Cookies {
			if c.Name == "_session" && c.Value != "S" {
				t.Errorf("_session の復号値が違う: %q", c.Value)
			}
		}

		named, err := testWS.ReadCookiesForHost("P", []byte(testPassword), "team.esa.io", "_session", "d")
		if err != nil {
			t.Fatal(err)
		}
		if g := got(named.Cookies); !slices.Equal(g, []hn{{".esa.io", "_session"}}) {
			t.Errorf("names と host の両方で絞るべき: %v", g)
		}
		assertNoLeftovers(t)
	})

	t.Run("鍵違いの判定は送信先の行だけで数える（他サイトの行は復号しない）", func(t *testing.T) {
		isolateTemp(t)
		home := os.Getenv("HOME")
		writeCookieDB(t, home, "P", "24", []fakeCookie{
			{host: ".slack.com", name: "d", enc: encWithPassword(t, "wrongpassword", "x", ".slack.com")},
		})
		res, err := testWS.ReadCookiesForHost("P", []byte(testPassword), "team.esa.io")
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Cookies) != 0 || res.decrypt.tried != 0 {
			t.Errorf("他サイトの行を復号しようとした: cookies=%v tried=%d", res.Cookies, res.decrypt.tried)
		}
		if derr := res.Diagnose("team.esa.io 宛ての Cookie "); derr != nil {
			t.Errorf("送信先の行が無いのに診断が出た: %v", derr)
		}
	})

	t.Run("reqHost が空ならエラー", func(t *testing.T) {
		isolateTemp(t)
		if _, err := testWS.ReadCookiesForHost("P", []byte(testPassword), ""); err == nil {
			t.Error("空の reqHost を受け付けた")
		}
	})
}
