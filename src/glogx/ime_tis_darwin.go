//go:build darwin && cgo

// TIS (Text Input Source, HIToolbox) の直接呼び出し。現在ソースの取得と任意ソースへの切替を
// fork なしで行う。切替結果の確認とリトライは ime.go が担当する。
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
// (呼び出し側が切替失敗として扱う)。
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
