package check

import (
	"math/big"
	"slices"
	"strings"
	"time"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyops"
	"ja-writing/internal/pyos"
)

// 実行 ID と回 (本家の _new_run_id・_same_inputs・_same_origin・_latest_record・_path_key・_latest_by_path・_link・_was_checked_final)。

const linkSeconds = 6 * 60 * 60 // _LINK_SECONDS (前の記録につなぐのは 6 時間以内)

// next は回の番号に 1 を足したもの (本家の numbers[-1] + 1。字の大きさに上限が無い)。
func (a attemptNo) next() attemptNo {
	n, ok := new(big.Int).SetString(a.digits, 10)
	if !ok {
		n = new(big.Int)
	}
	return attemptNo{digits: n.Add(n, big.NewInt(1)).String()}
}

// nextAttempt は (numbers[-1] + 1) if numbers else 1。
func nextAttempt(runDir string) attemptNo {
	numbers := attemptNumbers(runDir)
	if len(numbers) == 0 {
		return parseAttemptNo("1")
	}
	return numbers[len(numbers)-1].next()
}

// newRunID は _new_run_id (UTC の時刻と 4 バイトの乱数の 16 進。runs/ に同じ名前が無いもの)。
func newRunID(rt Runtime, state string, moment time.Time) string {
	for {
		random, err := rt.RandomHex(4)
		if err != nil {
			panic(failure{err})
		}
		runID := Stamp(moment) + "-" + random
		if _, err := pyos.Stat(state + "/runs/" + runID); err != nil {
			return runID
		}
	}
}

// sameInputs は _same_inputs (記録の Input の並びと今の入力の並びが、順によらず同じか)。current に nil があれば偽。
func sameInputs(recorded pyjson.Value, current []*Input) bool {
	if slices.Contains(current, nil) {
		return false
	}
	items, ok := recorded.([]pyjson.Value)
	if !ok || len(items) != len(current) {
		return false
	}
	for _, item := range items {
		if _, ok := item.(*pyjson.Object); !ok {
			return false
		}
	}
	allFiles := true
	for _, item := range items {
		if !pyops.Equal(pyops.Get(item, "via"), "file") {
			allFiles = false
		}
	}
	for _, item := range current {
		if item.Via != "file" {
			allFiles = false
		}
	}
	key, mine := "text_sha256", func(in *Input) string { return in.TextSHA256 }
	if allFiles {
		key, mine = "sha256", func(in *Input) string { return in.SHA256 }
	}
	theirs := make([]string, len(items))
	for i, item := range items {
		theirs[i] = pyops.Format(pyops.Get(item, key))
	}
	ours := make([]string, len(current))
	for i, item := range current {
		ours[i] = mine(item)
	}
	slices.SortFunc(theirs, pyStrCompare)
	slices.SortFunc(ours, pyStrCompare)
	return slices.Equal(theirs, ours)
}

// pyStrCompare は Python の str の大小 (コードポイントの順。WTF-8 のバイト列の順と同じ)。
func pyStrCompare(a, b string) int { return strings.Compare(a, b) }

// sameOrigin は _same_origin (同じモードで、同じ原文と同じ資料の集まりの記録か)。
func sameOrigin(record *pyjson.Object, mode string, source *Input, materials []*Input) bool {
	inputs, ok := pyops.Get(record, "inputs").(*pyjson.Object)
	if !pyops.Equal(pyops.Get(record, "mode"), mode) || !ok {
		return false
	}
	if mode == "rewrite" && !sameInputs([]pyjson.Value{pyops.Get(inputs, "source")}, []*Input{source}) {
		return false
	}
	return sameInputs(pyops.Get(inputs, "materials"), materials)
}

// latestRecord は _latest_record (実行の中で読める最新の記録。大きい回から探す)。
func latestRecord(state, runID string) *pyjson.Object {
	numbers := attemptNumbers(state + "/runs/" + runID)
	for i := len(numbers) - 1; i >= 0; i-- {
		if record := must(ReadRecord(attemptFile(state, runID, numbers[i]))); record != nil {
			return record
		}
	}
	return nil
}

// pathKey は _path_key (index/by-path のキー。ファイルは最終稿の実際のパス、標準入力は原文 (新規作成では資料) の本文のハッシュ)。
func pathKey(final, source *Input, materials []*Input) string {
	var base string
	switch {
	case final.Via == "file":
		base = must(Place(*final.Path))
	case source != nil:
		base = "stdin:" + source.TextSHA256
	default:
		digests := make([]string, len(materials))
		for i, item := range materials {
			digests[i] = item.TextSHA256
		}
		slices.Sort(digests)
		base = "stdin:" + strings.Join(digests, "")
	}
	return SHA256([]byte(base))
}

// latestByPath は _latest_by_path (index/by-path/<キー>/ の最新の 1 件だけを見る)。
func latestByPath(state, key string) (string, *pyjson.Object) {
	entries := byPathEntries(state, key)
	if len(entries) == 0 {
		return "", nil
	}
	first := entries[0]
	return first.runID, must(ReadRecord(attemptFile(state, first.runID, first.attempt)))
}

// link は _link の結果 (実行 ID と、次に確保を試す回)。
type link struct {
	runID   string
	attempt attemptNo
}

// linkRun は _link (読むだけ)。(結果, "") か (空, 誤りの文)。
func linkRun(rt Runtime, state string, runID *string, newRun bool, mode string, source *Input, materials []*Input, final *Input,
	moment time.Time, cwd string) (link, string) {
	if runID != nil {
		latest := latestRecord(state, *runID)
		if latest != nil && !sameOrigin(latest, mode, source, materials) {
			return link{}, "別の原文の実行IDです（--run-id " + *runID + "）。この実行の記録は、モード、原文、資料の集まりのどれかが違います。" +
				"この原文で検査するときは、--run-id を外すか、別の実行IDを指定してください。"
		}
		return link{*runID, nextAttempt(state + "/runs/" + *runID)}, ""
	}
	if !newRun {
		mustNotBeArmed(state, source)
		if foundID, record := latestByPath(state, pathKey(final, source, materials)); record != nil {
			createdText, _ := pyops.Get(record, "created_at").(string)
			created, ok := ParseISO(createdText)
			recordedCwd, isStr := pyops.Get(record, "cwd").(string)
			if ok && moment.Sub(created).Seconds() <= linkSeconds && isStr && must(SamePath(recordedCwd, cwd)) &&
				sameOrigin(record, mode, source, materials) {
				return link{foundID, nextAttempt(state + "/runs/" + foundID)}, ""
			}
		}
	}
	return link{newRunID(rt, state, moment), parseAttemptNo("1")}, ""
}

// errArmed は start の登録 (状態フォルダの armed/) に触れる run。登録は 5 本目で写すので、今は内部エラーで止める。
type errArmed struct{}

func (errArmed) Error() string {
	return "start の登録（armed/）を使う run はまだ実装していません（ja-writing）"
}

// mustNotBeArmed は、本家の _armed_for_source が登録を探しにいく場面 (原文があり、状態フォルダに armed/ がある) で止める。
// armed/ が無ければ本家も None を返すので、run の答えは変わらない。
func mustNotBeArmed(state string, source *Input) {
	if source != nil && state != "" && isDir(state+"/armed") {
		panic(failure{errArmed{}})
	}
}

// wasCheckedFinal は _was_checked_final (原文の本文が、前に最終稿として検査した本文と同じか)。
func wasCheckedFinal(state string, source *Input) bool {
	for _, item := range indexFound(state, "by-text", source.TextSHA256) {
		final := item.final()
		if item.status != RecordOK || final == nil || !pyops.Equal(pyops.Get(final, "text_sha256"), source.TextSHA256) {
			continue
		}
		var recorded pyjson.Value
		if inputs, ok := pyops.Get(item.recordOrEmpty(), "inputs").(*pyjson.Object); ok {
			recorded = pyops.Get(inputs, "source")
		}
		if o, ok := recorded.(*pyjson.Object); ok && pyops.Equal(pyops.Get(o, "text_sha256"), source.TextSHA256) {
			continue
		}
		return true
	}
	return false
}
