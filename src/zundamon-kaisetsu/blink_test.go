package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"maps"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"zundamon-kaisetsu/internal/build"
	"zundamon-kaisetsu/internal/paths"
	"zundamon-kaisetsu/internal/pyjson"
	"zundamon-kaisetsu/internal/script"
)

// まばたきの時刻表: 閉じている長さ・間隔・キャラごとのずれ (BlinkAt)
func TestBlinkAtSchedule(t *testing.T) {
	const span = 600.0 // 10 分
	starts := map[string][]float64{}
	for _, key := range script.CastKeys() {
		closedFrames, prev := 0, false
		for k := range int(span * build.MouthFPS) {
			tm := (float64(k) + 0.5) / build.MouthFPS
			c := build.BlinkAt(key, tm)
			if c {
				closedFrames++
			}
			if c && !prev {
				starts[key] = append(starts[key], tm)
			}
			if !c && prev && (closedFrames < 3 || closedFrames > 4) { // 0.12 秒 = 30fps で 3〜4 フレーム
				t.Errorf("%s: %.2f 秒で終わったまばたきが %d フレーム (3〜4 のはず)", key, tm, closedFrames)
			}
			if !c {
				closedFrames = 0
			}
			prev = c
		}
		if got, want := len(starts[key]), int(span/build.BlinkCell); got != want {
			t.Errorf("%s: %v 秒でまばたき %d 回 (区間ごとに 1 回なら %d 回)", key, span, got, want)
		}
		for i := 1; i < len(starts[key]); i++ {
			if gap := starts[key][i] - starts[key][i-1]; gap < 2-0.1 || gap > 6+0.1 {
				t.Errorf("%s: まばたきの間隔 %.2f 秒 (%.2f → %.2f) が 2〜6 秒の外", key, gap, starts[key][i-1], starts[key][i])
			}
		}
	}
	if keys := script.CastKeys(); slices.Equal(starts[keys[0]], starts[keys[1]]) {
		t.Error("2 人のまばたきの時刻が全部同じ (キャラごとにずらしていない)")
	}
	if build.BlinkAt(script.CastKeys()[0], -1) {
		t.Error("負の時刻でまばたいた")
	}
}

// 閉じ目の版がある表情を見せているフレームだけで、そのキャラのビットが立つ (BlinkRuns)
func TestBlinkRunsFollowsShownFace(t *testing.T) {
	metan, zunda := script.CastKeys()[0], script.CastKeys()[1]
	bit := map[string]int{metan: 1, zunda: 2}
	const dur = 24.0
	// 行 0 (0〜12 秒): めたんは説明・ずんだもんは笑顔。行 1 (12 秒〜): ずんだもんが通常に戻す
	tl := []build.TimelineLine{
		{Start: 0, End: 11, Faces: map[string]string{metan: "説明", zunda: "笑顔"}, Mouth: "0"},
		{Start: 12, End: 23, Faces: map[string]string{metan: "説明", zunda: "通常"}, Mouth: "0"},
	}
	frames := build.FrameRuns(tl, dur)
	blinkable := map[string]map[string]bool{metan: {"説明": true}, zunda: {"通常": true}} // 笑顔には閉じ目の版が無い
	runs := build.BlinkRuns(frames, tl, blinkable, dur)
	if runs == nil {
		t.Fatal("まばたきの列が空")
	}
	states := build.FrameStates(frames, runs, nil, dur)
	seen := map[string]bool{}
	for k, st := range states {
		tm := (float64(k) + 0.5) / build.MouthFPS
		for _, key := range []string{metan, zunda} {
			face := tl[st[0]].Faces[key]
			want := blinkable[key][face] && build.BlinkAt(key, tm)
			if got := st[3]&bit[key] != 0; got != want {
				t.Fatalf("フレーム %d (%.2f 秒) の %s (表情 %s): 閉じ目 %v want %v", k, tm, key, face, got, want)
			}
			if want {
				seen[key+"/"+face] = true
			}
		}
	}
	// 前提の確認: 両方のキャラが実際にまばたいた (どちらも一度も閉じないと、上の比較は false どうしで通る)
	for _, w := range []string{metan + "/説明", zunda + "/通常"} {
		if !seen[w] {
			t.Errorf("%s で一度もまばたいていない (テストの尺が足りない)", w)
		}
	}
	if build.BlinkRuns(frames, tl, map[string]map[string]bool{}, dur) != nil {
		t.Error("閉じ目の版が無いのに、まばたきの列を作った (データに載せないはず)")
	}
}

// まばたきの mask は CastOrder の i 番目が 1<<i (CastData.BlinkBit と BlinkRuns が同じ割り当てを使う)
func TestBlinkBitMatchesCastOrder(t *testing.T) {
	keys := script.CastKeys()
	tl := []build.TimelineLine{{Start: 0, End: 100, Faces: map[string]string{keys[0]: "通常", keys[1]: "通常"}, Mouth: "0"}}
	frames := build.FrameRuns(tl, 100)
	for i, key := range keys {
		runs := build.BlinkRuns(frames, tl, map[string]map[string]bool{key: {"通常": true}}, 100)
		for _, r := range runs {
			if r[1] != 0 && r[1] != 1<<i {
				t.Fatalf("%s だけがまばたく台本で mask %d (want 0 か %d)", key, r[1], 1<<i)
			}
		}
	}
}

// blinkFixture は testdata/build の台本と合成済みの wav を一時 dir に写し、めたんの立ち絵に閉じ目の版を足す。
// blinkList は faces.json の blink (nil なら書かない)、withFiles が false なら閉じ目の画像を置かない。
func blinkFixture(t *testing.T, blinkList any, withFiles bool) string {
	t.Helper()
	dir := t.TempDir()
	copyTree(t, filepath.Join("testdata", "build", "script.work"), filepath.Join(dir, "script.work"))
	faces := filepath.Join(dir, "faces", "metan")
	copyTree(t, filepath.Join("testdata", "build", "faces", "metan"), faces)
	b, err := os.ReadFile(filepath.Join("testdata", "build", "script.json"))
	must(t, err)
	must(t, os.WriteFile(filepath.Join(dir, "script.json"), b, 0o644))
	var meta map[string]any
	mb, err := os.ReadFile(filepath.Join(faces, "faces.json"))
	must(t, err)
	must(t, json.Unmarshal(mb, &meta))
	if blinkList != nil {
		meta["blink"] = blinkList
	}
	mb, err = json.Marshal(meta)
	must(t, err)
	must(t, os.WriteFile(filepath.Join(faces, "faces.json"), mb, 0o644))
	if withFiles {
		for _, face := range []string{"通常", "説明"} {
			for lv := range build.MouthLevels {
				src, err := os.ReadFile(filepath.Join(faces, face+"_"+string(rune('0'+lv))+".webp"))
				must(t, err)
				// 閉じ目の版と元の絵を見分けられるよう、末尾に 1 バイト足す (webp の中身は見ない)
				must(t, os.WriteFile(filepath.Join(faces, face+"_blink_"+string(rune('0'+lv))+".webp"), append(src, 'B'), 0o644))
			}
		}
	}
	return filepath.Join(dir, "script.json")
}

// withZundamonFaces は blinkFixture の台本で、ずんだもんにも閉じ目の版つきの立ち絵を持たせる (2 人目のビットを確かめるため)。
// 絵はめたんの fixture の通常に表情の名前を足したもの (webp の中身は見ないので、バイト列の差だけで見分ける)。台本で使う 驚き・笑顔 も置く。
func withZundamonFaces(t *testing.T, scr string) string {
	t.Helper()
	dir := filepath.Dir(scr)
	zf := filepath.Join(dir, "faces", "zundamon")
	must(t, os.MkdirAll(zf, 0o755))
	for _, face := range []string{"通常", "驚き", "笑顔"} {
		for lv := range build.MouthLevels {
			src, err := os.ReadFile(filepath.Join(dir, "faces", "metan", "通常_"+string(rune('0'+lv))+".webp"))
			must(t, err)
			src = append(src, []byte(face)...) // 表情ごとに違う絵にする
			must(t, os.WriteFile(filepath.Join(zf, face+"_"+string(rune('0'+lv))+".webp"), src, 0o644))
			if face != "笑顔" { // 笑顔はもともと目を閉じている表情として、閉じ目の版を置かない
				must(t, os.WriteFile(filepath.Join(zf, face+"_blink_"+string(rune('0'+lv))+".webp"), append(src, 'B'), 0o644))
			}
		}
	}
	mb, err := json.Marshal(map[string]any{"faces": []string{"通常", "驚き", "笑顔"}, "credit": "立ち絵: テスト用", "blink": []string{"通常", "驚き"}})
	must(t, err)
	must(t, os.WriteFile(filepath.Join(zf, "faces.json"), mb, 0o644))
	var raw map[string]any
	b, err := os.ReadFile(scr)
	must(t, err)
	must(t, json.Unmarshal(b, &raw))
	raw["cast"].(map[string]any)["zundamon"] = map[string]any{"faces": "faces/zundamon"}
	writeScriptFile(t, scr, raw)
	return scr
}

// decodeURI は data URI の中身 (base64) をバイト列に戻す。
func decodeURI(t *testing.T, uri string) []byte {
	t.Helper()
	i := strings.Index(uri, ";base64,")
	if i < 0 {
		t.Fatalf("data URI でない: %.40s", uri)
	}
	b, err := base64.StdEncoding.DecodeString(uri[i+len(";base64,"):])
	must(t, err)
	return b
}

// 閉じ目の版は、同じ表情・同じ口の段階の元の絵 + 'B' (fixture の作り方) と全部一致する (表情や口の段階の取り違えを見分ける)
func checkBlinkImages(t *testing.T, who string, c build.CastData) {
	t.Helper()
	for face, uris := range c.Blink {
		if len(uris) != build.MouthLevels {
			t.Fatalf("%s/%s の閉じ目の版が %d 枚 (口の段階は %d)", who, face, len(uris), build.MouthLevels)
		}
		for lv, u := range uris {
			want := append(decodeURI(t, c.Images[face][lv]), 'B')
			if got := decodeURI(t, u); !bytes.Equal(got, want) {
				t.Errorf("%s/%s の口 %d の閉じ目の版が、同じ表情・同じ口の元の絵から作ったものでない", who, face, lv)
			}
		}
	}
}

func assembleFixture(t *testing.T, scr string) (*build.PlayerData, error) {
	t.Helper()
	env := testEnv(t)
	s, err := script.LoadScript(paths.ResolvePath(scr), env)
	if err != nil {
		return nil, err
	}
	data, _, err := build.Assemble(s, env)
	return data, err
}

// 閉じ目の版を書き出した立ち絵では、使う表情の閉じ目を埋め込み、まばたきの列を作る
func TestBuildEmbedsBlinkImages(t *testing.T) {
	data, err := assembleFixture(t, blinkFixture(t, []any{"通常", "説明"}, true))
	must(t, err)
	metan := data.Cast[script.CastKeys()[0]]
	if got := pyjson.SortedKeys(metan.Blink); !slices.Equal(got, []string{"説明", "通常"}) {
		t.Errorf("閉じ目を埋め込んだ表情: %v (want 台本で使う 説明・通常。faces.json に無い 笑顔 は入れない)", got)
	}
	checkBlinkImages(t, "metan", metan)
	if metan.BlinkBit != 1 {
		t.Errorf("めたんの blinkBit: %d (castOrder の 0 番目なので 1)", metan.BlinkBit)
	}
	if z := data.Cast[script.CastKeys()[1]]; z.Blink != nil || z.BlinkBit != 0 {
		t.Error("立ち絵の無いずんだもんに閉じ目のデータがある")
	}
	if data.Blinks == nil {
		t.Fatal("まばたきの列が無い (3.58 秒の台本でも最初の区間でまばたく)")
	}
	for _, r := range data.Blinks {
		if r[1]&^metan.BlinkBit != 0 {
			t.Errorf("まばたけないずんだもんのビットが立った: %v", r)
		}
	}
	b, err := json.Marshal(data)
	must(t, err)
	for _, k := range []string{`"blinks":`, `"blinkBit":1`, `"blink":{`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("プレイヤーのデータに %s が無い", k)
		}
	}
}

// 閉じ目の版の無い (古い書き出しの) 立ち絵では、まばたきのデータを何も載せない (Python 版と同じデータのまま)
func TestBuildWithoutBlinkKeepsData(t *testing.T) {
	data, err := assembleFixture(t, blinkFixture(t, nil, false))
	must(t, err)
	b, err := json.Marshal(data)
	must(t, err)
	for _, k := range []string{`"blinks"`, `"blinkBit"`, `"blink"`} {
		if strings.Contains(string(b), k) {
			t.Errorf("閉じ目の版が無いのにプレイヤーのデータに %s がある", k)
		}
	}
}

// faces.json が閉じ目の版を書いたと言うのに画像が欠けていれば、黙ってまばたきを省かずに止まる
func TestBuildFailsOnMissingBlinkImages(t *testing.T) {
	_, err := assembleFixture(t, blinkFixture(t, []any{"説明"}, false))
	if err == nil || !strings.Contains(err.Error(), "説明_blink_0.webp") {
		t.Fatalf("欠けた閉じ目の画像を挙げて止まるはず: %v", err)
	}
	_, err = assembleFixture(t, blinkFixture(t, "説明", true))
	if err == nil || !strings.Contains(err.Error(), "blink") {
		t.Fatalf("blink がリストでない faces.json で止まるはず: %v", err)
	}
}

// 2 人とも閉じ目の版を持つと、ずんだもんは CastOrder の 1 番目なのでビット 2 で、2 人のビットが列に現れる
func TestBuildBlinkBitsForBothCast(t *testing.T) {
	data, err := assembleFixture(t, withZundamonFaces(t, blinkFixture(t, []any{"通常", "説明"}, true)))
	must(t, err)
	keys := script.CastKeys()
	for i, key := range keys {
		c := data.Cast[key]
		if c.BlinkBit != 1<<i {
			t.Errorf("%s の blinkBit: %d (want %d)", key, c.BlinkBit, 1<<i)
		}
		checkBlinkImages(t, key, c)
	}
	seen := 0
	for _, r := range data.Blinks {
		seen |= r[1]
	}
	if seen != 3 {
		t.Errorf("まばたきの列に現れたビット: %b (want 2 人とも = 11)", seen)
	}
}

// まばたきのある台本でも、mp4 の撮影・切り出し・フレームの並びが状態 (まばたきを含む) と合う
func TestWriteMP4CropsBlinkStates(t *testing.T) {
	scr := blinkFixture(t, []any{"通常", "説明"}, true)
	data, err := assembleFixture(t, scr)
	must(t, err)
	blinkFrames := 0
	for _, st := range build.FrameStates(data.Frames, data.Blinks, data.Cards, data.Duration) {
		if st[3] != 0 {
			blinkFrames++
		}
	}
	if blinkFrames == 0 || blinkFrames > int(math.Ceil(build.BlinkClosed*build.MouthFPS))+1 {
		t.Fatalf("前提: 3.58 秒の台本で目を閉じるフレームは 1 回分 (3〜4) のはず: %d", blinkFrames)
	}
	checkMP4CropsEachState(t, scr)
}

// 2 人ともまばたく台本でも (ビット 2 の状態を撮る)、mp4 の撮影・切り出しが状態と合う
func TestWriteMP4CropsBothCastBlinkStates(t *testing.T) {
	checkMP4CropsEachState(t, withZundamonFaces(t, blinkFixture(t, []any{"通常", "説明"}, true)))
}

// 区切りのカード (issue 681 / 745): チャプターの 1 行目の音声が終わるまで出し (最短 1.5 秒・最長 4 秒)、1 行目に図解が出ていれば
// 最短のまま。次のチャプターが始まったら数え直す。トピック名と同じ条件 (チャプター 2 つ以上・overlays に topic) でだけ出す
func TestChapterCardRuns(t *testing.T) {
	chs := []build.ChapterData{{Title: "a", Start: 0.4}, {Title: "b", Start: 3}, {Title: "c", Start: 3.5}, {Title: "d", Start: 9}, {Title: "e", Start: 16}}
	one := 1
	timeline := []build.TimelineLine{
		{Chapter: 0, Start: 0.4, End: 2.8},          // a: 1 行目が 2.4 秒 → 行の終わり (2.8) まで
		{Chapter: 1, Start: 3, End: 3.2},            // b: 1 行目が短い → 最短の 1.5 秒 (次の c が 3.5 で始まるので 3.5 で数え直す)
		{Chapter: 2, Start: 3.5, End: 9},            // c: 1 行目が長い → 最長の 4 秒 (7.5 まで)
		{Chapter: 3, Start: 9, End: 12, Show: &one}, // d: 1 行目に図解 → 最短 (10.5 まで)
		{Chapter: 4, Start: 16, End: 17},            // e: 最後のチャプター
	}
	ends := build.ChapterCardEnds(chs, timeline)
	if want := []float64{2.8, 4.5, 7.5, 10.5, 17.5}; !slices.Equal(ends, want) {
		t.Fatalf("カードを消す時刻 %v, want %v", ends, want)
	}
	got := build.ChapterCardRuns(chs, ends, script.OverlayKeys, 20)
	on := func(sec float64) int {
		k := int(sec * build.MouthFPS)
		return build.RunCursor(got)(k)
	}
	for _, tc := range []struct {
		sec  float64
		want int
	}{{0.2, 0}, {0.5, 1}, {2.7, 1}, {2.9, 0}, {3.1, 1}, {3.6, 1}, {7.4, 1}, {7.6, 0}, {9.1, 1}, {10.4, 1}, {10.6, 0}, {16.1, 1}, {17.4, 1}, {17.6, 0}} {
		if g := on(tc.sec); g != tc.want {
			t.Errorf("%.1f 秒: カード %d, want %d (runs %v)", tc.sec, g, tc.want, got)
		}
	}
	if r := build.ChapterCardRuns(chs[:1], ends[:1], script.OverlayKeys, 6); r != nil {
		t.Errorf("チャプターが 1 つでカードを出した: %v", r)
	}
	if r := build.ChapterCardRuns(chs, ends, []string{"title", "date"}, 6); r != nil {
		t.Errorf("overlays に topic が無いのにカードを出した: %v", r)
	}
	// FrameStates の 5 欄目に載る
	st := build.FrameStates([][4]int{{0, 0, 0, 0}}, nil, got, 6)
	if st[int(0.5*build.MouthFPS)][4] != 1 || st[int(2.9*build.MouthFPS)][4] != 0 {
		t.Errorf("frameStates の 5 欄目がカードの列と違う")
	}
}

// 感情の演出 (issue 682) は player.html だけにあり、立ち絵の表情の名前 (data-fx) で出し分ける。
// 表情の名前は FaceVocab が正本なので、演出が指す名前が語彙にあること・表情を data-fx に渡していることを字句で見る
// (演出の見た目そのものは人が見る。issue 645)
func TestEmotionEffectsWired(t *testing.T) {
	tpl := playerTemplate(t)
	for _, want := range []string{"a.box.dataset.fx = face;", "fig.insertAdjacentHTML('beforeend', FX);",
		"box.insertAdjacentHTML('beforeend', FX);",       // 丸アバター
		".char .fx { position: absolute; display: none;", // 表情に合わないものは隠す (消すと全員に全部の演出が出る)
		// 立ち絵の箱と画像の上限 (演出の位置の基準) を同じ変数から決める (片方だけ直すと演出がずれる)
		"width: calc(var(--char-w) * 1%); height: calc(var(--char-h) * 1%);",
		"max-width: calc(var(--char-w) * 1cqw); max-height: calc(var(--char-h) * .5625cqw);"} {
		if !strings.Contains(tpl, want) {
			t.Errorf("player.html に %q が無い (表情を演出へ渡していない)", want)
		}
	}
	re := regexp.MustCompile(`\.char\[data-fx="([^"]+)"\] \.fx-([a-z]+)`)
	got := map[string]string{}
	for _, m := range re.FindAllStringSubmatch(tpl, -1) {
		got[m[1]] = m[2]
		if !slices.Contains(script.FaceVocab, m[1]) {
			t.Errorf("演出が指す表情 %s が faceVocab に無い (表情の名前を変えたら演出の CSS も直す)", m[1])
		}
		if !strings.Contains(tpl, `class="fx fx-`+m[2]+`"`) {
			t.Errorf("演出 fx-%s の絵が FX に無い", m[2])
		}
	}
	if want := map[string]string{"驚き": "surprise", "怒り": "anger", "悲しみ": "gloom"}; !maps.Equal(got, want) {
		t.Errorf("演出と表情の対応が違う: %v (want %v)", got, want)
	}
}
