package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// 読みの fixture は VOICEVOX 0.25.2 が実際に返した kana (2026-10-08 に Mac vs Windows と IT 構造の台本で取った。issue 673)。
// エンジンの版で読みが変わったら、取り直してよい (テストが見ているのは照合の規則で、特定の版の読みではない)。
var engineLetters = map[string]string{
	"A": "エ'イ", "B": "ビ'イ", "C": "シ'イ", "D": "ディ'イ", "E": "イ'イ", "F": "エ'フ", "G": "ジ'イ", "H": "エ'イチ", "I": "ア'イ",
	"J": "ジェ'イ", "K": "ケ'イ", "L": "エ'ル", "M": "エ'ム", "N": "エ'ヌ", "O": "オ'オ", "P": "ピ'イ", "Q": "キュ'ウ", "R": "ア'アル",
	"S": "エ'ス", "T": "ティ'イ", "U": "ユ'ウ", "V": "ブ'イ", "W": "ダ'ブリュウ", "X": "エ'ッ_クス", "Y": "ワ'イ", "Z": "ズィ'イ",
}

// 台本の行の (音声にする文, エンジンの読み)。want は 1 文字ずつ読まれた語
var engineLines = []struct {
	spoken, kana string
	want         []string
}{
	{"設計の仕事よ。SOLIDWORKSという設計ソフトは、Windowsでしか正式に使えないの。",
		"セッケエノ'/シゴトヨ'、エ'スオオエルアイディイダブリュウオオアアルケエエスト/イウ'/セッケエソ'_フトワ、ウィ'ンドオズデ_シカ/セエ_シキニ'/_ツカエ'ナイノ", []string{"SOLIDWORKS"}},
	{"IDORは、IDを書き換えるだけで他人のデータが見えてしまう脆弱性のことね。",
		"ア'イディイオオアアルワ、ア'イディイオ/カ_キカエ'ルダケデ/タニンノ'/デ'エタガ/ミエ'テ/シマウ'/ゼエジャ_クセエノ'/コト'ネ", []string{"IDOR"}},
	// 崩れた読み (1 文字ずつではない) は検査では拾わない。一覧で人が見る
	{"認可とIDORって何なのだ？", "ニ'ンカト/イドオ'/ア'アル/ッテ'/ナ'ニナ/ノダ'？", nil},
	{"元になったのは、SIerの多重下請けのせいでは", "モト'ニ/ナ'ッタ/ノワ'、エ'スイアアノ/タジュウシ'タウケノ/セ'イデワ", nil},
	{"CPUやGPU、冷却、メモリ", "シイピイユ'ウヤ/ジイピイユ'ウ、レエキャク'、メモリ'", nil}, // 略語の一覧にあるので鳴らない
	// え段で終わる文字名 (A・K) の後に E (イイ) が続く形。行の読みの長音を揃えると境目をまたいで置き換わり、取りこぼした (敵対的レビュー)
	{"AEDを使う", "エエイイディ'イオ/_ツカウ'", []string{"AED"}},
	{"KEYを押す", "ケ'エイイワイオ/オス'", []string{"KEY"}},
	// 全角英字もエンジンは同じに読む
	{"ＳＯＬＩＤＷＯＲＫＳという設計ソフト", "エ'スオオエルアイディイダブリュウオオアアルケエエスト/イウ'/セッケエソ'_フト", []string{"SOLIDWORKS"}},
	// it は イット と読まれている。前の IT (略語) の読みに当てて誤検出していた (敵対的レビュー)
	{"ITとitの話", "アイティ'イト/イ'ットノ/ハナシ'", nil},
	{"itとITの話", "イ'ットト/アイティ'イノ/ハナシ'", nil}, // 逆の順も
	// No は ノオ と読まれている。No の 1 文字読みの並び (エヌオオ) が NOC・SNOWDEN の読みの途中に現れる。
	// 照合位置を前から進める形では、No を誤検出し NOC・SNOWDEN を取りこぼした (敵対的レビュー 2 周目)
	{"NoとNOC", "ノ'オト/エヌオオシ'イ", []string{"NOC"}},
	{"NoのSNOWDENです", "ノ'オノ/エ'スエヌオオダブリュウディイイイエヌデ_ス", []string{"SNOWDEN"}},
	// 前の語の最後の C と CC の頭の C の一致が取り分と重なる。重なった一致の終わりから探すと、CC 本体を拾えなかった (3 周目)
	{"メールはPC、CCに送るのだ", "メエルワ'/ピイシ'イ、シ'イシイニ/オクル'/ノダ'", []string{"CC"}},
	{"ABC、CC", "エイビイシ'イ、シ'イシイ", []string{"ABC", "CC"}},
	{"Windowsが95.03%、macOSは1.92%だったわ。", "ウィ'ンドオズガ/キュ'ウジュウ/ゴ'オテン/ゼロ'/サンパアセ'ント、マックオ'オエスワ/イ'ッテン/キュウ'/ニイパアセ'ントダッタワ", nil},
}

func fixtureNames() map[byte][]string {
	names := map[byte][]string{}
	for c, k := range engineLetters {
		names[c[0]] = letterForms(k, letterAliases[c[0]])
	}
	return names
}

func TestSpelledWordsOnEngineReadings(t *testing.T) {
	names := fixtureNames()
	acronyms := map[string]bool{"CPU": true, "GPU": true, "ID": true, "IT": true, "PC": true}
	for _, tc := range engineLines {
		if got := spelledWords(tc.spoken, tc.kana, names, acronyms); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v (want %v)", tc.spoken, got, tc.want)
		}
	}
	// 略語の一覧が無ければ、CPU・GPU も 1 文字ずつ読んだ語として鳴る (一覧が効いていることの確かめ)
	if got := spelledWords("CPUやGPU、冷却、メモリ", "シイピイユ'ウヤ/ジイピイユ'ウ、レエキャク'、メモリ'", names, nil); !slices.Equal(got, []string{"CPU", "GPU"}) {
		t.Errorf("略語の一覧が無いとき: %v (want [CPU GPU])", got)
	}
}

// 単独の文字名 (K = ケイ) と文の中の読み (ケエ) は書き方が違う。文字名の側に両方を持たせないと SOLIDWORKS を取りこぼす
func TestLetterFormsHasBothLongVowels(t *testing.T) {
	for in, want := range map[string][]string{"ケ'イ": {"ケイ", "ケエ"}, "オ'オ": {"オオ"}, "エ'ッ_クス": {"エックス"}, "ア'イ": {"アイ"}} {
		if got := letterForms(in, nil); !slices.Equal(got, want) {
			t.Errorf("letterForms(%q) = %v (want %v)", in, got, want)
		}
	}
}

// kana --script --check の入口: 警告があれば rc=1 (error)・警告の行と英字の語の一覧を出す。無ければ nil
func TestKanaCheckCommand(t *testing.T) {
	kanaOf := map[string]string{}
	for c, k := range engineLetters {
		kanaOf[c] = k
	}
	for _, l := range engineLines {
		kanaOf[l.spoken] = l.kana
	}
	kanaOf["SOLIDWORKS"] = "エ'スオオエルアイディイダブリュウオオアアルケエエス"
	kanaOf["MacとWindows、どちらが上かという論争よ。"] = "マ'ッ_クト/ウィ'ンドオズ、ド'チラガ/ウエカト'/イウ'/ロンソオヨ'"
	kanaOf["Mac"] = "マ'ック"
	kanaOf["IDOR"] = "イドオ'/ア'アル"
	kanaOf["ID"] = "ア'イディイ"
	kanaOf["Windows"] = "ウィ'ンドオズ"
	kanaOf["CPU"] = "シイピイユ'ウ"
	kanaOf["GPU"] = "ジイピイユ'ウ"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio_query" {
			http.NotFound(w, r)
			return
		}
		k, ok := kanaOf[r.URL.Query().Get("text")]
		if !ok {
			t.Errorf("想定していない問い合わせ: %q", r.URL.Query().Get("text"))
		}
		b, _ := json.Marshal(map[string]any{"kana": k, "accent_phrases": []any{}})
		_, _ = w.Write(b)
	}))
	defer srv.Close()
	run := func(lines ...string) (string, error) {
		var raw []any
		for _, l := range lines {
			raw = append(raw, map[string]any{"who": "metan", "text": l})
		}
		path := writeScript(t, map[string]any{"lines": raw})
		env := testEnv(t)
		env.Engine = srv.URL
		var out strings.Builder
		env.Stdout = &out
		err := cmdKana(env, nil, path, "metan", nil, true)
		return out.String(), err
	}
	out, err := run(engineLines[0].spoken, engineLines[4].spoken)
	if err == nil || !strings.Contains(err.Error(), "1 件") {
		t.Errorf("1 文字ずつ読んだ行があるのに rc=0 か件数が違う: %v", err)
	}
	if !strings.Contains(out, "0\tSOLIDWORKS\t") || !strings.Contains(out, "SOLIDWORKS\t1 行\t") || !strings.Contains(out, "CPU\t1 行\tシイピイユ'ウ\t(略語)") {
		t.Errorf("警告の行か英字の語の一覧が出ていない:\n%s", out)
	}
	out, err = run(engineLines[4].spoken)
	if err != nil || !strings.Contains(out, "(なし)") {
		t.Errorf("略語だけの台本で警告した: %v\n%s", err, out)
	}
	// 一覧の「行の中では別の読み」: 単独では崩れた読み・行の中では 1 文字ずつ読まれる IDOR には付き、正しく読めている Windows
	// (行の読みの長音を揃えると、マックトウィンドオズ の トウ が トオ になって印が付いていた) には付かない
	out, _ = run("MacとWindows、どちらが上かという論争よ。", engineLines[1].spoken)
	if !strings.Contains(out, "Windows\t1 行\tウィ'ンドオズ\n") || !strings.Contains(out, "IDOR\t1 行\tイドオ'/ア'アル\t(行の中では別の読み: 1 行目)") {
		t.Errorf("一覧の「行の中では別の読み」の付け方が違う:\n%s", out)
	}
	if err := cmdKana(testEnv(t), nil, "", "metan", nil, true); err == nil {
		t.Error("--script 無しの --check を受け入れた")
	}
}

// 略語の一覧は skill の dir に置き、読めなければ止める (黙って空の一覧で検査しない)
func TestAcronymsFile(t *testing.T) {
	env := testEnv(t)
	m, err := loadAcronyms(env)
	if err != nil || !m["CPU"] || m["IDOR"] {
		t.Errorf("skill の acronyms.json: CPU=%v IDOR=%v err=%v (IDOR は 1 文字ずつ読むと誤読なので載せない)", m["CPU"], m["IDOR"], err)
	}
	env.setSkillDir(t.TempDir())
	if _, err := loadAcronyms(env); err == nil {
		t.Error("acronyms.json が無いのに止まらない")
	}
	must(t, os.WriteFile(filepath.Join(env.SkillDir, "acronyms.json"), []byte(`{"CPU":1}`), 0o644))
	if _, err := loadAcronyms(env); err == nil {
		t.Error("配列でない acronyms.json を受け入れた")
	}
}

// 入口 (dispatch) から --check が届くこと、素の kana --script の出力の形 (行番号 / 話者 / 字幕 (差し替えた行は *) / 読み) を固定する
func TestKanaScriptThroughDispatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		k := map[string]string{"SOLIDWORKSという": "エ'スオオエルアイディイダブリュウオオアアルケエエスト/イウ'", "ソフトです": "ソ'_フトデ_ス", "SOLIDWORKS": "エ'スオオエルアイディイダブリュウオオアアルケエエス"}[r.URL.Query().Get("text")]
		if l, ok := engineLetters[r.URL.Query().Get("text")]; ok {
			k = l
		}
		b, _ := json.Marshal(map[string]any{"kana": k, "accent_phrases": []any{}})
		_, _ = w.Write(b)
	}))
	defer srv.Close()
	path := writeScript(t, map[string]any{"readings": map[string]any{"ソフト": "ソフト"}, "lines": []any{
		map[string]any{"who": "metan", "text": "SOLIDWORKSという"},
		map[string]any{"who": "zundamon", "text": "ソフトです", "read": "ソフトです"},
	}})
	run := func(args ...string) (string, error) {
		env := testEnv(t)
		var out strings.Builder
		env.Stdout = &out
		err := dispatch(append([]string{"--engine", srv.URL}, args...), env)
		return out.String(), err
	}
	out, err := run("kana", "--script", path)
	if err != nil || out != "0\tmetan\t SOLIDWORKSという\tエ'スオオエルアイディイダブリュウオオアアルケエエスト/イウ'\n1\tzundamon\t ソフトです\tソ'_フトデ_ス\n" {
		t.Errorf("素の kana --script の出力が変わった: %v\n%q", err, out)
	}
	out, err = run("kana", "--script", path, "--check")
	if err == nil || !strings.Contains(out, "0\tSOLIDWORKS\t") {
		t.Errorf("dispatch から --check が届いていない: %v\n%s", err, out)
	}
}

// 一覧の「行の中では別の読み」: 語だけの読みが行の読みに現れるか。行の読みの長音は揃えない (「マックトウィンドオズ」の
// トウ を トオ に揃えて、正しく読めている Windows に印を付けていた)。長音の書き方の違い (エイ / エエ) は許す
func TestFlexibleKanaInLine(t *testing.T) {
	for _, tc := range []struct {
		alone, line string
		want        bool
	}{
		{"ウィ'ンドオズ", "マ'ッ_クト/ウィ'ンドオズ、ド'チラガ/ウエカト'/イウ'/ロンソオヨ'", true},
		{"ジェ'イ/エ'ス", "ネ'_クスト、ジュ'スト/ユ'ウティイエフ、ハチ'", false}, // Next.js の js は行の中で崩れている
		{"イドオ'/ア'アル", "ア'イディイオオアアルワ、ア'イディイオ/カ_キカエ'ルダケデ", false},
		// 単独の K は ケイ、SOLIDWORKS の行の中では ケエ (どちらも実エンジンの読み)
		{"ケ'イ", "エ'スオオエルアイディイダブリュウオオアアルケエエスト/イウ'", true},
	} {
		if got := flexibleKana(tc.alone).MatchString(stripKanaMarks(tc.line)); got != tc.want {
			t.Errorf("%s in %s = %v (want %v)", tc.alone, tc.line, got, tc.want)
		}
	}
}
