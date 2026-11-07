package filer

import "sync/atomic"

// stopFlag は裏の goroutine に「止めて」を伝える旗。UI の goroutine が立て、goroutine は次の単位 (フォルダ 1 つ) の前に見る
// (`>` の explodeJob と `/` の finderScan が埋め込む)。
type stopFlag struct{ v atomic.Bool }

func (f *stopFlag) cancel() { f.v.Store(true) }

func (f *stopFlag) canceled() bool { return f.v.Load() }
