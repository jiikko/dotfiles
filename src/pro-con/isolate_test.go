package main

import (
	"os"
	"testing"

	"pro-con/wake"
)

// 🚨 socket の逃がし先を本物の /tmp/pro-con-<uid> にしない (t.TempDir の置き場は長く、逃がし先へ倒れる)。
func TestMain(m *testing.M) { os.Exit(wake.RunIsolated(m.Run)) }
