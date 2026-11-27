# shell-errexit-off-in-conditions の根拠

## 起源

vlc-multi-video-player issue 611 (2026-11-27)。App Store のスクリーンショットを撮る道具 (`bin/store-screenshots`) で、画面を作る台本の関数を
`"setup_$shot" || die "画面を作れなかった"` と呼んでいた。関数の中の待ち・確かめ (`vgp-client wait …`) が失敗しても止まらず、
最後の行が成功すれば、レイアウトの違う絵・ポップオーバーの開いていない絵を撮って rc 0 で終わる形だった。通しの実行は 2 回とも緑で、
敵対レビュー (read-only のサブエージェント) が見つけた。`||` を外して素で呼び、止まった画面を ERR の trap で知らせる形に直した。

同じ規範は `_claude/rules-checklists/adversarial-review-own-safeguards.md` の §2 に既にあったが、checklist は「安全機構を新設したとき」にだけ読むもので、
道具を書いている最中には読まれなかった。シェルのファイルに触れたときに読み込まれる rule に置いた (2026-11-27 ユーザー決定)。

## 実測 (2026-11-27。macOS の /bin/bash 3.2.57 と Homebrew の bash 5.3.9 で同じ結果)

```
set -e; f(){ false; echo continued; }; f || echo died                 # continued が出る。died は出ない
set -e; f(){ false; echo continued; }; if f; then echo then; fi       # continued と then が出る
set -e; f(){ false; echo continued; }; rc=0; ( set -e; f ) || rc=$?   # continued が出る。rc=0
set -e; f(){ false; echo continued; }; set +e; ( set -e; f ); rc=$?; set -e   # continued は出ない。rc=1
set -eE; trap "echo ERR" ERR; f(){ false; echo continued; }; f        # ERR が出て、スクリプトは rc 1 で終わる
```

POSIX の規定 (条件の文脈では errexit を無視する) で、bash の不具合ではない。

zsh 5.9 でも、`f || …`・`if f`・`f && …`・`! f` のどれでも f の中の失敗を通す (continued が出る)。違いは `! f`: bash は次の文へ進み、
zsh は f が成功で終わると `! f` が偽になり、そこでスクリプトが rc 1 で終わる (2026-11-27 に実測)。
もう 1 つの違いは EXIT の trap: zsh は、単独の文として呼んだ関数の中の失敗で rc 1 で終わるとき、`trap cleanup EXIT` の cleanup を呼ばなかった (トップレベルの失敗では呼ぶ。bash はどちらでも呼ぶ)。
bash の `out=$(f)` も f の途中の失敗を通した (3.2 と 5.3)。

`source` したファイルの中の行の扱いは bash 3.2 と 5 で違う (`_claude/rules-checklists/adversarial-review-own-safeguards.md` の §2 と、その rationale の issue 583)。この rule の実測は source を使わない形だけ。

## 反証レビュー (2026-11-27。5 観点・read-only のサブエージェント)

- 事実: 実測の 5 行は 3 つのシェルで書いたとおり。zsh の細部の違いを足した
- 重複・衝突: 同じ規範は checklist の §2 にあり (issue 583 の retro で、当時は checklist へ寄せた)、発動点が違うので rule を足す。checklist の `( … ); rc=$?` は
  `set -e` の下ではサブシェルの失敗でスクリプトが終わるので、この rule の書き方に直して参照を付け替えた。`f || die` を勧める記述は無かった
- 機械化: `check-set-e-suppressed` を repo 全体で有効にすると、`set -e` の 40 本に 40 件出て、読んだ 15 件の 8 割以上が条件を 1 つ見るだけの関数だった。
  常時の検査にはせず、書いた後に 1 本へ打つ使い方を rule に書いた。字面を見る hook は、関数が複数の手順かを判定できないので作らない
- 恒久性・置き場所: global・paths 付きで足す。新規に Write するときは書いた後に届く (同じ paths の rule 2 本と同じ弱点)。後の編集とレビューで読まれる
- 使う人の目: 「後片付けをしたい」「外部コマンド 1 つ」の場面が決められなかったので足した。「素で呼ぶ」「条件の中」を定義した

## shellcheck

`shellcheck -o check-set-e-suppressed` が `f || exit 1` に SC2310 を出すことを確かめた (2026-11-27)。既定では無効。
`f || true` のように途中の失敗を通したい形にも出るので、repo 全体で有効にするかは別に決める。
- 2 周目 (事実・使う人の目): zsh の `! f` と EXIT の trap の書き方が言い過ぎだったので、rule を bash の話に限り、zsh は確かめるよう書いた。`$(f)`・`elif`・`until`・`set -e` の無いスクリプトの扱いと、後片付けの 2 つの形を足した
