# 608 (perf): Go ツールの起動ごとに go-runewidth v0.0.27 の init が 15〜20 ms かかる (glogx / pro-con / ratelimit)

起票日: 2026-10-02

## 概要

ユーザーの依頼 (2026-10-02): 「src の下で改善できる観点を git log を見て調べ、パフォーマンス・安定性の改善を issue に書いて直して」。

src/ の Go ツールを `GODEBUG=inittrace=1` で起動すると、**init の大半が `github.com/mattn/go-runewidth` 1 つ**だった。
v0.0.27 の `init()` が `strictWidthLUT [2][0x110000]byte` を 1 rune ずつ `runeWidthNoLUT` で埋める (約 220 万回の呼び出し)。
未知のフラグで即終了させても、毎起動これを払う。

| バイナリ (runewidth の版) | runewidth の init | 備考 |
|---|---|---|
| ratelimit (v0.0.27) | 20 ms | UserPromptSubmit hook (`_claude/hooks/ratelimit-warn.sh`) が毎プロンプト起動 |
| glogx (v0.0.27) | 15〜17 ms | tmux popup (C-g) の起動の律速の一部 |
| pro-con (v0.0.27) | 15〜16 ms | カードの CLI として頻繁に起動 |
| schedkeys (v0.0.24) | 0.13 ms | 比較対照: v0.0.24 の init は LUT を作らない |

実測 (2026-10-02、この Mac (darwin/arm64)、go1.26.0、ロードアベレージ 5〜9): `GODEBUG=inittrace=1 <bin> --zz-nonexistent </dev/null`。
init の 2 位以下は glogx / pro-con の chroma (`styles` 約 3.5 ms・`lexers` 約 3 ms)、他は 0.3 ms 未満。
`ratelimit --nonexistent` 全体は約 29〜31 ms で、`/usr/bin/true` (同じ計り方で約 7 ms) との差 約 23 ms のうち 20 ms がこれ。

## 対応方針

- **go-runewidth を v0.0.30 へ上げる** (glogx / pro-con / ratelimit の go.mod。どれも `// indirect`)。
  v0.0.30 は LUT を初回の幅の問い合わせまで遅らせ (upstream issue #104)、遅延構築も区間の塗りつぶし (`fillBytes`) に変えている。
  init では下位 0x300 だけを埋める
- 版を下げる (v0.0.24 へ固定) は採らない: 依存 (x/ansi v0.11.7・bubbletea v2.0.8・ultraviolet は v0.0.23、tuikit は v0.0.24) の
  要求は v0.0.24 以下なので replace なしでも下げられるが、**v0.0.24 は v0.0.27 と幅の値が違う** (反証レビューの実測: 全 rune の
  RuneWidth を比べると幅 0 の rune が 2982 → 4221、ZWJ・国旗・VS16 の絵文字を含む見本の StringWidth が 14 → 15)。
  glogx / pro-con / ratelimit は今 v0.0.27 で描いているので、下げると表示幅が変わる。**v0.0.27 と v0.0.30 は全 rune
  (0..0x10FFFF) の RuneWidth が非 EA・EA とも一致** (同レビューの実測) なので、上げる方は表示を変えない
- tuikit / restartable / schedkeys は v0.0.24 のままで init が軽いので触らない (版を揃えるだけの変更はしない)
- chroma の init (約 6.5 ms) は全 lexer / style の登録で、パッケージの作りに由来する。今回は触らない
  (遅らせるには chroma を使う側を別 package に分けて遅延 import するしかなく、Go には遅延 import が無い。再評価の trigger: popup の起動時間を詰める作業が来たとき)

## 受け入れ条件

- [x] 3 つの go.mod / go.sum を v0.0.30 にし、各 module の `make test` / `make lint` が通る
- [x] inittrace で runewidth の init が 1 ms 未満になる (before / after を下に記録)
- [x] 遅延構築が最初の描画へ移っただけでないかを測る (初回の `RuneWidth` の非 ASCII の問い合わせのコスト)

## 関連ファイル

- `src/glogx/go.mod` / `src/pro-con/go.mod` / `src/ratelimit/go.mod`
- `_claude/hooks/ratelimit-warn.sh` (毎プロンプトの起動元)

## 進捗

- 2026-10-02 「deps(go): go-runewidth を v0.0.30 へ上げ、起動ごとの LUT の init を消す (608)」
  - `go get github.com/mattn/go-runewidth@v0.0.30 && go mod tidy` を glogx / pro-con / ratelimit で。go.sum の差分は runewidth の 2 行だけ (x/ansi 等は動かない)
  - inittrace の runewidth の init (各 3 回): glogx 16 → 0.020〜0.053 ms / pro-con 15〜16 → 0.011〜0.035 ms / ratelimit 20 → 0.012〜0.049 ms
  - 遅延構築: 使い捨てのプログラムで最初の `StringWidth("日本語のテキスト ✅")` を測ると v0.0.30 は 232〜248 µs (v0.0.27 は init 済みなので 9〜32 µs)。
    init の 15〜20 ms が初回の描画へ移るのではなく、約 0.25 ms に縮む。2 回目以降の `RuneWidth` は両版とも数十 ns
  - `make -C src/{glogx,pro-con,ratelimit} test` / `lint` すべて rc=0。`tests/scripts/test_tuikit_consumers_aligned.sh` rc=0 (x/ansi v0.11.7 / bubbletea v2.0.8 で揃ったまま)
  - 残り: なし (chroma の init は上の方針どおり見送り)
