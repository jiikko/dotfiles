package filer

// theme.go は配色の表 (spec §1.1 の ground・§1.2 の accent・§1.3 の熱の色)。設定の板で選ぶ。
//
// 🚨 選んだ配色はパッケージの色の変数 (palette.go の cBg など) へ書き込む (applyTheme)。描くのは UI の 1 本の
// goroutine だけなので競合しない。Model を 2 つ同時に違う配色で描く使い方はしない (glogx も単体も 1 つ)。

type ground struct {
	name                                             string
	bg, bar, pop, text, routeText, flash, dot, muted rgb
	ignored, matchBg, ripple, rippleBg               rgb
	untracked, staged, modified, conflict            rgb
	palettes                                         []string // この ground で選べる熱の色 (先頭が既定)
	paperAccent                                      string   // 紙の ground はアクセントが固定
}

var grounds = []ground{
	{name: "dark", bg: rgb{9, 10, 15}, bar: rgb{17, 19, 29}, pop: rgb{13, 15, 23}, text: rgb{238, 240, 250}, routeText: rgb{238, 240, 250},
		flash: rgb{235, 242, 255}, dot: rgb{255, 58, 58}, muted: rgb{110, 118, 150}, ignored: rgb{214, 216, 224}, matchBg: rgb{92, 70, 22},
		ripple: rgb{255, 200, 150}, rippleBg: rgb{110, 38, 28},
		untracked: rgb{110, 200, 225}, staged: rgb{120, 215, 130}, modified: rgb{240, 200, 90}, conflict: rgb{255, 90, 120},
		palettes: []string{"ember", "magma", "neon", "aurora", "glacier", "sepia", "mono"}},
	paper(ground{name: "parchment", bg: rgb{242, 232, 207}, bar: rgb{226, 212, 178}, pop: rgb{236, 224, 194}, text: rgb{40, 32, 24},
		routeText: rgb{24, 76, 38}, flash: rgb{20, 16, 12}, muted: rgb{130, 118, 100}, ignored: rgb{80, 74, 66},
		palettes: []string{"growth", "ink", "gilded"}, paperAccent: "forest"}),
	paper(ground{name: "vellum", bg: rgb{248, 242, 226}, bar: rgb{229, 223, 208}, pop: rgb{242, 236, 220}, text: rgb{36, 30, 26},
		routeText: rgb{27, 46, 99}, flash: rgb{18, 15, 13}, muted: rgb{153, 147, 136}, ignored: rgb{71, 65, 58},
		palettes: []string{"manuscript", "ink", "irongall"}, paperAccent: "lapis"}),
}

// paper は紙の ground に共通の色 (印・一致・波紋・git の色) を足す。
func paper(g ground) ground {
	g.dot, g.matchBg = rgb{190, 30, 30}, rgb{236, 204, 120}
	g.ripple, g.rippleBg = rgb{190, 70, 20}, rgb{240, 196, 160}
	g.untracked, g.staged, g.modified, g.conflict = rgb{30, 118, 150}, rgb{40, 130, 50}, rgb{176, 120, 0}, rgb{180, 30, 60}
	return g
}

type accent struct{ dim, active, route rgb }

var accents = map[string]accent{
	"indigo": {rgb{33, 39, 68}, rgb{68, 86, 168}, rgb{140, 168, 255}},
	"teal":   {rgb{24, 50, 54}, rgb{44, 124, 126}, rgb{110, 222, 212}},
	"violet": {rgb{45, 33, 68}, rgb{108, 70, 170}, rgb{198, 152, 255}},
	"amber":  {rgb{58, 44, 24}, rgb{150, 108, 40}, rgb{255, 198, 110}},
	"mono":   {rgb{44, 44, 50}, rgb{100, 100, 110}, rgb{212, 212, 222}},
	"forest": {rgb{176, 158, 130}, rgb{96, 120, 70}, rgb{34, 92, 48}},
	"lapis":  {rgb{184, 178, 166}, rgb{98, 113, 155}, rgb{34, 58, 124}},
}

var accentOrder = []string{"indigo", "teal", "violet", "amber", "mono"}

// heatAges は熱の色の 7 つの停止点の年齢 (秒)。
var heatAges = []float64{60, 3600, 86400, 7 * 86400, 30 * 86400, 365 * 86400, 5 * 365 * 86400}

var palettes = map[string][7]rgb{
	"ember":      {{255, 70, 60}, {255, 118, 48}, {255, 172, 64}, {240, 190, 70}, {196, 112, 170}, {108, 118, 196}, {66, 80, 214}},
	"magma":      {{255, 248, 180}, {255, 190, 120}, {250, 120, 96}, {222, 72, 120}, {184, 70, 160}, {146, 86, 196}, {120, 96, 210}},
	"neon":       {{255, 140, 200}, {255, 110, 225}, {215, 120, 255}, {160, 135, 255}, {110, 160, 250}, {70, 170, 225}, {50, 165, 185}},
	"aurora":     {{253, 231, 37}, {170, 220, 50}, {84, 197, 104}, {36, 166, 136}, {44, 136, 156}, {70, 106, 170}, {96, 80, 170}},
	"glacier":    {{180, 240, 255}, {130, 220, 255}, {90, 200, 242}, {60, 170, 228}, {70, 136, 230}, {96, 112, 226}, {118, 100, 214}},
	"sepia":      {{255, 226, 170}, {248, 210, 150}, {234, 188, 118}, {214, 160, 90}, {192, 134, 80}, {170, 114, 78}, {152, 100, 80}},
	"mono":       {{250, 250, 250}, {220, 220, 226}, {190, 190, 198}, {160, 160, 170}, {132, 132, 142}, {108, 108, 118}, {90, 90, 100}},
	"growth":     {{34, 128, 40}, {78, 124, 18}, {140, 116, 0}, {170, 104, 0}, {166, 70, 28}, {120, 64, 36}, {70, 44, 30}},
	"ink":        {{28, 22, 18}, {56, 36, 26}, {88, 50, 24}, {116, 66, 22}, {136, 82, 30}, {146, 94, 40}, {150, 102, 50}},
	"manuscript": {{196, 40, 30}, {186, 74, 14}, {164, 108, 0}, {106, 112, 18}, {38, 106, 60}, {32, 80, 96}, {46, 40, 70}},
	"gilded":     {{160, 108, 0}, {146, 110, 0}, {128, 118, 10}, {104, 112, 20}, {86, 96, 22}, {70, 76, 26}, {54, 58, 28}},
	"irongall":   {{150, 102, 50}, {146, 94, 40}, {136, 82, 30}, {116, 66, 22}, {88, 50, 24}, {56, 36, 26}, {28, 22, 18}},
}

// heatRanges は「いちばん冷たい色になる年齢」(spec §1.3 の heat_range)。
var heatRanges = map[string]float64{"day": 86400, "week": 7 * 86400, "month": 30 * 86400, "year": 365 * 86400, "5y": 5 * 365 * 86400}

func findGround(name string) ground {
	for _, g := range grounds {
		if g.name == name {
			return g
		}
	}
	return grounds[0]
}

// applyTheme は設定の配色をパッケージの色の変数へ書き込む。
func applyTheme(s *Settings) {
	g := findGround(s.Ground)
	cBg, cBar, cPop, cText, cRouteTxt = g.bg, g.bar, g.pop, g.text, g.routeText
	cFlash, cDot, cMuted, cIgnored, cMatchBg = g.flash, g.dot, g.muted, g.ignored, g.matchBg
	cRipple, cRippleBg = g.ripple, g.rippleBg
	gitColor = map[byte]rgb{'?': g.untracked, '+': g.staged, 'M': g.modified, '!': g.conflict}
	acName := s.Accent
	if g.paperAccent != "" {
		acName = g.paperAccent
	}
	ac := accents[acName]
	cAccDim, cAccAct, cAccRoute = ac.dim, ac.active, ac.route
	pal, ok := palettes[s.Palette]
	if !ok || !contains2(g.palettes, s.Palette) {
		pal = palettes[g.palettes[0]]
	}
	scale := heatAges[6] / heatRanges[s.HeatRange]
	for i := range ember {
		ember[i] = stop{heatAges[i] / scale, pal[i]}
	}
}

func contains2(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
