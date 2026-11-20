package oracle

import (
	"encoding/json"
	"testing"
)

// driver に渡す argv と setenv の値は、単独のサロゲートを "\udcff" の escape で書く (python3.15 の json.loads が
// '["z\\udcff", "q\\udced\\udca0\\udc80"]' を ['z\udcff', 'q\udced\udca0\udc80'] に読むことを確かめた)。encoding/json に任せると U+FFFD になる。
func TestArgvAndOpWriteSurrogatesAsEscapes(t *testing.T) {
	argv := Argv{"a", ArgvSurrogates[0].Value, ArgvSurrogates[1].Value, "あ\"\n"}
	got, err := json.Marshal(argv)
	if err != nil {
		t.Fatal(err)
	}
	if want := `["a","z\udcff","q\udced\udca0\udc80","あ\"\n"]`; string(got) != want {
		t.Errorf("argv: %s、期待 %s", got, want)
	}
	op, err := json.Marshal(Op{Kind: "setenv", Name: "S", Value: ArgvSurrogates[0].Value})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"op":"setenv","name":"S","value":"z\udcff"}`; string(op) != want {
		t.Errorf("op: %s、期待 %s", op, want)
	}
	// 上位と下位が並ぶと json.loads が 1 字に合わせるので断る。WTF-8 でないバイトも断る
	for _, bad := range []string{surrogates(0xD800, 0xDC00), "\xff"} {
		if _, err := json.Marshal(Argv{bad}); err == nil {
			t.Errorf("% x を断らない", bad)
		}
	}
}
