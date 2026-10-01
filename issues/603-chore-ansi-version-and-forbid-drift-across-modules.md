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

- [ ] 1. x/ansi・bubbletea の版を揃え、版の一致の検査を足す
- [ ] 2. 折り返しの禁止を 4 module に足し、禁止の集合の一致の検査を足す
