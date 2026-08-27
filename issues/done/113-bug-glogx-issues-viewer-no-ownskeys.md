# 113 bug: issues viewer に `ownsKeys()` が無く、外側の `U` 横取りが URL ピッカーの契約を破る

起票日: 2026-08-27 / 出典: leaky-abstraction 監査 (L4 capability の嘘) / priority: medium

## 事実

`src/glogx/url_picker.go:urlPicker.handleKey` の doc は明示的にこう宣言している:

> ⚠️ 印字文字はすべて検索語に流す (default 節)。**ここで個別のキーを先に横取りすると、
> その文字を含む URL を検索できなくなる。**

ところが `src/glogx/tui.go` の `handleKey` は、`if m.issuesOv.visible()` の中で
**viewer へ委譲する前に** `if key == "U" { return m, m.toggleUsage() }` を無条件で実行する
(委譲はその 3 行あと)。つまり URL ピッカーに `U` は永久に届かない。

## 非対称

同型の横取りは status viewer 側にもあったが、そちらは `a9f3fa5` で
`statusView.ownsKeys()` を新設して塞がれている (`v.pagerKey != "" || v.discarding`)。
`tui.go` はそれを `if !m.statusOv.ownsKeys()` でガードしている。

**`ownsKeys` の実装は repo 全体で statusView の 1 つだけ** (grep で確認)。
**issues viewer には対の述語が無く、横断確認もされていない。**

## 発火条件と壊れ方

issues viewer の本文で `u` → URL ピッカー → **大文字 `U` を含む URL**
(`github.com/Ueno/...`、クエリ文字列、base64 断片) を絞り込もうとしたとき。
押した文字が検索語に入らず、代わりに残量モーダルが開く。

**silent**。compile error にも lint にもならない。

## 反証の試み (監査が実施済み)

- `docs/issues-viewer-spec.md` の「viewer の上でも `U` は効く」(ユーザー要望 2026-08-01) は
  **横取りすること自体**の根拠であり、**ピッカー入力中も横取りする**とは書いていない。
  同 spec の URL ピッカー節は「打った文字がそのまま絞り込み」と書いており、2 節が突き合わされていない
- `issues/done/071-*.md` の「反証で崩れた」表に本件は無い

## 将来さらに広がる

同 spec は「タイトル検索は未実装。足すときは検索語へ流す形になる」と予告している。
実装した日に `/` の入力中も同じ横取りに当たる。

## 対応

`issuesView` に `statusView` と対の `ownsKeys()` (`urlPick.active || numFilter.typing || markNext.active`)
を足し、`U` 横取りと `usageOv.dismiss()` をそのガードの内側に入れる。
status 側の「ガードは横取りだけに掛ける (委譲には掛けない)」というコメントがそのまま使える。

---

## 対応 (2026-08-27)

`issuesView.ownsKeys()` を新設 (`urlPick.active || numFilter.typing || markNext.active`) し、
`tui.go` の `U` 横取りと `usageOv.dismiss()` をそのガードの内側へ入れた。
**委譲はガードの外に残す** — status 側が実装中に踏んだ「`if` 全体に付けると委譲も飛んで
viewer がキーを受け取れなくなる」罠を避けるため。

### 3 モードの `U` の振る舞い (変更後)

| モード | 変更前 | 変更後 |
|---|---|---|
| URL ピッカー入力中 | usage が開く (**契約違反**) | 検索語に入る ✅ |
| 番号の絞り込み入力中 | usage が開く | 無言の no-op (他の英字と同じ) |
| 「次にやる」の y/N 確認中 | usage が開き、**確認が armed のまま裏に残る** | 確認を取り消す (安全側) ✅ |

絞り込みを**確定した後** (`typing=false, active=true`) は通常のナビゲーションなので `U` は効く。
`active` を見ると「絞り込みを解くまで U が恒久的に死ぬ」。

### 敵対的レビューで直した 3 点

1. **述語 3 節のうち 2 節がテストで守られていなかった**。`numFilter.typing` と
   `markNext.active` を両方削っても、`typing`→`active` の 1 語変異でも**全テスト green**だった
2. **テストの非難メッセージが誤診を出した**。`dismiss()` の位置を変えただけで
   「viewer のキー語彙を外側が奪っている」と報告する形になっていた (実際は委譲は生きていた)。
   主張を 2 つに分けた
3. **起動時グランスの既定を継承していた**。`m.usageOv.visible` をテストが明示設定しないと、
   片方向で vacuous になる
4. **コメントの根拠が false だった**。numFilter を入れる理由に URL ピッカーの契約を引いていたが、
   numFilter は数字しか受けないので `U` が検索語になることは今は無い。
   実際の理由 (spec がタイトル検索を予告しており、実装した日に同じ穴を開け直さないため) に書き直した

### 変異検証 7/7 red

ガードを外す / `ownsKeys` を常に false / 常に true / `urlPick` を落とす /
`numFilter` と `markNext` を落とす / `typing`→`active` / `markNext` だけ落とす。

⚠️ `typing`→`active` の変異は、**テストが数字を打たずに確定していると素通りする**
(空入力の `confirm()` は `clear()` に落ちて `active` も false になるため)。
再現手順どおり `/` → `0` → `Enter` の形にして初めて red になった。

### 固定できなかったもの / latent (記録)

- **`usageOv.dismiss()` がガードの内か外か**は、どちらでも本命の主張 (キーが viewer へ届く) は
  守られる。見た目の差なのでテストは足していない
- **`finishClose` が `markNext` を片付けない** (`urlPick` と `numFilter` は片付ける)。
  現状**到達不能** — `markNextKey` が任意のキーで必ず clear し、確認中に viewer を閉じられる
  キー経路が無い。将来 markNext がキーを消費しない形になるか外部 close 経路が増えると、
  閉じ→開き直しで `ownsKeys()` が true のまま蘇り「U が永久に効かない viewer」ができる
- **「モードへ入る直前に必ず `dismiss()` が走る」が暗黙の不変条件**。usage 表示中に
  `ownsKeys()=true` の状態を人工的に作るとどのキーでも消せなくなるが、モードへ入るキーは
  必ず `ownsKeys()=false` の時点を通るので到達しない (15×15 キーの総当たりでも到達せず)。
  doc もテストも無い
