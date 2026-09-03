# usage の codex 枠は allowlist を通らず、キャッシュ由来の Label が無害化されずに描画される

起票日: 2026-09-04
種別: bug (信頼境界 / 表示)
優先度: **P2** (発火には `~/.cache/glog/claude-usage.json` の書き換えが要る。live 取得経路は安全)
出典: audit (leaky-abstraction) 2026-09-03 の索引 [10] → **反証レビューで半分が崩れ、半分が再現された**

## 症状

`usage/render.go: renderWindows` は Claude と codex で拾い方が違う:

```go
for _, label := range defaultOrder {          // Claude: allowlist ("5h" / "7d") で選抜
	if w, ok := s.Find(label); ok { ws = append(ws, w) }
}
for _, w := range s.Windows {
	if w.Source == SourceCodex { ws = append(ws, w) }   // codex: 取得できた枠すべて
}
```

コメントが理由を書いている:

> Claude は defaultOrder による選抜、codex は取得できた枠すべて
> (枠構成がプラン依存で事前に列挙できないため、**ラベルでなく Source で拾う**)

この設計判断自体は妥当だが、副作用として **codex 枠の `Label` は allowlist の固定値ではなく
スナップショットが持つ文字列そのもの**になる。そして `w.Label` は無害化されずに描画へ渡る:

- `RenderLine` — `fmt.Sprintf("%s:%s%d%%(%s)", w.Label, …)`
- `RenderTableGroups` — `padRight(w.Label, labelW)` / 幅計算 `termwidth.Of(w.Label)`
- `RenderDashboard` 経路も同じ `Label` を使う

## 確認したこと (2026-09-04 実測)

1. **live の取得経路は安全**: `usage/codex.go` の `Label: codexLabel(w.WindowDurationMins)` は
   **数値 (分) から組み立てる**ので、外部文字列は入らない。Claude 側 (`usage/usage.go: labelFor`) も
   `defaultOrder` の完全一致でしか描かれない
2. **キャッシュ経路が無防備**: `glogx/usage_cache.go: loadUsageCache` は `json.Unmarshal` した後、
   **鮮度と完全性 (`HasClaude` / `HasCodex` / TTL) しか見ない**。`Label` の検査も置換も無い
3. `usage` パッケージで `termsafe` を通しているのは **`dial.go` の 1 箇所だけ** (別の値)。
   `render.go` は 0 件
4. 反証レビューが、細工した `~/.cache/glog/claude-usage.json` (`Source:"codex"` + OSC 入り `Label`) から
   **`RenderLine` / `RenderTableGroups` / `RenderDashboard` の 3 経路すべてに生で出ることを再現した**

### 出典の主張のうち崩れた部分 (記録)

主張は「`/usage` の出力経由で Label が汚染される」も含んでいたが、**これは実測で崩れた**:
Claude 枠は `defaultOrder` の allowlist + `Snapshot.Find` の完全一致に阻まれて端末に出ない。
`Window.Raw` も非テストコードからの読み出しが 0 件で描画されない。
**成立するのはキャッシュ経由の半分だけ。**

## 発火条件

`~/.cache/glog/claude-usage.json` に `Source:"codex"` の枠を書き、`Label` に OSC / CSI を入れる。
次回の起動でキャッシュが採用されると、そのまま端末へ出る (画面破壊・タイトル書き換え・OSC52)。

**silent に壊れる**。build もテストも lint も通る。

この脅威モデル (キャッシュファイルが一般ユーザー権限で書き換えられる) は
**この repo が既に採用しているもの** — `issues/done/178` / `issues/done/193` が
`doctor-snapshot.json` について同じ前提で境界を引いている。新しい仮定は持ち込んでいない。

## なぜ見落とされたか

`untrusted_display_test.go` には **`TestUsageBoxSanitizesVersion` が既に在る** =
usage は「外部由来の表示」の規律の対象に入っている。にもかかわらず **Label 経路だけが抜けている**。
「このパッケージは対象になっている」ことが、「**このパッケージのどの経路も守られている**」の
根拠として使われた形。

## 対応方針

### 最小

`loadUsageCache` が復元した `Snapshot` の `Label` (と、描画に出る他の文字列) を
**`termsafe.PlainLine` へ 1 回通す**。入口 1 箇所で閉じる。

代案として `renderWindows` / `RenderLine` 側で通す形もあるが、**入口で 1 回**の規約
(`src/glogx/CLAUDE.md`) に合わせるなら復元直後が正しい。

### 変異検証

「OSC を含む `Label` の codex 枠を持つキャッシュを書いて読み込み、`RenderLine` の出力に
`\x1b` が現れない」テストを書き、**無害化を外す変異で red** を確認する。
🚨 fixture は **codex 枠**で作ること — Claude 枠で書くと `defaultOrder` の allowlist に
阻まれて**退行しても最初から不可視**になり、何も守らないテストになる。

### 併せて

`untrusted_display_test.go` に usage の Label 経路を足す (今は Version 経路だけ)。

## 関連

- issue 228 (doctor の live 経路が termsafe を通らない。**同じ「外部由来の表示」規律の別の穴**)
- issue 229 (doctor の snapshot 復元経路が Entry を再束縛しない。**同じ「キャッシュ由来の文字列」の穴**)
- `issues/done/178` / `issues/done/193` — キャッシュファイルの信頼境界の前例
