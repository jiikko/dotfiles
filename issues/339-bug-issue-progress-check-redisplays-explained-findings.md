# 339 bug: `issue-progress-check` が、説明済みの指摘を毎ターン再掲する

- 起票: 2026-09-09
- 種別: `bug`
- 状態: open
- 起源: obaket `issues/757-retro-732-design-and-inventory-2026-09-08.md` 項目 5

## 症状

長いセッションで、Stop hook `_claude/hooks/issue-progress-check.sh` が
**一度「更新不要」と説明した指摘を、以後ほぼ毎ターン再掲する**。

実測 (2026-09-08 の obaket セッション): 同じ 20 数件のリストが **6 回**出た。
そのうち実質的な指摘は 2 件だけで (734 の前提更新 / 732 の棚卸し訂正)、残りは
すべて「このセッションのより前の commit で決着済み」「番号の付け替えだけで状態は変わっていない」
と毎回同じ説明を書き直すことになった。

## 原因 (コードで確認)

dedup が**指摘の集合全体の cksum** で行われている。

```sh
# _claude/hooks/issue-progress-check.sh:116-120
sig=$(printf '%s' "$findings" | cksum | cut -d' ' -f1)
marker="$state_dir/$session_id.reported"
[ -f "$marker" ] && grep -qxF "$sig" "$marker" && exit 0
```

`$findings` はセッション開始 HEAD (`$base`) から現在までの差分で毎回計算し直される。
したがって:

- **commit を 1 つ積むたびに findings の集合が変わる** → cksum が変わる → 「同じ指摘」と判定されない
- 結果、**新しい指摘が 1 件増えるだけで、説明済みの N 件が丸ごと再掲される**
- セッションが長く commit が多いほど再掲が増える (差分の起点が動かないため findings は単調増加しやすい)

コメントは「同じ指摘は 1 セッション 1 回」「指摘の集合が変わったら改めて出す」と書いており、
**実装は仕様どおり**。仕様の方が長いセッションを想定していない。

## 何が困るか

- 同じ説明を毎ターン書き直すコストがかかる (実測 6 回)
- **本当に新しい 1 件が、既知の 20 数件に埋もれる**。今回は拾えたが、埋もれれば見落とす
- block なので、説明を書かないとセッションを閉じられない

## 対応案

**dedup の単位を「集合」から「個々の指摘」へ変える。**

```sh
# 案: finding 1 行ごとに署名を作り、未報告の行だけを出す
while IFS= read -r line; do
  s=$(printf '%s' "$line" | cksum | cut -d' ' -f1)
  grep -qxF "$s" "$marker" 2>/dev/null && continue
  printf '%s\n' "$s" >>"$marker"
  new_findings+="$line"$'\n'
done <<<"$findings"
[ -n "$new_findings" ] || exit 0
```

これで「新しい指摘が出たときだけ、その行だけが出る」になる。

### 検討したが採らない案

- **`base` を進める** (report のたびに開始 HEAD を現在へ) — 「触ったが進捗を書いていない」の
  検出が壊れる。差分の起点が動くと、後から issue を触らずに閉じたケースを見逃す
- **報告回数の上限** — 本当に新しい指摘まで止めてしまう

## 受け入れ条件

- [ ] 一度出た finding 行は、同じセッションで再掲されない
- [ ] **新しい finding 行は、既出の行に埋もれず単独で出る**
- [ ] 既存の検出ロジック (触っていない / `[x]` も進捗見出しも増えていない / 参照元 open issue が未変更) は変えない
- [ ] 変異検証: dedup を「集合」に戻す変異で、上の 1 番目が red になる
- [ ] `tests/claude/test_issue_progress_check.sh` (実在) に、再掲しないこと・新規行が単独で出ることのケースを足す

## 関連

- `_claude/hooks/issue-progress-check.sh` — 実装 (:116-120 が dedup)
- `_claude/hooks/issue-progress-start.sh` — 開始 HEAD の記録
- obaket `issues/757-retro-732-design-and-inventory-2026-09-08.md` — 起源の retro
