package main

import "testing"

func TestParseLog(t *testing.T) {
	// 実 git 出力を模す: 各レコード (%x1e 終端) の後ろに改行が入る。2 件目の先頭 \n が
	// SHA に混入しないこと (混入すると graphql oid が壊れる回帰) を固定する。
	got, err := parseLog("fullsha1\x1fabc1234\x1ffirst subject\x1e\nfullsha2\x1fdef5678\x1fsecond\x1e\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].SHA != "fullsha1" || got[0].ShortSHA != "abc1234" || got[0].Subject != "first subject" {
		t.Fatalf("parsed commits = %#v", got)
	}
}
