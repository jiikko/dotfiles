package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

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
)

// doctorDiskCache は保存する形。エントリは表示に要る最小限 (トーストの上位 2 件と合計)。
type doctorDiskCache struct {
	ScannedAt      time.Time              `json:"scanned_at"`
	Partial        bool                   `json:"partial"` // Esc で中断した部分結果
	Total          int64                  `json:"total"`   // 今消せる量 (disk.Report.Total)
	Entries        []doctorDiskCacheEntry `json:"entries"` // 占有量の降順
	LastNotifiedAt time.Time              `json:"last_notified_at"`
}

type doctorDiskCacheEntry struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Size   int64  `json:"size"`
	Status string `json:"status"`
}

func doctorDiskCachePath() (string, error) {
	base, err := cachedir.Base()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "doctor-disk.json"), nil
}

// doctorCacheFromReport は保存する形に落とす。候補 0 件のエントリは持たない。
func doctorCacheFromReport(rep disk.Report, prevNotified time.Time) doctorDiskCache {
	c := doctorDiskCache{ScannedAt: rep.ScannedAt, Partial: rep.Partial, Total: rep.Total, LastNotifiedAt: prevNotified}
	for _, r := range rep.Results {
		if r.Status == disk.StatusOK && len(r.Items) == 0 {
			continue
		}
		c.Entries = append(c.Entries, doctorDiskCacheEntry{ID: r.Entry.ID, Label: r.Entry.Label, Size: r.Size, Status: string(r.Status)})
	}
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
	if !ok || c.Total < doctorToastThreshold {
		return ""
	}
	if !c.LastNotifiedAt.IsZero() && now.Sub(c.LastNotifiedAt) < doctorToastCooldown {
		return ""
	}
	text := disk.HumanSize(c.Total) + " 解放できます"
	if tops := doctorTopEntries(c, 2); tops != "" {
		text += " (" + tops + ")"
	}
	text += " — D で doctor を開く"
	if age := now.Sub(c.ScannedAt); age > doctorStaleAfter {
		text += fmt.Sprintf(" (%d 日前の診断)", int(age.Hours()/24))
	}
	return text
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
	return sn, true
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
