package main

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
)

// 🚨 **照合してから書くまでのあいだに引き継がれても、別人の lock を上書きしないこと** (issue 380)。
//
// 旧版の `Renew` は `readLock` で照合してから **名前で開き直して** `O_TRUNC` + write していた。
// そのあいだに引き継ぎが挟まると、**別人の lock を自分のメタで上書き**し、しかも **nil (成功) を
// 返す**。被害は上書きだけではない: 上書きされた側の次の `Renew` は errNotOwner になり、
// 既定の `--on-lost=kill` が**健全な子を SIGTERM/SIGKILL する**。
//
// いまは照合と書き込みが**同じ fd** に対して行われるので、名前がすり替わっても当たるのは
// 自分が確かめた実体だけ = 窓が縮むのではなく**構造的に消える**。
func TestRenewDoesNotOverwriteLockTakenOverMidway(t *testing.T) {
	l := newTestLocker(t)
	mine, err := l.Acquire(time.Minute, "mine")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	other, err := NewLocker(l.dir, 5*time.Second)
	if err != nil {
		t.Fatalf("NewLocker(other): %v", err)
	}

	var taken *Meta
	fired := false
	old := renewBeforeWriteHook
	renewBeforeWriteHook = func() {
		if fired {
			return
		}
		fired = true
		// 照合を通った後・書く前に、引き継ぎが起きる (break + 別ホストの取得で決定論にする)
		if err := l.Break(); err != nil {
			t.Errorf("Break: %v", err)
			return
		}
		m, err := other.Acquire(time.Hour, "other-host-job")
		if err != nil {
			t.Errorf("other.Acquire: %v", err)
			return
		}
		taken = m
	}
	defer func() { renewBeforeWriteHook = old }()

	// 🚨 **戻り値も assert する** (敵対レビュー P1-2)。旧版は「成功しても失敗してもよい」と
	// 書いて**残存欠陥を仕様として固定していた**。保持していないのに nil を返すと、
	// `__av1ify_lock_still_held` (renew の rc だけを見る) が「保持している」と答え、
	// **出力を公開して元ファイルを削除する**ところまで行く。
	rerr := l.Renew(mine.Token)
	if !fired {
		t.Fatalf("前提: 照合を通っていない (seam が呼ばれていない)")
	}
	if taken == nil {
		t.Fatalf("前提: 引き継ぎが成立していない")
	}
	m, _, err := l.readLock()
	if err != nil {
		t.Fatalf("readLock: %v", err)
	}
	if m == nil {
		t.Fatalf("引き継いだ側の lock が壊れている (中身を読めない)")
	}
	if m.Token != taken.Token {
		t.Fatalf("引き継いだ側の lock を上書きした (二重実行になる): got=%s want=%s", m.Token, taken.Token)
	}
	if !errors.Is(rerr, errNotOwner) {
		t.Fatalf("保持していないのに errNotOwner を返していない (呼び出し側が「保持している」と誤認する): %v", rerr)
	}
}

// 🚨 **照合してから消すまでのあいだに引き継がれても、別人の lock を消さないこと** (issue 380)。
//
// 旧版の `Release` は `os.Remove(l.lockPath())` で、照合から消すまでのあいだに引き継がれると
// **別人の lock を消し**、nil (成功) を返していた。消えた直後に第三者が acquire できるので
// **二重実行に直結する**。
//
// 🚨 ここは `Renew` と違い**構造的には閉じられない** (unlink は名前でしか撃てず、
// 「この inode を消す」原始操作は macOS に無い)。窓は「照合 → Remove」に縮むだけ。
func TestReleaseDoesNotRemoveLockTakenOverMidway(t *testing.T) {
	l := newTestLocker(t)
	mine, err := l.Acquire(time.Minute, "mine")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	other, err := NewLocker(l.dir, 5*time.Second)
	if err != nil {
		t.Fatalf("NewLocker(other): %v", err)
	}

	var taken *Meta
	fired := false
	old := releaseBeforeRemoveHook
	releaseBeforeRemoveHook = func() {
		if fired {
			return
		}
		fired = true
		if err := l.Break(); err != nil {
			t.Errorf("Break: %v", err)
			return
		}
		m, err := other.Acquire(time.Hour, "other-host-job")
		if err != nil {
			t.Errorf("other.Acquire: %v", err)
			return
		}
		taken = m
	}
	defer func() { releaseBeforeRemoveHook = old }()

	rerr := l.Release(mine.Token)
	if !fired {
		t.Fatalf("前提: 照合を通っていない (seam が呼ばれていない)")
	}
	if taken == nil {
		t.Fatalf("前提: 引き継ぎが成立していない")
	}
	// 引き継いだ側の lock が**残っている**こと
	m, _, err := l.readLock()
	if err != nil {
		t.Fatalf("readLock: %v", err)
	}
	if m == nil {
		t.Fatalf("引き継いだ側の lock を消した (直後に第三者が取れる = 二重実行)")
	}
	if m.Token != taken.Token {
		t.Fatalf("引き継いだ側の lock が別物に置き換わっている: got=%s want=%s", m.Token, taken.Token)
	}
	// 呼び出し側には「自分のものではない」と伝わること (nil = 成功で返さない)
	if !errors.Is(rerr, errNotOwner) {
		t.Fatalf("引き継がれていたのに errNotOwner を返していない: %v", rerr)
	}
}

// 対照: 誰も割り込まなければ、Renew は打刻し Release は消す (上の 2 本が
// 「常に何もしない」へ倒れていないこと)。
func TestRenewAndReleaseStillWorkWithoutInterference(t *testing.T) {
	l := newTestLocker(t)
	mine, err := l.Acquire(time.Minute, "mine")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	before, err := os.Stat(l.lockPath())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	// mtime が動いたことを見たいので、打刻の粒度を跨ぐために過去へ打っておく
	past := time.Now().Add(-3 * time.Second)
	if err := os.Chtimes(l.lockPath(), past, past); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	if err := l.Renew(mine.Token); err != nil {
		t.Fatalf("Renew: %v", err)
	}
	after, err := os.Stat(l.lockPath())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !after.ModTime().After(past) {
		t.Fatalf("Renew が mtime を打刻していない: %v", after.ModTime())
	}
	if after.Size() != before.Size() {
		t.Fatalf("Renew が中身の長さを変えた: %d → %d", before.Size(), after.Size())
	}
	m, _, err := l.readLock()
	if err != nil || m == nil || m.Token != mine.Token {
		t.Fatalf("Renew の後に自分の lock が読めない: m=%v err=%v", m, err)
	}
	if err := l.Release(mine.Token); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := os.Stat(l.lockPath()); !os.IsNotExist(err) {
		t.Fatalf("Release の後も lock が残っている: %v", err)
	}
}

// 🚨 **Renew は 0 バイトの窓を開かないこと** (issue 383 の発生源を消す)。
//
// 旧版は `O_TRUNC` で開いてから write していたので、そのあいだ lock は 0 バイトで、
// 読み手には「中身を読めない lock」として見えた (fail-closed 側で引き継ぎが止まる)。
//
// 🚨 **検出しない形** (敵対レビュー 380 P1-3。射程を先に書く): この検査の観測点は
// `renewBeforeWriteHook` = **書き込みの前**なので、**`Seek`/`Write` より後ろに置かれた
// truncate は原理的に見えない**。実測で、hook の直後に `f.Truncate(0)` を入れる変異は
// 全テスト緑のまま通った。いま 0 バイト窓の発生源が構造的に無いのは、この検査ではなく
// **読んだバイト列を verbatim で書き戻す** (長さが変わらないので truncate を呼ばない) から。
// `TestRenewPreservesUnknownFields` の長さ assert がそちらを守っている。
func TestRenewNeverLeavesLockEmpty(t *testing.T) {
	l := newTestLocker(t)
	mine, err := l.Acquire(time.Minute, "mine")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	// 「照合を通った後・書く前」に読み手が居たら何が見えるか
	var sawSize int64 = -1
	fired := false
	old := renewBeforeWriteHook
	renewBeforeWriteHook = func() {
		if fired {
			return
		}
		fired = true
		if fi, err := os.Stat(l.lockPath()); err == nil {
			sawSize = fi.Size()
		}
	}
	defer func() { renewBeforeWriteHook = old }()

	if err := l.Renew(mine.Token); err != nil {
		t.Fatalf("Renew: %v", err)
	}
	if !fired {
		t.Fatalf("前提: seam が呼ばれていない")
	}
	if sawSize <= 0 {
		t.Fatalf("書く前の時点で lock が %d バイトになっている (0 バイトの窓が開いている)", sawSize)
	}
}

// 🚨 **Renew は未知フィールドを落とさないこと** (敵対レビュー 380 P2-1b)。
//
// 旧版は読んだバイト列ではなく `json.Marshal(m)` を書き戻していたので、**旧バイナリが
// 新バイナリの書いた lock を renew するたびに未知フィールドが永久に消えた**。
// 共有 SMB の lock dir を複数マシンが使う = 混在版が既定なので、これは現実的な経路。
// `Version: "lockman/1"` を持つ設計とも矛盾する。
//
// 副次: verbatim なら書き戻す長さが必ず一致するので、`Seek → Write → Truncate` の途中が
// **新旧の混ざった JSON** になる窓 (P2-1) も同時に消える。
func TestRenewPreservesUnknownFields(t *testing.T) {
	l := newTestLocker(t)
	mine, err := l.Acquire(time.Minute, "mine")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	// 「新しい版が書いた lock」を模す: 既知フィールドはそのまま、未知フィールドを足す
	raw, err := os.ReadFile(l.lockPath())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	obj["future_field"] = "v2-only"
	withFuture, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(l.lockPath(), withFuture, lockFileMode); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := l.Renew(mine.Token); err != nil {
		t.Fatalf("Renew: %v", err)
	}

	after, err := os.ReadFile(l.lockPath())
	if err != nil {
		t.Fatalf("ReadFile(after): %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(after, &got); err != nil {
		t.Fatalf("Renew の後に JSON として壊れている: %v (%q)", err, string(after))
	}
	if got["future_field"] != "v2-only" {
		t.Fatalf("Renew が未知フィールドを落とした (版が混在する共有では永久に消える): %q", string(after))
	}
	if len(after) != len(withFuture) {
		t.Fatalf("書き戻した長さが違う (verbatim でない): %d → %d", len(withFuture), len(after))
	}
}

// 🚨 **identity の土台を「読んだ実体」から取ること** (敵対レビュー 380 P1-1)。
//
// 初版は `readLock` の後に `os.Lstat` で取り直していた。そのあいだ (中に `io.ReadAll` =
// SMB では 1 往復が入る) に引き継がれると、**照合した対象と guard する対象が別の実体**になり、
// guard が「引き継いだ側の lock を消す許可」として働く = 380 本文の不具合が nil のまま残る。
func TestReleaseIdentityComesFromTheLockItRead(t *testing.T) {
	l := newTestLocker(t)
	mine, err := l.Acquire(time.Minute, "mine")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	other, err := NewLocker(l.dir, 5*time.Second)
	if err != nil {
		t.Fatalf("NewLocker(other): %v", err)
	}
	var taken *Meta
	fired := false
	old := readLockAfterStatHook
	readLockAfterStatHook = func() {
		if fired {
			return
		}
		fired = true
		// readLock の Fstat と ReadAll のあいだ = 初版が identity を取り直していた窓
		if err := l.Break(); err != nil {
			t.Errorf("Break: %v", err)
			return
		}
		m, err := other.Acquire(time.Hour, "other-host-job")
		if err != nil {
			t.Errorf("other.Acquire: %v", err)
			return
		}
		taken = m
	}
	defer func() { readLockAfterStatHook = old }()

	rerr := l.Release(mine.Token)
	if !fired || taken == nil {
		t.Fatalf("前提: 引き継ぎが成立していない (fired=%v taken=%v)", fired, taken)
	}
	m, _, err := l.readLock()
	if err != nil {
		t.Fatalf("readLock: %v", err)
	}
	if m == nil {
		t.Fatalf("引き継いだ側の lock を消した (照合した対象と guard する対象が違う)")
	}
	if m.Token != taken.Token {
		t.Fatalf("引き継いだ側の lock が別物: got=%s want=%s", m.Token, taken.Token)
	}
	if rerr == nil {
		t.Fatalf("保持していないのに nil (成功) を返した")
	}
}
