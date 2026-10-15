# 771 (cleanup): doctor の 4 package の「行の slice を無害化する」関数を termsafe に 1 つにする

起票日: 2026-10-15

## 概要

doctor の 4 package が、同じ「空なら nil を返し、各行に `termsafe.PlainLine` を通す」関数をそれぞれ持っている。termsafe に `PlainLines` を置いて 1 つにする。

## 詳細 (2026-10-15 に grep で確認)

- `doctor/docker/display.go` / `doctor/ssd/display.go` / `doctor/svc/display.go` の `sanitizeDisplayLines`、`doctor/disk/display.go` の `sanitizeLines`
- `termsafe` に slice 版 (`PlainLines`) は今は無い
- 🚨 `doctor/internal/displaycheck` は「関門の右辺が `termsafe.*` / `sanitize*` を呼んでいるか」で無害化を数える (`displaycheck.go` の冒頭の注記)。
  置き換えの後も検査が関門を数えることを、関門を外す変異で red になるかで確かめる (名前の規則で数えているなら、`termsafe.PlainLines` が数えられるかは未確認)

## 対応方針

- `termsafe.PlainLines` を足し (空なら nil の契約もテストで固定する)、4 つを置き換える
- termsafe は replace で使われる module なので、使う側の go.mod に変化は無い見込み (着手時に確かめる)

## 関連ファイル

- 上の 4 ファイル / `src/termsafe/` / `doctor/internal/displaycheck/displaycheck.go`
- 調査の全体: 773
