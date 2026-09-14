package main

import "testing"

// parentRowKey は「子の行から畳むべき親の行」を返す唯一の出典で、disk (diskitem:) と
// docker (dockeritem:) で分解の形が違う。ここが壊れると Enter で入った中から抜けられなく
// なるが、**壊れ方は map miss = 無言**なので、rowKey の型を入れる前は誰も守っていなかった。
func TestParentRowKey(t *testing.T) {
	for _, c := range []struct {
		name string
		in   rowKey
		want rowKey
		ok   bool
	}{
		// 🚨 最初の 2 件は **リテラル**で書く。入力も期待値も構成子から作ると、prefix 文字列
		// そのものを変える退行を素通しする (構成子と分解が揃って変わるため。敵対レビュー 2026-09-14)
		{"disk の対象パス (綴りをリテラルで固定)", rowKey("diskitem:e00\x00/c/e00/x"), rowKey("disk:e00"), true},
		{"docker の候補 (綴りをリテラルで固定)", rowKey("dockeritem:images:alpine"), rowKey("docker:images"), true},
		{"disk の対象パスはエントリを親に持つ", diskItemRowKey(diskItemKey("e00", "/c/e00/x")), diskRowKey("e00"), true},
		{"パスに : が入っていても壊れない", diskItemRowKey(diskItemKey("e01", "/c/a:b/x")), diskRowKey("e01"), true},
		{"エントリ ID に : が入っていても壊れない", diskItemRowKey(diskItemKey("a:b", "/c/x")), diskRowKey("a:b"), true},
		{"docker の候補は群を親に持つ", dockerItemRowKey("images", "alpine"), dockerGroupRowKey("images"), true},
		{"docker の候補名に : が入っていても群は先頭まで", dockerItemRowKey("images", "repo:tag"), dockerGroupRowKey("images"), true},

		// 親を持たない行 (ここが false のままであることが、畳む側の前提)
		{"エントリの行", diskRowKey("e00"), "", false},
		{"brew の手", brewActionRowKey(1, 2), "", false},
		{"docker の群そのもの", dockerGroupRowKey("images"), "", false},
		{"まとめて回収", dockerPruneRowKey, "", false},
		{"空", "", "", false},

		// 分解できない形 (prefix だけ合っている)
		{"diskitem で \\x00 が無い", rowKey("diskitem:e00"), "", false},
		{"dockeritem で : が無い", rowKey("dockeritem:images"), "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parentRowKey(c.in)
			if ok != c.ok || got != c.want {
				t.Fatalf("parentRowKey(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.ok)
			}
		})
	}
}

// itemKey は「どのエントリのどのパスか」で、前半だけを取り出せることが選択の集計
// (hasSelectedItems / clearItemsOf) の前提になっている。
func TestItemKeyEntryID(t *testing.T) {
	k := diskItemKey("e00", "/c/e00/x")
	id, ok := k.entryID()
	if !ok || id != "e00" {
		t.Fatalf("entryID() = (%q, %v), want (e00, true)", id, ok)
	}
	if !k.belongsTo("e00") {
		t.Error("自分のエントリに属していない")
	}
	// 🚨 前方一致だけで判定すると、ID が前方一致する別エントリを巻き込む
	if k.belongsTo("e0") {
		t.Error("ID が前方一致する別のエントリに属している判定になった")
	}
	if k.belongsTo("e000") {
		t.Error("より長い ID のエントリに属している判定になった")
	}
}
