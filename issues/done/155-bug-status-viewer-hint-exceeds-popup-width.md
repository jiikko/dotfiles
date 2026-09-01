# 155 bug: status viewer の hint が popup 実幅で末尾ごと切れている (R/s/q が見えない)

起票日: 2026-09-01

`statusView.hint()` の一覧モードの文字列は **155 桁** (`status_view.go:hint`)。glogx を tmux
popup で使うときの実幅は 84 桁前後 (`tui_helpers_test.go:testPopupWidth = 84`) で、hint は
`browseModel.hintLineText` (`tui.go`) が **末尾から黙って切り捨てる** (`clipToWidth`)。

## 実測 (2026-09-01)

幅 82 (フレーム有効時の clip 幅 = `m.width-2`) で見えるのはここまで:

```
j/k: 移動  Tab: セクション  Space: stage/unstage  a: 全 stage  X: 変更を捨てる  d…
```

⚠️ `clipToWidth` (`render.go`) は**末尾に `…` を付ける**ので、本文に使えるのは 81 桁。
`d:` のコロンまで届かず `d` で切れる (初稿は `d:` と書いていたが誤り。反証レビューで訂正)。

切れて**画面に出ないキー** (`dispWidth` での開始位置: `b` = 100 / `p` = 109 / `R` = 128 /
`s` = 137 / `q` = 148。幅 84 でも全て範囲外):

```
: diff  r: 再読込  b: push  p: pull  U: usage  R: 残量  s: 一覧へ  q: 終了
```

つまり **status viewer から抜ける手段 (`s` = 一覧へ / `q` = 終了) が案内から消えている**。
`b`/`p` (push / pull) と、2026-09-01 に足した `R` (ratelimit ダッシュボードへの横断、`d01c52a`)
も同様に見えない。

## なぜ issue にするか (非対称)

issues viewer 側には **`TestIssuesViewHintFitsPopupWidth`** (`issues_view_test.go`) があり、
`popupWidth = testPopupWidth` を超えたら落ちる。実際そのテストがあるおかげで、issues 側は
「`i: 一覧へ` は入れられない」と判断してキーを削り、案内を `--help` と README へ寄せている
(`issues_view.go:hint` のコメントが正本)。

**status viewer にはその制約が無く、テストも無い**。同じ repo の同じ種類の画面で、片方だけ
無検査なので、キーを足すたびに末尾が静かに削られていく (今回の `R` 追加がまさにそれ)。

⚠️ さらに、`status_view_test.go` の既存コメント (`TestStatusHintWordsMatchBehavior` の近く) は
**「hint 行は端末幅でクリップされ、テストの 80 桁では末尾が `…` に落ちる」ことを認めた上で**
`hint()` の戻り値を直接 assert する回避策を取っている。切れている事実は既に知られていて、
「案内が画面に出るか」を守る側だけが無いということ。

## 対応案 (どれを採るかは未決定)

1. **status 側にも幅テストを足し、入らないキーを削る** (issues 側と同じ解決。削ったキーは
   `--help` / README / `docs/status-viewer-spec.md` を正本にする)。⚠️ 削る候補を選ぶ判断が要る
   — 抜ける手段 (`s`/`q`) を残すのが最優先で、`Space`/`a`/`X` の説明語を短縮する余地がある
2. **hint を幅で切らずに畳む** (入らない分を `…` にする / 2 段に分ける)。全画面ビューなので
   最下行を 2 行にする余地はあるが、issues viewer と行数の契約が変わる
3. **幅に応じてキーを落とす** (広い端末では全部、狭いと必須だけ)。実装が最も重い

## 対応 (2026-09-01) — 案 3 (幅に応じて落とす) を採用

`6f52c1c` / `2b122c7`。3 案のうち **3 を採った**理由: 案 1 (キーを削る) は「広い端末でも
案内が減る」ぶん損が大きく、案 2 (2 段) は issues viewer と行数の契約が変わる。案 3 は
glogx に既にある「収まる候補を選ぶ」イディオム (`fitLine` / `fitText`) と同型で、実装も
50 行に収まった。

- `fitHintItems` が幅に収まるところまで**優先度の高い順**に採り、元の並び順で繋ぐ。
  優先度は ①抜ける手段 (`s`/`q`) ②主目的 (`Space`) ③破壊操作と確認 (`X`/`d`) ④移動
  ⑤`Tab`/`a`/`r` ⑥`b`/`p` ⑦`U`/`R`
- 組む側の予算と描画側の clip 幅を `browseModel.hintWidth` の 1 か所へ寄せた。**2 か所に式が
  あったことがこの issue の根**で、片方だけ余白を変えた瞬間にずれる

### 実測 (対応後)

```
w= 60: Space: stage/unstage  X: 変更を捨てる  s: 一覧へ  q: 終了
w= 84: j/k: 移動  Space: stage/unstage  X: 変更を捨てる  d: diff  s: 一覧へ  q: 終了
w=160: 全 13 項目
```

popup の実幅 (84) で**抜ける手段が見える**ようになった (この issue の実害)。

### 変異検証

5 本すべて red。⚠️ 初回は「組む側だけ予算をずらす」変異が **green** だった — 幅 1 点でしか
見ておらず、その幅ではずれ 2 桁が項目間の余白に吸われて表に出なかった。幅の刻みで走査する
形に直して検出 (`2b122c7`)。

### 却下・記録

- **git log 一覧の hint も 163 桁**で同じ状態にある (実測 2026-09-01)。今回は issue の
  スコープ (status viewer) に留めた。`fitHintItems` は再利用できるので、適用するなら
  「一覧のどのキーを優先するか」を決めるだけ。**未対応であることを明記しておく**
- `TestStatusRSwitchesToRatelimitDash` の「hint に `R: 残量` が入る」assert は、幅 200 を
  渡す形へ変えた。popup 実幅では `R` は落ちる (正本は `--help` / README)

## 関連

- `d01c52a` — `R` (ratelimit ダッシュボードへの横断) を hint に足した commit。この issue の
  発見契機。⚠️ `TestStatusRSwitchesToRatelimitDash` は `hint()` の**文字列**に `R: 残量` が
  入ることしか見ておらず、**画面に出るか**は見ていない (この issue が直せば意味を持つ assert)
- `_claude/rules/no-mixed-width-columns-in-terminal-ui.md` — 「桁は合っているが目には合わない」
  同族。こちらは「桁も合っていない」形
- popup の実幅の根拠: `_tmux.conf` の popup 起動が `-w 90%` (端末幅の 9 割)。
  `testPopupWidth = 84` はその代表値
