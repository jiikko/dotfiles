# 598 (test): restartable の status / restart の終了コードをテストしていない (obaket の dev-restart が rc 2 に依存している)

起票日: 2026-10-01

出典: restartable の audit (codex のコードスキャン、2026-10-01。[601](601-research-restartable-audit-2026-10-01.md))。Claude がコードで裏を取った。

## 問題

`src/restartable/main.go` の `controlCommand` は応答を終了コードに写す (socket が無い = 2、要求の失敗 / ok:false = 1、時間切れ = 124)。
テストは `main_test.go` の SIGPIPE の 2 本だけで、この写しを検査するものが無い (`control.Call` を直接叩くテストはあるが、CLI の rc は通らない)。
`!response.OK` の判定を外しても、今のテストは赤くならない見込み (変異は未確認)。

## 発火条件

写しの退行 (例: ok:false を rc 0 にする、socket が無いのを rc 1 にする)。obaket の `macOS/bin/dev-restart` は「`restartable status` の rc 2 = ループが動いていない (アプリを止めずに人へ頼む)」に依存しているので、
写しが崩れると dev-restart の判断が黙って変わる。

## 推奨対応

CLI を実際に起動して (テストバイナリの helper か `go run`)、socket が無い / ok:false / ok:true / 時間切れ の各場合の rc と出力を検査するテストを足す。`!response.OK` を外す変異と、rc 2 を 1 にする変異で red を確かめる。

## 確認

テストの走査で確認 (2026-10-01 Claude: main_test.go の Test は SIGPIPE の 2 本だけ)。
