# termsafe — 外部由来の文字列を端末へ出す前に無害化する単一の関門

`glogx` と `doctor` が go.mod の `replace termsafe => ../termsafe` で取り込む共有 module。
依存はゼロ (標準ライブラリのみ)。仕様・トレードオフの一次情報は `termsafe.go` の doc コメント。

## なぜ独立 module か

無害化が要るのは 2 つの module にまたがるため:

- **TUI (`glogx`)** — git / CI ログ / issue markdown / 作業ツリーのファイル名。描画層はセル単位に
  分解するので端末制御そのものは落ちるが、**改行で偽の行**が作れ、SGR が次の行へ滲み、
  `y` / `Y` のコピーは `pbcopy` へ生で渡る (貼った先の端末で OSC52 が発火する)
- **CLI (`doctor` の `bin/diskdoctor` / `bin/svcdoctor`)** — **stdout へ直接**書くので後段が無く、
  「表示しただけ」でクリップボード書き込み・タイトル書き換え・画面消去が起きる

`glogx` は `doctor` を replace で取り込んでいるので、`doctor` から `glogx/termsafe` は引けない
(循環)。両方から引ける位置へ出したのが本 module (issue 228)。

## 使い分け

| 関数 | SGR (色) | タブ | 改行 | 使いどころ |
| --- | --- | --- | --- | --- |
| `DetailLine` | 残す | スペース 4 | 落とす | git / CI ログ (`--color` の出力を出す契約がある) |
| `LineKeepTabs` | 残す | 残す | 落とす | git の subject / message (静的出力とのパリティ契約) |
| `PlainLine` | 落とす | スペース 4 | 落とす | **既定**。ファイル名・issue 本文・診断結果の自由文 |
| `PlainLineKeepTabs` | 落とす | 残す | 落とす | 自前でタブストップ揃えをする整形層の入口 |
| `PlainBlock` | 落とす | スペース 4 | **残す** | 1 件が複数行の塊 (brew doctor の警告本文) |
| `IsPlain` | — | — | — | **書き換えず落とす**判定 (同一性を持つ値: パス / ラベル) |

🚨 **`PlainBlock` を「1 件 = 1 行」の場所に使わない**。偽の行を差し込まれて固定高パネルの
行数が狂う (幅を数えるテストは改行を検出しないので素通りする)。

🚨 **同一性を持つ値は書き換えない**。パスやラベルを無害化して表示すると、
「画面に出ているものと、実際に消す / 案内するものが違う」を作る。落とす側へ倒し、
落とした件数を人に見せる (`disk.DisplayablePath` / `svc` の `displayableIdentity` がその形)。

## 通し忘れを機械で止める (issue 251)

関門は `string -> string` なので、**通した値と通していない値を型では区別できない**。
代わりに「関門そのものの網羅性」を検査している。

| 検査 | 何を止めるか |
| --- | --- |
| `doctor/disk/display_coverage_test.go` の `TestSanitizeForDisplayCoversEveryStringField` | **`doctor/disk` の**表示用構造体に新しい文字列フィールドを足したのに `Sanitize*ForDisplay` へ通し忘れる |
| `src/glogx/untrusted_display_test.go` | sink ごとの回帰 (実際に素通しが見つかった経路を固定) |
| `scripts/check_go_project_lanes.sh` | `go.mod` の `replace` 先が dependent の workflow paths に入っているか (= 共有 module を変えた push で CI が走るか) |

🚨 **新しい表示用の構造体を足したら `sanitizeGate` の表にも足すこと**。
表に無い型は検査されない (それ自体は機械では止められない)。

### なぜ「読み手側の lint」ではないか

無害化は**値の生成側 (この module と `doctor/disk`)** で済ませており、読み手 (`glogx` の
`doctor_view.go`) には `termsafe` の呼び出しが 1 つも現れない。
実測 2026-09-04: `doctorRow.text` への代入 35 件のうち、右辺が `termsafe.` なのは **0 件**
(無害化済みの `r.Reason` などをそのまま使う)。**読み手側で構文 lint を書くと 35 件全部が
誤検出になる**ので、案として却下した (issue 251 の案 2 の当初案)。

### 検出しない形 (この形の指摘は採用せず、ここに記録する)

- 構造体の外 (ローカル変数・関数の戻り値・引数) を経由して表示に出る値
- **新しい構造体そのもの**を足したとき (`sanitizeGate` に載っている型しか見ない)
- 無害化の**中身**が正しいか (右辺が `termsafe.*` / `sanitize*` を呼んでいるかまでしか見ない)
- **関門がコピーを無害化して親へ書き戻さない形** (`r.Items = kept` の一文だけを消す等)。
  この形は sink テスト (`glogx/untrusted_display_test.go`) が end-to-end で見る担当
- 🚨 **`doctor/svc` 側の関門は未対応**。`svc.Finding` (Label / PlistPath / Domain / Reasons /
  MissingExec / RestartKeys / BrewFormula / Commands) と `svc.Report` (StatusErr / BrewErr /
  DirErrs) に文字列を足しても誰も止めない。`~/Library/LaunchAgents` には誰でも plist を置ける
  (`svc/display.go` 自身がそう書いている) ので脅威は同じ。**別 issue として起票済み**

いずれも **review の責務**。字句・構文の検査は迂回が原理的に無限にあるので、
「全部塞ぐ」を目標にしない (`_claude/rules/adversarial-review-own-safeguards.md` の §8)。

### 型で持つ案 (`termsafe.Safe`) を採らなかった理由

`PlainLine` が新しい string 型を返す形にすると通し忘れが型で止まるが、
**パッケージ境界で剥がれる**。`glogx/issues` の公開 API は `[]*Issue` / `[]string` を返すので、
1 パッケージだけ型にしても境界で `string` に戻る。効果を出すには表示層 (`[]string` ベースの
描画) まで通す必要があり、影響は production 50 呼び出し + 受け側の全経路になる。
**再提案するなら、この影響範囲を数え直してから**。

