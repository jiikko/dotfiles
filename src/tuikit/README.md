# tuikit — 端末 UI の部品

glogx の issues viewer で作り込んだ「一覧 → 詳細」の画面遷移・演出・幅計算を、別の TUI でも
使えるように切り出した部品集。**描画フレームワークに依存しない** (bubbletea v1 / v2 のどちらでも、
それ以外でも使える)。入出力は「1 要素 = 1 行」の `[]string` と、壁時計で進む小さな状態機械だけ。

![一覧 → 詳細の遷移](examples/listdetail/demo.gif)

(gif は演出を 3 倍に伸ばして撮っている。実速度は `go run ./examples/listdetail` で見る)

## パッケージ

| パッケージ | 中身 | 使いどころ |
|---|---|---|
| `termwidth` | 表示幅の単一情報源 (`Of` / `Truncate` / `Clip` / `PadSpaces` / `FillRight` …) | 行を幅で切る・揃えるときは**必ずここを通す** |
| `widthenv` | 幅モデルが支持しない環境変数 (`RUNEWIDTH_EASTASIAN`) の検出 | 起動時の警告・テストのガード |
| `anim` | `Transition` (開く / 閉じる / 途中で逆再生) / `ScrollGlide` / `CursorGlide` / easing | 開閉演出と「数行ぶんの移動を滑らせる」演出 |
| `layout` | `ComposeDrawer` (一覧の上に詳細を右から重ねる) / `DrawerGeometry` / `SlideIn` / `WindowOffset` / `ClampOffset` / `PadTo` | 画面の合成と窓の計算 |

## 遷移のパターン

### 一覧 → 詳細: 右から滑り込む引き出し

詳細は一覧を全画面で置き換えず、**右外から滑り込む板**として一覧の上に重ねる。左に一覧の端
(番号・状態・カテゴリ) が残るので、「どの一覧のどこから開いたか」が画面から消えない。

```go
var geo = layout.DrawerGeometry{Ratio: 0.8, Extra: 10, MinList: 8, MaxPeek: 18}
var drawer anim.Transition // zero value = 閉じている

// 開く / 閉じる (所要は動き始めるときに渡す)
drawer.Open(now, 112*time.Millisecond)
drawer.Close(now, 112*time.Millisecond)

// 描画: 詳細は開ききった幅 (geo.Target) で整形しておき、演出中は切るだけ
w := layout.DrawerWidth(geo.Target(width), drawer.Openness(now, anim.EaseOutCubic))
screen := layout.ComposeDrawer(listLines, detailLines, w, width, colored)

// tick のたびに: 閉じ切ったら中身を捨てる
if drawer.Settle(now) { detail = nil }
```

- 閉じる動きは開く動きの**逆再生** (同じカーブを反転して通る)。途中で向きを変えても見えている位置は跳ばない
- 開いたまま隣の項目へ送るときは `Open` を呼ばずに中身だけ差し替える (板が動かないので、左に覗く一覧のカーソルが追従して見える)

### 一覧を開く: 上から順に右から流れ込む

```go
p := float64(now.Sub(openedAt)) / float64(175*time.Millisecond)
if p < 1 { screen = layout.SlideIn(screen, p, width, false, 0.35) }
```

入ってくるときは行ごとに開始をずらし (stagger)、終端で減速する。**閉じるときは全行同時・等速**
(`closing=true`)。着地点の無い板に減速を掛けると「もう見えないのに畳まれない時間」になる。

### 数行ぶんの移動を滑らせる

- `ScrollGlide`: 表示 offset を論理 offset へ数フレームで寄せる (本文の半ページスクロール)。連打中は積み上げずに即時へ倒す
- `CursorGlide`: **描画カーソルだけ**を滑らせる (一覧の半ページ移動)。窓は論理カーソルを含む最小の窓のまま動かさないので、Enter などの決定キーは常に着地点へ効く

```go
glide.Start(prev, offset, 6) // 6 フレーム (× tick 16ms ≒ 100ms)
drawn := glide.Offset(offset) // 描画だけこの値を使う。論理 offset は使う側が持つ
if glide.Active() { glide.Advance(offset) } // tick のたびに
```

## 使う側の約束

- **tick は使う側が回す**。`Transition.Animating(now)` / `ScrollGlide.Active()` / `CursorGlide.Active()` の
  どれかが真のあいだは再描画を続ける (tuikit は tick を持たない。フレームワーク非依存にするため)
- **時刻は引数で渡す** (部品の中で `time.Now` を呼ばない。lint が止める)。使う側のテストで時刻を固定できる
- **演出中に来たキーは、先に着地させてから効かせる** (`Finish()` / `Stop()`)。演出の終わりまで入力を待たせない
- **閉じる演出のあいだは中身を捨てない**。捨てるのは `Settle` / `Finish` が `closed=true` を返してから
- 詳細は**開ききった幅で整形**して渡す。途中の幅で整形し直すと毎フレーム折り返しが変わって文字が踊る
- 幅は `termwidth` だけで測る。別の幅ライブラリ (runewidth 等) を混ぜると、枠の計算が端末・locale でずれる

## 不変条件の守り

| 何を | どこで |
|---|---|
| 部品 (`anim` / `layout` / `termwidth` / `widthenv`) は bubbletea を import しない | `.golangci.yml` の depguard |
| 部品の中で `time.Now` / `time.Since` を呼ばない | `.golangci.yml` の forbidigo |
| 幅モデルは 1 系統 (runewidth・uniseg・`ansi.StringWidthWc` を使わない) | `.golangci.yml` の depguard / forbidigo |
| VS16 付きの文字列リテラルを書かない / 2 本目の幅エンジンを使わない | glogx の `own_sources_test.go` 経由の走査 (tuikit も対象) |

CI は `.github/workflows/src_tuikit.yml` (lint + test)。tuikit を変えると glogx の CI も走る
(`src_glogx.yml` の paths に `src/tuikit/**` がある)。

## デモ

```sh
cd src/tuikit
go run ./examples/listdetail          # 実速度で触る
go run ./examples/listdetail -slow 3  # 演出を 3 倍に伸ばす
make demo                             # vhs で examples/listdetail/demo.gif を撮り直す (vhs が要る)
```

撮る手順は [`examples/listdetail/demo.tape`](examples/listdetail/demo.tape)。部品や演出を変えたら撮り直す。

- 🚨 デモの状態記号は ASCII にしてある。`○ ● ✓` のような East Asian Ambiguous の字は、vhs の描画
  (xterm.js) が 2 桁と数えて差分描画の位置がずれ、gif でだけカーソル行の反転が消える (tmux では正しく出る)

## 取り込み方 (dotfiles の中から)

```
// go.mod
require tuikit v0.0.0
replace tuikit => ../tuikit
```
