//go:build darwin && cgo

// TIS (Text Input Source, HIToolbox) の直接呼び出し。macism の fork (1 本 ≈ 40-60ms) を
// 起動クリティカルパスから外すための in-process 版で、扱うのは安定ケースだけに限る:
//   - 現在ソースの取得 (TISCopyCurrentKeyboardInputSource): read-only で常に安全
//   - ABC (プレーンなキーボードレイアウト) への切替: TISSelectInputSource の安定ケース
//
// ⚠️ CJK 入力メソッド「への」切替 (終了時の復元) はここでやらない: TISSelectInputSource が
// CJK IM 選択で反映されないことがある既知の挙動 (macism が存在する理由そのもの) のため、
// 復元は従来どおり macism fork に委ねる (ime.go の restore)。この分担を崩すときは macism の
// リトライ実装相当を持ち込むこと。
package main

/*
#cgo LDFLAGS: -framework Carbon
#include <stdlib.h>
#include <Carbon/Carbon.h>

static char *glogxTISCurrentSourceID(void) {
	TISInputSourceRef src = TISCopyCurrentKeyboardInputSource();
	if (src == NULL) {
		return NULL;
	}
	CFStringRef sid = (CFStringRef)TISGetInputSourceProperty(src, kTISPropertyInputSourceID);
	char *out = NULL;
	if (sid != NULL) {
		CFIndex len = CFStringGetMaximumSizeForEncoding(CFStringGetLength(sid), kCFStringEncodingUTF8) + 1;
		out = malloc(len);
		if (out != NULL && !CFStringGetCString(sid, out, len, kCFStringEncodingUTF8)) {
			free(out);
			out = NULL;
		}
	}
	CFRelease(src);
	return out;
}

static int glogxTISSelectSourceID(const char *cid) {
	CFStringRef sid = CFStringCreateWithCString(NULL, cid, kCFStringEncodingUTF8);
	if (sid == NULL) {
		return -1;
	}
	CFMutableDictionaryRef filter = CFDictionaryCreateMutable(
		NULL, 1, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	CFDictionaryAddValue(filter, kTISPropertyInputSourceID, sid);
	CFArrayRef list = TISCreateInputSourceList(filter, false);
	CFRelease(filter);
	CFRelease(sid);
	if (list == NULL) {
		return -2;
	}
	int rc = -3; // 該当ソースなし
	if (CFArrayGetCount(list) > 0) {
		TISInputSourceRef src = (TISInputSourceRef)CFArrayGetValueAtIndex(list, 0);
		rc = (int)TISSelectInputSource(src); // noErr = 0
	}
	CFRelease(list);
	return rc;
}
*/
import "C"

import "unsafe"

// tisCurrentSourceID は現在の入力ソース ID を fork なしで返す。取れないときは ok=false
// (呼び出し側が macism fork へ fallback する)。
func tisCurrentSourceID() (id string, ok bool) {
	p := C.glogxTISCurrentSourceID()
	if p == nil {
		return "", false
	}
	defer C.free(unsafe.Pointer(p))
	return C.GoString(p), true
}

// tisSelectSourceID は指定 ID への切替要求を発行する。false = 失敗 (該当ソースなし・
// select エラー)。反映確認は呼び出し側 (ime.go の confirm ループ) が行う。
func tisSelectSourceID(id string) bool {
	cid := C.CString(id)
	defer C.free(unsafe.Pointer(cid))
	return C.glogxTISSelectSourceID(cid) == 0
}
