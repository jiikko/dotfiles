//go:build !serialrun_testseam

package main

// 本番のバイナリはテストの差し込み口を読まない (環境変数が漏れても、待ちの刻みや ps の置き場が変わらない)。
// テストは TestMain で -tags serialrun_testseam を付けてビルドし、seam_on.go の方を使う

func applyTestSeams(*tunables) {}

func pauseBeforeRename() {}

func psBin() string { return "/bin/ps" }

func noteSeqRetry() {}
