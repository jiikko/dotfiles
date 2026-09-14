package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/user"
	"time"
)

// clockSkewTolerance は「mtime を打刻したのはサーバか」の検算で許す差。
// ネットワーク往復と丸めを吸収する程度に留める (大きくすると検算の意味が消える)。
const clockSkewTolerance = 5 * time.Second

// mustToken は識別子を作る。**小文字 hex に限る**: APFS の既定は case-insensitive なので、
// 大文字小文字だけで区別する識別子は衝突する。
func mustToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand が失敗する環境では、当てずっぽうの id を作るより落ちるほうが安全。
		panic("lockman: 乱数を取得できない: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}

func username() string {
	u, err := user.Current()
	if err != nil {
		return "unknown"
	}
	return u.Username
}

// writeFileSync は書ききって fsync する。fsync まで済ませないと、SMB クライアントの
// write-behind により「書いたのに他経路から見えない」状態が起こりうる。
func writeFileSync(path string, b []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, lockFileMode)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// warnf は人間向けの注意を stderr に出す。stdout は機械が読む面なので汚さない。
func warnf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "lockman: "+format+"\n", args...)
}
