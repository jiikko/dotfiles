# 196 human: ratelimit ダッシュボードの「未消費」カードを、実際に 5h を使っていない時間帯で確認する

起票日: 2026-09-03
期限: 2026-09-10
種別: human
関連: `src/glogx/usage/codex.go` の `parseCodexRateLimits` / `src/glogx/usage/dial.go` の
`unusedFoot` / `textCardBody` / `go run ./tools/dial-preview -unused`

報告 (2026-09-03): 「API が 5h の ratelimit を返さなかった場合 (まだ 5h を使っていない時)、
ダッシュボードの表示が丸ごと無くなる」。codex の rateLimits は未消費の枠を `resetsAt: null` で
返し、パーサがそれを捨てていた。`Window.Unused` を足し、捨てずに「未消費」カードを出すようにした。

## 確認してほしいこと

1. **codex の 5h を使っていない時間帯**に `R` でダッシュボードを開き、codex の段に 5h カードが
   「まだ消費されていません / 0% 未消費 / リセット時刻なし」で出ること (消えない)
2. `U` の一覧 (表) で同じ枠が `未消費 / —` の行として出ること
3. その後 codex を 1 回使うと、次の更新 (毎分) で通常の盤へ戻ること

## Claude 側は未確認

Claude Code の `/usage` が 5h 未消費のときに何を出すかは**確認できていない** (テンプレートが
minify されたバイナリの中で、grep で確定できなかった)。もし Claude の 5h カードも消える現象が
あるなら、そのときの `claude -p /usage --output-format json` の `result` 文字列をこの issue に貼って
ほしい。`Current session` 行が無いのか、`resets` の無い別の文言なのかで直し方が変わる
(前者なら「出所に枠が無い」を未消費と推定するしかなく、後者ならパーサに 1 パターン足せば済む)。
