# 603 (chore): x/ansi の版と、ansi の折り返しの直呼びの禁止が、tuikit の消費者の module ごとにずれている

起票日: 2026-10-01

出典: tuikit の監査 (dependency / lint-from-done、2026-10-01。[604](604-research-tuikit-audit-remaining-types-2026-10-01.md))。数は Claude が `go list -m` と grep で数え直した。

## 1. x/ansi の版のずれ

| module | x/ansi | `ansi.(Hardwrap\|Wrap\|Wordwrap)` の forbidigo |
|---|---|---|
| tuikit | v0.11.7 | 無し (`termwidth/wrap.go` が正当な使い手) |
| glogx | v0.11.7 | 有り |
| pro-con | v0.11.7 | 有り |
| ratelimit | v0.11.7 | 無し |
| restartable | v0.11.7 | 無し |
| schedkeys | v0.11.8 | 無し |

(2026-10-01、`cd src/<m> && go list -m github.com/charmbracelet/x/ansi` と `grep -c 'Hardwrap|Wrap|Wordwrap' src/<m>/.golangci.yml`)

tuikit は幅・切り詰めの単一の出典 (termwidth) を x/ansi v0.11.7 でテストしているが、schedkeys は v0.11.8 で tuikit をビルドする。
ほかに ultraviolet が 2 種類 (tuikit・ratelimit・restartable・schedkeys と glogx・pro-con)、runewidth が 0.0.24 (tuikit・restartable・schedkeys) と 0.0.27 (glogx・pro-con・ratelimit) に分かれている (`go list -m`)。

- 0.11.7 と 0.11.8 の差 (反証レビューが module cache で `diff -r`): 幅計算の差は `width.go` の WcWidth モードの分岐と新設の `wcwidth.go` だけ。tuikit は `ansi.StringWidthWc` を forbidigo で禁止し、幅は GraphemeWidth の 1 系統なので、**今の 0.11.7 / 0.11.8 のずれでは幅・切り詰めの挙動は変わらない**。
  ほかの差 (kitty の Quiet・sixel・mouse・runewidth 0.0.23→0.0.24) も tuikit の経路に入らない。今は壊れる条件が無く、版が次に上がったときの予防として揃える (重要度は低い)
- 直し方: tuikit の ansi (と bubbletea) を schedkeys に揃える。あわせて、tuikit を使う全 module で x/ansi の版が一致することを見る静的検査を tests/ に置く (go.mod を読むだけ。意図して遅らせる restartable のような module は理由つきで除く)

## 2. 折り返しの直呼びの禁止 (issue 590 の再発防止) が 2 module だけ

glogx と pro-con の `.golangci.yml` にだけ forbidigo `^ansi\.(Hardwrap|Wrap|Wordwrap)$` がある。tuikit 自身・ratelimit・restartable・schedkeys には無い (今の production の使用は `termwidth/wrap.go` の 1 か所だけで、違反は 0 件)。
ansi の禁止の一覧が 5 つの `.golangci.yml` にコピーされていて正本が無いのが原因。

- 直し方: 4 module に同じ forbidigo を足す (tuikit は termwidth を exclusions に)。さらに、tuikit を使う全 module の forbidigo の ansi の禁止が同じ集合であることを見る静的検査を tests/ に置く

## 進捗

- [x] 1. x/ansi・bubbletea の版を揃え、版の一致の検査を足す
- [x] 2. 折り返しの禁止を 4 module に足し、禁止の集合の一致の検査を足す

## 結果 (2026-10-01)

- 1: schedkeys を bubbletea v2.0.8 / x/ansi v0.11.7 に下げて、tuikit がテストしている版に揃えた (schedkeys は 8/27 の新設時にその時点の最新を入れただけで、新しい版が要る理由は見当たらなかった)。
  下げた後の ultraviolet・runewidth も tuikit と同じ (20260703 / 0.0.24)。`make -C src/schedkeys test lint` は通る
- 2: forbidigo `^ansi\.(Hardwrap|Wrap|Wordwrap)$` を tuikit・ratelimit・restartable・schedkeys に足した (tuikit は termwidth を exclusions に。restartable は dotfiles-44 の df2869c2 の上に重ねた)。
  変異: 各 module の本番のファイルに `var _ = ansi.Wordwrap(...)` → 4 module とも forbidigo が赤。termwidth の wrap.go では赤にならない (除外が効く)
- 静的検査 `tests/scripts/test_tuikit_consumers_aligned.sh`: tuikit と、go.mod に tuikit を持つ module の x/ansi の版 (`go list -m`) が一致し、全部が折り返しの禁止を持つこと。
  版が読めない・対象が 2 module 未満は失敗。変異: schedkeys の禁止の行を消す / go.mod の x/ansi を v0.11.8 → どちらも ✗ src/schedkeys で赤。`make test` の `[ok]` の行で起動を確認
- 検出しないもの: glogx の幅の禁止 (issue 524) は forbidigo ではなく depguard の説明で持っていて、この検査は折り返しの禁止だけを見る。bubbletea の版の一致は見ていない
