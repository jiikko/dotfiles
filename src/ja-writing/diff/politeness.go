package diff

import (
	"math"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
)

// 群 X: 敬語の度合い (本家の _politeness)。mail だけ。敬語 5 家族の出現数/本文の文数、敬体/(敬体+だ・である) の差が politenessThreshold 以上なら候補。
// 分母 0 の文末は比べない。体言止め・口語を無理に常体とみなさない。件数と割合を記録し、合否には使わない。

const politenessThreshold = 0.20

var (
	// 活用形も数える。長い「させていただく」を先に取り、いただくと二重に数えない。
	mailHonorific = pyre.NewLazy(`(?P<causative>させていただ(?:く|き|け|い|か))|(?P<receive>いただ(?:く|き|け|い|か))`+
		`|(?P<give>くださ(?:る|り|れ|い|っ|ら))|(?P<say>申し上げ(?:る|ま|た|て))|(?P<know>存じま(?:す|した|せん))`, 0)
	politeEnd = pyre.NewLazy(`(?:です|でした|ます|ました|ません|ませんでした)(?:か|ね|よ|よね)?$`, 0)
	plainEnd  = pyre.NewLazy(`(?:だ|だった|である|であった)(?:ね|よ)?$`, 0)
)

// honorificNames は敬語の家族を、一覧に出す順に並べたもの (組の左は正規表現のグループ名)。
var honorificNames = [][2]string{{"causative", "させていただく"}, {"receive", "いただく"}, {"give", "くださる"}, {"say", "申し上げる"}, {"know", "存じます"}}

// politenessMetrics は本家の _politeness_metrics。rate と ratio の none は、分母が 0 のとき (rate は 0.0、ratio は null)。
type politenessMetrics struct {
	sentences, honorificCount, polite, plain int
	counts                                   [5]int
}

func (m politenessMetrics) honorificRate() float64 {
	if m.sentences == 0 {
		return 0
	}
	return float64(m.honorificCount) / float64(m.sentences)
}

func (m politenessMetrics) politeRatio() (float64, bool) {
	if m.polite+m.plain == 0 {
		return 0, false
	}
	return float64(m.polite) / float64(m.polite+m.plain), true
}

func (m politenessMetrics) value() pyjson.Value {
	honorifics := pyjson.NewObject()
	for i, n := range honorificNames {
		honorifics.Set(n[1], int64(m.counts[i]))
	}
	o := pyjson.NewObject()
	o.Set("sentences", int64(m.sentences))
	o.Set("honorifics", honorifics)
	o.Set("honorific_count", int64(m.honorificCount))
	o.Set("honorific_rate", m.honorificRate())
	o.Set("polite_endings", int64(m.polite))
	o.Set("plain_endings", int64(m.plain))
	if ratio, ok := m.politeRatio(); ok {
		o.Set("polite_ratio", ratio)
	} else {
		o.Set("polite_ratio", nil)
	}
	return o
}

func measurePoliteness(bodies []body) politenessMetrics {
	m := politenessMetrics{sentences: len(bodies)}
	text := func(b body) pyre.Text { return pyre.NewText(b.text) }
	for _, b := range bodies {
		for _, match := range mustFinditer(mailHonorific, text(b), 0, pyre.NoEnd) {
			name, _ := match.LastGroup()
			for i, n := range honorificNames {
				if n[0] == name {
					m.counts[i]++
					m.honorificCount++
				}
			}
		}
		end := BareEnd(b.text)
		if politeEnd.MustSearch(end) {
			m.polite++
		}
		if plainEnd.MustSearch(end) {
			m.plain++
		}
	}
	return m
}

// politeness は本家の _politeness。docType は正規化した文書の種類 (空は指定なし)。
func (s *sides) politeness(docType string) *pyjson.Object {
	if docType != "mail" {
		section := reviewSection(nil)
		section.Set("applicable", false)
		section.Set("threshold", politenessThreshold)
		section.Set("metrics", nil)
		return section
	}
	bodies := [2][]body{s.allBodies(0), s.allBodies(1)}
	metrics := [2]politenessMetrics{measurePoliteness(bodies[0]), measurePoliteness(bodies[1])}
	var out []*pyjson.Object
	for _, key := range []string{"honorific_rate", "polite_ratio"} {
		var a, b float64
		var okA, okB bool
		if key == "honorific_rate" {
			a, b, okA, okB = metrics[0].honorificRate(), metrics[1].honorificRate(), true, true
		} else {
			a, okA = metrics[0].politeRatio()
			b, okB = metrics[1].politeRatio()
		}
		if !okA || !okB || math.Abs(a-b)+1e-12 < politenessThreshold {
			continue
		}
		var samples [2]*body
		for side := range 2 {
			for i := range bodies[side] {
				b := bodies[side][i]
				var hit bool
				if key == "honorific_rate" {
					hit = mailHonorific.MustSearch(b.text)
				} else {
					end := BareEnd(b.text)
					hit = politeEnd.MustSearch(end) || plainEnd.MustSearch(end)
				}
				if hit {
					samples[side] = &bodies[side][i]
					break
				}
			}
		}
		detail := pyjson.NewObject()
		detail.Set("old_ratio", a)
		detail.Set("new_ratio", b)
		detail.Set("threshold", politenessThreshold)
		out = append(out, reviewCandidate(key, samples[0], samples[1], detail))
	}
	section := reviewSection(out)
	section.Set("applicable", true)
	section.Set("threshold", politenessThreshold)
	metricsObject := pyjson.NewObject()
	metricsObject.Set("old", metrics[0].value())
	metricsObject.Set("new", metrics[1].value())
	section.Set("metrics", metricsObject)
	return section
}

// PolitenessSection は元の文と書き直した文の、敬語の度合いの節 (JSON の値の形)。docType は文書の種類 (空なら指定なし。mail だけ比べる)。
func PolitenessSection(orig, rw, docType string) (pyjson.Value, error) {
	s, err := newSides(orig, rw)
	if err != nil {
		return nil, err
	}
	return s.politeness(docType), nil
}
