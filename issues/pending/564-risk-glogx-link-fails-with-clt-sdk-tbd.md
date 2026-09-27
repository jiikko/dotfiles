# 564 (risk): このマシンで Go の link が SDK の tbd を読めずに落ち、make test の Go の段が赤いまま

起票日: 2026-09-27

> **pending (2026-09-27〜)**: ユーザーの判断で凍結 (Air 1 台のツールチェーンの問題で、手間に見合わない)。
> 再開の trigger: Air で dotfiles の Go を触るとき / 別のマシンでも同じ `tapi error` が出たとき。
> 2026-09-27 時点の確認: kojiM3MBP (macOS 27.0) では `go build` が通り再現しない。
> 自動修復を作るなら「警告を出して `SDKROOT` を Xcode の SDK へ逃がす」が候補。先に Air で逃がし先が通るかを確かめる。
> `CGO_ENABLED=0` で逃がすのは不可 (`ime_tis_darwin.go` の cgo の IME 読み取りが黙って抜ける)。

## 概要

2026-09-27 に KOJIm2-MacBook-Air (macOS 27.0 / Mac14,2) で `make test` を回すと、`test-go` が `glogx [build failed]`、
`tests/bin/test_go_autobuild_warmup.sh` も「glogx: go build に失敗」で落ちた。`src/glogx` で `go build` すると:

```
/usr/bin/clang -arch arm64 ... -framework CoreFoundation -lresolv -framework CoreFoundation -framework Security
ld: multiple errors: tapi error: malformed file
/Library/Developer/CommandLineTools/SDKs/MacOSX27.0.sdk/usr/lib/libresolv.9.tbd:4:20: error: unknown architecture
```

- go 1.26.0 (goenv)。`xcode-select -p` は `/Applications/Xcode.app/Contents/Developer`、CLT は 27.0.0.0.1788430756。エラーのパスは CommandLineTools の SDK
- 同じ日の 19:5x にも再現した。コードの変更ではなく、このマシンのツールチェーン (CLT の SDK と Xcode の ld の組み合わせ) の問題と見ている (未確認)
- 影響: このマシンでは make test が常に赤く、Go の段の退行を見分けられない

## 対応方針 (案)

- `SDKROOT` を Xcode の SDK に向けて link が通るかを試す (`SDKROOT=$(xcrun --sdk macosx --show-sdk-path) go build`)。通れば CLT の SDK の不整合で確定
- 確定したら CLT を入れ直すか、どの SDK を使うかを揃える。同じ組み合わせのマシンで再発するなら、make test の前提の検査に足す

## 受け入れ条件

- [ ] このマシンで `src/glogx` の `go build` が通る
- [ ] 原因 (どの SDK / ld の組み合わせで落ちるか) を本文に書く
