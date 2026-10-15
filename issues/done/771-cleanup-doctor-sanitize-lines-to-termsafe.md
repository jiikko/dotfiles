# 771 (cleanup): doctor の 4 package の「行の slice を無害化する」関数を termsafe に 1 つにする

起票日: 2026-10-15

## 概要

doctor の 4 package が、同じ「空なら nil を返し、各行に `termsafe.PlainLine` を通す」関数をそれぞれ持っている。termsafe に `PlainLines` を置いて 1 つにする。

## 詳細 (2026-10-15 に grep で確認)

- `doctor/docker/display.go` / `doctor/ssd/display.go` / `doctor/svc/display.go` の `sanitizeDisplayLines`、`doctor/disk/display.go` の `sanitizeLines`
- `termsafe` に slice 版 (`PlainLines`) は今は無い
- 🚨 `doctor/internal/displaycheck` は「関門の右辺が `termsafe.*` / `sanitize*` を呼んでいるか」で無害化を数える (`displaycheck.go` の冒頭の注記)。
  `sanitizingRHS` は `termsafe.<何でも>` の呼び出しを関門として数えるので、`termsafe.PlainLines` も数えられる。置き換えの後、関門を外す変異で red になることを確かめる

## 対応方針

- `termsafe.PlainLines` を足し (空なら nil の契約もテストで固定する)、4 つを置き換える
- termsafe は replace で使われる module なので、使う側の go.mod に変化は無い見込み (着手時に確かめる)

## 関連ファイル

- 上の 4 ファイル / `src/termsafe/` / `doctor/internal/displaycheck/displaycheck.go`
- 調査の全体: 773

## 反証レビュー

- 2026-10-15 (read-only のサブエージェント 1 体): P3 displaycheck が `termsafe.PlainLines` を数えるかは `sanitizingRHS` で確かめられる → 未確認を外した

## 進捗

- [x] 2026-10-15: `termsafe.PlainLines` を足し、docker・ssd・svc の `sanitizeDisplayLines` と disk の `sanitizeLines` を置き換えた (termsafe の README・CLAUDE.md にも足した)
  - commit:「cleanup(doctor): 4 package の行の slice の無害化を termsafe.PlainLines に 1 つにする」
  - 変異 (全部 red): disk の `r.Failures = termsafe.PlainLines(…)` を外す → displaycheck の TestSanitizeForDisplayCoversEveryStringField と TestFormatSanitizesUntrustedText /
    PlainLines の「空なら nil」を外す → TestPlainLines
  - 敵対的レビューは省いた (同じ中身の 4 関数を 1 つにする機械的な置き換え。displaycheck が関門を数え続けることは上の変異で確認)
- make test (統合した木・rebase の後) rc=0 (src/PACKAGES.md の索引の漏れを直した後に test-lint を回し直した)
