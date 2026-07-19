package main

import "testing"

func TestParseLog(t *testing.T) {
	got, err := parseLog("fullsha1\x1fabc1234\x1ffirst subject\x1efullsha2\x1fdef5678\x1fsecond\x1e")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].SHA != "fullsha1" || got[0].ShortSHA != "abc1234" || got[0].Subject != "first subject" {
		t.Fatalf("parsed commits = %#v", got)
	}
}
