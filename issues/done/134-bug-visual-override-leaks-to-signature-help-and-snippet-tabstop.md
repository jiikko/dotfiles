# 134 bug: Visual (Kraft) の上書きが signature help の現在引数と snippet tabstop へも漏れている

起票日: 2026-08-28 / 出典: ee5e2b7 (LSP 参照ハイライトの Visual 漏れ修正) の残課題 / priority: low

ee5e2b7 は `LspReferenceText` / `LspReferenceTarget` の漏れだけを断った。**同じ根 (nvim 既定の
`Visual` への link + 本構成の `Visual` 上書き) を持つ漏れが 2 つ残っている**。実害を体感して
いないので priority は low だが、原因と発火経路は実測済みで、直し方は ee5e2b7 と同型。

`_nviminit.lua` の `LspReferenceTarget` 定義の直下と `docs/theme-colors.md` の Kraft 行に
「未対応 (実害待ち)」として記録してある。着手したら両方の記述も直す。

---

## 実測 (2026-08-28、nvim 0.11.5)

`nvim_get_hl` を両 colorscheme 分岐で採取した結果:

| group | 256色 (retrobox = 主環境) | truecolor (gruvbox) |
|---|---|---|
| `LspSignatureActiveParameter` | **`link=Visual` → Kraft 180 (漏れ)** | `link=Search` → reverse (gruvbox が独自定義。漏れなし) |
| `SnippetTabstop` | **`link=Visual` → Kraft 180 (漏れ)** | **`link=Visual` → Kraft 180 (漏れ)** |

つまり **`SnippetTabstop` は両分岐で漏れ、`LspSignatureActiveParameter` は主環境だけ漏れる**。

### 発火経路 (どちらもこの構成で生きている)

- **`LspSignatureActiveParameter`**: blink.cmp が
  `BlinkCmpSignatureHelpActiveParameter` を `{ default = true, link = 'LspSignatureActiveParameter' }`
  で定義している (`blink.cmp/lua/blink/cmp/highlights.lua:43`)。`_nviminit.lua` の blink 設定は
  `signature = { enabled = true }` なので、シグネチャヘルプの float に地色が塗られる
  (描画は `blink/cmp/signature/window.lua:86,96`)。float の前景は `NormalFloat` (ctermfg=187)。
- **`SnippetTabstop`**: nvim native の `vim.snippet` が使う (`runtime/lua/vim/snippet.lua:4`)。
  blink.cmp の既定 preset がそのまま `vim.snippet.expand` / `jump` を呼ぶ
  (`blink/cmp/config/snippets.lua:30,57`)。**バッファ内**の tabstop に塗られるため、前景は
  通常のシンタックス色 = ee5e2b7 で問題になったのと同じ条件。

### コントラスト (xterm-256 実値。termguicolors=off なので端末が使うのはこちら)

- signature help: `NormalFloat` の前景 187 on Kraft 180 = **1.37:1** (gui: `#ebdbb2` on `#D4A27F` = 1.65:1)
- snippet tabstop: 前景がシンタックス色なので **1.10:1 (Type 214) 〜 1.77:1 (Comment 102)**
  (ee5e2b7 の issue 本体と同じ表)

いずれも `tests/nvim/lsp_reference_hl_check.lua` が LspReference 一族に課している 3.0:1 を
大きく下回る。

---

## なぜ ee5e2b7 で直さなかったか

「実害を確認していない」の一点。ee5e2b7 の起点は「Go で `func` にカーソルを置くと読めない」
という**実際に踏んだ症状**で、その範囲 (LspReference 一族) に絞った。この 2 つは同じ計算で
低コントラストになると分かるが、

- signature help は float が出ている間だけ、しかも現在引数 1 個だけに塗られる (面積が小さい)
- snippet tabstop は補完から snippet を展開したときだけ

なので「読めなくて困った」に至っていない。**先回りで色を足すより、踏んだときに踏んだ形で
直すほうがコメントの根拠が強くなる**と判断した。

## 対応方針 (着手するとき)

ee5e2b7 と同型で、gruvbox の意図と衝突しないように分ける:

1. `SnippetTabstop` — 両分岐で漏れているので明示定義する。ただし **`LspReference*` と同じ
   dark1 にすると「参照」と「tabstop」が同じ見え方になる**。tabstop は「次にジャンプする先」
   なので、地色より **下線 or 別トーン**のほうが意味に合う可能性がある。ここは見た目の判断が
   入るので、`_claude/rules/decide-layout-in-sample-renderer-first.md` に従い
   サンプルレンダラで候補を並べてから決める
2. `LspSignatureActiveParameter` — 漏れているのは 256色分岐だけ。truecolor 側の gruvbox は
   `Search` (reverse) を選んでいる。**reverse は前景色を殺さないので思想としては正しい**ため、
   256色側も reverse に寄せるのが素直か、dark1 系の地に揃えるかは 1 と同じ手順で決める
3. どちらを入れても `tests/nvim/lsp_reference_hl_check.lua` の検査対象へ加える
   (`COLOR_GROUPS` に足すだけでは足りない: あのファイルは「LspReference 一族」を名前で
   pin しているので、group 名と発火経路の pin を別に足すか、検査を「Visual を link 経由で
   引いている地塗り group 全部」へ一般化するかを選ぶ)
4. 変異検証 (`_claude/rules/mutation-verify-new-tests.md`) は使い捨て worktree で当てる
5. `_nviminit.lua` の「未対応 (実害待ち)」コメントと `docs/theme-colors.md` の Kraft 行を直す

## 対象外と決めたもの

- **`VisualNOS`** — `Visual` と同義 (Visual だがウィンドウが非アクティブ) なので、Kraft を
  引くのが正しい。漏れではない
- **`Visual` そのものの色を変える** — Kraft は「長時間注視する選択範囲」として意図的に選ばれた
  色 (`docs/theme-colors.md` / `palette.accent.kraft`)。link 先を切るのが正しい方向で、
  基調色を巻き添えにしない

## 関連

- ee5e2b7 — 同根の修正 (`LspReferenceText` / `LspReferenceTarget`)
- `docs/theme-colors.md` の「選択中テキスト」行 — Visual が他 group から link されている罠の入口
- `tests/nvim/lsp_reference_hl_check.lua` — コントラスト 3.0:1 の検査 (両 colorscheme 分岐)


---

## 対応 (2026-09-03)

**着手前に実測で再現した** (nvim 0.11.5)。issue の表と完全に一致:

| group | 256色 (retrobox) | truecolor (gruvbox) |
|---|---|---|
| `SnippetTabstop` | `link=Visual` → Kraft 180 | `link=Visual` → Kraft 180 |
| `LspSignatureActiveParameter` | `link=Visual` → Kraft 180 | `link=Search` → reverse (漏れなし) |

### 方式を用途で分けた (対応方針 1 / 2 の判断)

コントラストを計算して候補を比べた (xterm-256 実値。基準 3.0:1):

| 案 | 最悪コントラスト |
|---|---|
| A 現状 (Kraft 180) | **1.10:1** (Type 214) ✗ |
| B / E dark1 237 | 3.17:1 (Comment 102) ✓ |
| C reverse / D 下線のみ | 地色を塗らないので前景色がそのまま残る (Type 9.24 / Comment 4.74) ✓ |

- **`SnippetTabstop` = dark1 + 下線**。地色だけだと `LspReference` (同じ dark1) と見分けが
  付かない。下線を足して「次に `<Tab>` で飛ぶ先」を示す (`MatchParen` は dark2 + bold なので
  地色で区別できる)
- **`LspSignatureActiveParameter` = reverse**。**truecolor 側の gruvbox が既に採っている方式**
  (`link=Search` の実体が reverse) に 256色側を揃えた。分岐ごとに違う見え方にしない方が、
  後から読む人が驚かない。reverse は前景色を殺さないので float の中で色を失わない

### 検査を「名前の pin」から「Visual を引きうる地塗り group」へ広げた (対応方針 3)

`COLOR_GROUPS` に 2 つを足しただけでは足りなかった。**reverse は地色を持たない**ので、
定義が消えて「地色も reverse も無い」状態へ退行しても、既存の地色検査は素通りする。
`REVERSE_OK` を持たせ、**reverse があること**と**地色を持たないこと**の両方を見る形にした。

⚠️ `REVERSE_OK` に足すのは reverse を**明示定義した** group だけ。link 先が偶然 reverse を
持つ形 (retrobox の `Search` 等) を許すと、link が変わったときに無言で検査が緩む。

### 変異検証 (使い捨て worktree、3 本とも red)

| 変異 | 落ちた assert |
|---|---|
| `SnippetTabstop` の定義を消す | ctermbg が Visual と同じ (180) |
| `LspSignatureActiveParameter` の定義を消す | reverse で定義しているはずだが reverse が無い |
| reverse を外して地色 (Kraft) に変える | 同上 (**方式が混ざる形も落ちる**) |

### ドキュメント

`_nviminit.lua` の「未対応 (実害待ち)」コメントを消し、方式を分けた理由を書いた。
`docs/theme-colors.md` は Kraft 行の「残り 2 つは未対応」を直し、**2 行を新設**した
(snippet の tabstop / シグネチャヘルプの現在引数)。

### 対象外 (issue の記述どおり)

`VisualNOS` は Visual と同義なので Kraft のままが正しい。`Visual` 自体の色も変えない。

## 受け入れ条件

- [x] 両 colorscheme 分岐で `SnippetTabstop` / `LspSignatureActiveParameter` が Visual を引かない
- [x] `tests/nvim/test_lsp_reference_hl.sh` が両分岐で緑 (256色 35 件 / truecolor 23 件)
- [x] 変異検証で red を確認 (3 本)
- [x] `_nviminit.lua` と `docs/theme-colors.md` の「未対応」記述を直した
