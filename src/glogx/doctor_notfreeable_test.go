package main

import (
	"strings"
	"testing"
	"time"

	"doctor/disk"
)

// 🚨 「解放可能」の合計は **disk.SumDeletable とは別に glogx 側にも実装がある**
// (キャッシュ → 起動時トースト)。あちらだけ直しても、トーストには効かない。
//
// 実害 (敵対レビュー 2 周目 2026-09-04): `prune では .raw は縮まない` と書いた
// Docker の 1GB が「N 解放できます」に混ざり、上位 2 件の内訳に名指しで出ていた。
func TestNotFreeableIsExcludedFromToastTotal(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.Local)
	// 実在の ID を使う (sanitizeDiskCache がカタログに無い ID を落とすため)
	const freeableID, notFreeID = "swiftpm-cache", "docker-vm-disk"
	if e, ok := disk.CatalogEntry(notFreeID); !ok || !e.NotFreeable {
		t.Fatalf("%s に NotFreeable が付いていない (この検査の前提が崩れている)", notFreeID)
	}
	if e, ok := disk.CatalogEntry(freeableID); !ok || e.NotFreeable {
		t.Fatalf("%s は解放可能なエントリのはず", freeableID)
	}
	// 🚨 2 つのサイズは**別の数**にする。同じにすると取り違えの変異が緑で通る
	rep := disk.Report{ScannedAt: now, Results: []disk.Result{
		// Items が空だと「候補が無くなった」として落とされるので入れておく
		{Entry: disk.Entry{ID: freeableID, Label: "解放できる"}, Status: disk.StatusOK, Size: 30 << 30,
			Items: []disk.Item{{Path: "/a", Size: 30 << 30}}},
		{Entry: disk.Entry{ID: notFreeID, Label: "Docker の VM ディスクイメージ"}, Status: disk.StatusOK, Size: 7 << 30,
			Items: []disk.Item{{Path: "/b", Size: 7 << 30}}},
	}}
	c := doctorCacheFromReport(rep, doctorDiskCache{})
	if c.Total != 30<<30 {
		t.Errorf("キャッシュの合計に NotFreeable を足している: %d (want %d)", c.Total, int64(30)<<30)
	}
	// sanitize (表示の入口) でも同じ基準
	if got := sanitizeDiskCache(c).Total; got != 30<<30 {
		t.Errorf("sanitize 後の合計に NotFreeable を足している: %d", got)
	}
	// トーストの文言と内訳
	toast := doctorStartupToast(c, true, now)
	if toast == "" {
		t.Fatal("トーストが出ない (閾値の前提が変わった?)")
	}
	if strings.Contains(toast, "Docker") {
		t.Errorf("解放可能の内訳に NotFreeable を名指しで出している:\n%s", toast)
	}
	if !strings.Contains(toast, "解放できる") {
		t.Errorf("解放可能なエントリが内訳に出ていない:\n%s", toast)
	}
}
