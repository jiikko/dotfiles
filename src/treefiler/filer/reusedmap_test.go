package filer

import "testing"

// reusedMap は中身を空にして同じ map を返し、要る数が容量の 1/4 を切ったら作り直す (巨大なフォルダの後に小さなフォルダへ
// 移ったとき、毎 tick 空の大きな map を舐めないため)。
func TestReusedMapShrinksAfterBigFolder(t *testing.T) {
	var r reusedMap[int, int]
	big := r.take(10000)
	big[1] = 1
	again := r.take(9000)
	if len(again) != 0 || r.cap != 10000 {
		t.Fatalf("同じくらいの数では使い回すはず: len=%d cap=%d", len(again), r.cap)
	}
	again[2] = 2
	if big[2] != 2 {
		t.Fatal("同じくらいの数で別の map を作った (使い回していない)")
	}
	if r.take(100); r.cap != 100 {
		t.Errorf("要る数が容量の 1/4 を切ったのに作り直さない: cap=%d", r.cap)
	}
	if r.take(500); r.cap != 500 {
		t.Errorf("容量を超えたのに作り直さない: cap=%d", r.cap)
	}
}

// 見込み (take の数) より多く入れたら fit で覚え、次に少なく要るときは作り直す (検索の layoutAll が 5 万件を入れた後、
// 膨らんだ map を毎フレーム舐め続けた。issue 712 の敵対レビュー)。
func TestReusedMapFitRemembersOverfill(t *testing.T) {
	var r reusedMap[int, int]
	m := r.take(10)
	for i := range 10000 {
		m[i] = i
	}
	r.fit(len(m))
	small := r.take(10)
	small[-1] = -1
	if _, ok := m[-1]; ok {
		t.Fatal("1 万件入れた map を 10 件の見込みで使い回した (膨らんだ map を毎回舐める)")
	}
}
