//go:build !race

// 全パターン × 全単位の突き合わせは 2,800 万回の照合で、-race を付けると 10 倍遅くなって go test の時間の上限を超える。
// この検査にデータ競争の検出は要らない (worker は結果を mutex の下で集めるだけ) ので、race の run からは外し、Makefile の test が race なしで別に回す。

package pyre

import (
	"maps"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"yomiyasu/oracle"
)

const corpusDir = "../../testdata/corpus"

type unit struct {
	key  string
	text Text
}

// corpusUnits は記録の単位 (ファイルごとの全文と、"\n" で割った各行) を記録と同じ順に作る。
func corpusUnits(t *testing.T, header []oracle.CorpusFile) []unit {
	t.Helper()
	corpus, err := oracle.LoadCorpus(corpusDir)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(corpus.Files, header) {
		t.Fatalf("入力の集まりが記録を取ったときと違う (%d ファイルと %d ファイル)。make golden で記録を作り直す", len(corpus.Files), len(header))
	}
	var units []unit
	for _, f := range corpus.Files {
		body, err := corpus.Read(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		units = append(units, unit{oracle.UnitKey(f.Path, -1), NewText(body)})
		for i, line := range strings.Split(body, "\n") {
			units = append(units, unit{oracle.UnitKey(f.Path, i), NewText(line)})
		}
	}
	if len(units) != corpus.Units() {
		t.Fatalf("単位が %d 個、期待 %d 個", len(units), corpus.Units())
	}
	return units
}

// recordPattern は 1 つのパターンを全単位に当てて、Python の記録と同じ形にする。
func recordPattern(p *Pattern, units []unit) (digest string, matchedUnits, matches int, err error) {
	d := oracle.NewDigest()
	for _, u := range units {
		ms, ferr := p.Finditer(u.text, 0, NoEnd)
		if ferr != nil {
			return "", 0, 0, ferr
		}
		if len(ms) == 0 {
			continue
		}
		matchedUnits++
		matches += len(ms)
		d.Unit(u.key)
		for _, m := range ms {
			d.Match(m.spans...)
		}
	}
	return d.Sum(), matchedUnits, matches, nil
}

// 本家の道具が実際に使った全パターンを、全入力の行と全文に当てた一致 (位置・長さ・グループ) が、Python の re と同じになること。
func TestPatternsMatchPythonRecording(t *testing.T) {
	var g oracle.PatternsGolden
	if err := oracle.ReadGolden(goldenDir, oracle.PatternsFile, &g); err != nil {
		t.Fatalf("make golden で記録を作る: %v", err)
	}
	if len(g.Patterns) < 800 {
		t.Fatalf("パターンの記録が %d 件しかない", len(g.Patterns))
	}
	units := corpusUnits(t, g.Header.Corpus)

	work := make(chan oracle.PatternRecord)
	var mu sync.Mutex
	var failures []string
	var checked, matched int
	var wg sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for rec := range work {
				msg := checkRecord(rec, units)
				mu.Lock()
				checked++
				if msg != "" {
					failures = append(failures, msg)
				} else if rec.Matches > 0 {
					matched++
				}
				mu.Unlock()
			}
		}()
	}
	for _, rec := range g.Patterns {
		work <- rec
	}
	close(work)
	wg.Wait()
	for _, f := range failures[:min(len(failures), 20)] {
		t.Error(f)
	}
	if len(failures) > 0 {
		t.Fatalf("%d / %d パターンが Python と違う", len(failures), checked)
	}
	// 当たりが 1 件もないパターンばかりだと、突き合わせが空振りしていても緑になる
	if matched < 700 {
		t.Fatalf("1 回でも当たったパターンが %d 個しかない (記録は 765 個)", matched)
	}
	t.Logf("%d パターン (うち当たったもの %d) を %d 単位に当てて Python と一致", checked, matched, len(units))
}

func checkRecord(rec oracle.PatternRecord, units []unit) string {
	p, err := Compile(rec.Pattern, rec.Flags)
	if err != nil {
		return rec.Pattern + ": compile できない: " + err.Error()
	}
	if p.Flags() != rec.CompiledFlags || p.Groups() != rec.Groups || !maps.Equal(p.GroupIndex(), rec.GroupIndex) {
		return rec.Pattern + ": flags / groups / groupindex が Python と違う"
	}
	digest, matchedUnits, matches, err := recordPattern(p, units)
	if err != nil {
		return rec.Pattern + ": " + err.Error()
	}
	if digest != rec.Digest || matchedUnits != rec.MatchedUnits || matches != rec.Matches {
		return rec.Pattern + ": 一致が Python と違う (当たった単位 " + strconv.Itoa(matchedUnits) + " と " + strconv.Itoa(rec.MatchedUnits) + "、一致 " + strconv.Itoa(matches) + " と " + strconv.Itoa(rec.Matches) + ")"
	}
	return ""
}
