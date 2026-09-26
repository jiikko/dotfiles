package store

// 所要の記録 (issue 516)。閉じたカード 1 枚を 1 行 (metrics.Row) にして、カードの記録とは別のファイルに溜める。
// カードの記録・書庫は完了から 1 週間で消える (purge.go) が、こちらは MetricsKeep (90 日) まで残す。
//
// 書き手は dispatcher だけ (記録と同じ。426 の決定 1)。失敗モード:
//   - 同じカードの行を 2 度書く (書いた後、書いた印を持つ前に落ちた) → 読む側は同じカードを後の行で上書きする
//   - 末尾が書きかけの行 → 足す前に改行を補う (appendLines)。読む側は読めない行を飛ばし、その数をエラーで返す
//   - 古い行の削除は一時ファイルからの rename (dropLines)。読めない行は消さずに残す (閉じた時刻を読めないので消してよいか示せない)

import (
	"encoding/json"
	"path/filepath"
	"time"

	"pro-con/metrics"
)

// MetricsFile は所要の記録のファイル名 (記録と同じ置き場)。
const MetricsFile = "metrics.jsonl"

// MetricsKeep は所要の記録の行を残す長さ (閉じた時刻から。ユーザーの依頼「3 ヶ月分まで溜まればいい」)。
const MetricsKeep = 90 * 24 * time.Hour

// AppendMetrics は行を足し、ディスクへ書き切ってから返す。
func AppendMetrics(dir string, rows []metrics.Row) error {
	if len(rows) == 0 {
		return nil
	}
	return appendLines(filepath.Join(dir, MetricsFile), rows)
}

// LoadMetrics は所要の記録を書いた順に返す (同じカードは後の行を正とし、位置は最初の行のまま)。無ければ空。
// 読めない行は飛ばし、読めた分と一緒にその数をエラーで返す (LoadArchive と同じ)。
func LoadMetrics(dir string) ([]metrics.Row, error) {
	return loadLatest(filepath.Join(dir, MetricsFile), "所要の記録", func(r metrics.Row) string { return r.Card })
}

// OldMetrics は閉じた時刻から MetricsKeep たった行の数 (PruneMetrics が消す行。消す前に数を出来事へ書くため)。
func OldMetrics(dir string, now time.Time) (int, error) {
	n := 0
	_, err := eachLine(filepath.Join(dir, MetricsFile), func(line []byte) bool {
		if metricExpired(line, now) {
			n++
		}
		return true
	})
	return n, err
}

// PruneMetrics は閉じた時刻から MetricsKeep たった行を消す (dispatcher だけが呼ぶ)。
func PruneMetrics(dir string, now time.Time) error {
	return dropLines(filepath.Join(dir, MetricsFile), func(line []byte) bool { return metricExpired(line, now) })
}

// metricExpired は行が MetricsKeep より古いか。閉じた時刻を読めない行は古いとしない (消してよいか示せない)。
func metricExpired(line []byte, now time.Time) bool {
	var r struct {
		ClosedAt time.Time `json:"closedAt"`
	}
	return json.Unmarshal(line, &r) == nil && !r.ClosedAt.IsZero() && now.Sub(r.ClosedAt) >= MetricsKeep
}
