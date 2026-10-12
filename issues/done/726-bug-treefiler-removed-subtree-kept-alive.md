# 726 (bug): treefiler で消したフォルダの部分木を Model が掴んだまま回収されない (2 万件で約 14 MiB)

起票日: 2026-10-12

## 概要

子が多いフォルダ (仮想化の閾値 1024 件を超える) を開いた後、そのフォルダをディスクで消して読み直す (`r` かライブ更新) と、消えた部分木の node が
Model から到達できるまま残り、何フレーム進めても GC に回収されない。glogx は同じ dir なら F を閉じても Model を使い回す (`filer_view.go` の `toggle`) ので、
プロセスが終わるまで残る。発見はユーザー依頼「リソースリークの観点でコードスキャン」の監査 (dotfiles-4e が opus で回し、実験で裏を取って検閲した)。
712 (仮想化) で入った構造が主因。起票は 4e の依頼で dotfiles-5f。

## 再現 (監査の実験)

子 2 万件のフォルダを開く → `os.RemoveAll` → `r` (またはライブ更新) → フレームを進めて GC → 消えた node への weak pointer が nil にならない。
HeapInuse は 20.5 MB のまま。下の 4 つの経路を全部外すと 6.3 MB に下がる (約 14 MiB)。経路は reflect の到達探索で、1 つずつ外して確かめた (どれか 1 つでも残ると回収されない)。

## 掴んでいる経路 (4 つ)

1. `virt.go` の `takeVirt` / `resetVirt`: 仮想化のブロックの使い回しの置き場 (`virtPool`) のうち、今回の配置で使わなかったブロックの `real` / `placed` / `drawn` が
   len ごと残る (使い回すときにしか `[:0]` にしない)
2. `kids.go` の `visOf`: 親の覚えの `removed` (作り直しで見せる子から外れた子)。消すのは `ghostCulled` と `realizeVirtual` で、どちらも `stepVirt` (仮想化していた親) しか
   見ないので、仮想化していない親 (子が 1024 件以下) では次に子が変わるまで残る
3. `model.go` の `step` ほか: `kept := m.order[:0]` で詰めた後ろ (監査の実験で cap 23552 に古いポインタ 19822 個)。`realBuf` と `stepVirt` の後ろも同じ形
   (`order` の分は 712 より前から)
4. `virt.go` の `anyRef`: `m.refs` (キーが消えた親)。`clear` されるのは次に仮想化した配置で `anyRef` が呼ばれたときだけ

## 対応方針

- 詰めた slice の後ろのポインタは nil で埋める (`clear(s[len(kept):])`)。`virtPool` は使わなかったブロックも中身を空にする (len を 0 にするだけでは cap の先の
  ポインタが残るので、残った分を nil で埋めるか作り直す)
- `removed` を読むのは次の step の `stepVirt` (直前の step で仮想化した親) だけ (`ghostCulled`・`realizeVirtual`。`reviveAnim` は `virtSeq==0` なら nil)。
  🚨 「使い終わったら捨てる」を「配置を写した時点」と読むと危険 (View の layout で作った `removed` を、View の後の `realizeVirtual` と次の step の `ghostCulled` の両方が読む)。
  捨ててよいのは、その step で仮想化しなかった親の分だけ。`m.refs` は layout ごとの覚えなので、次の layout の始め (今の `refsSeq` で消すのと同じ時点) に消す
- 🚨 保持を外すと描画が変わる経路が無いことを、712 の等価性のテスト (`filer/virt_test.go`) と `TREEFILER_VIRT_SEEDS=300 go test -run TestVirtualizedRandomOps ./filer` で確かめる
- 回帰テスト: weak pointer (`weak.Make`) で「消えた部分木の node が回収されること」を固定し、4 つの経路を 1 つずつ戻す変異で red を確かめる
  (監査の実験のテストを下敷きにする。4e の scratchpad にあったものを 5f の `tmp/` へ写した。回帰テストとして repo に入れる)

## 受け入れ条件

- [x] 消したフォルダの部分木が、読み直しの後に回収される (weak pointer のテスト + 4 つの経路それぞれの変異で red)
- [x] 等価性のテストが 300 本の種で通る

## 関連

- issue 712 (仮想化。主因の構造) / issue 727 / issue 728 (同じ監査の他の指摘)

## 進捗

- 2026-10-12: 起票 (4e の監査の結果。5f が各経路のコードの実在を確かめた)
- 2026-10-12: 反証レビュー (sonnet) で中核は反証できず。監査のテストで再現: 全部を外すと HeapInuse 20.2〜20.9 MB → 6.2〜6.4 MB、1 つずつ残すとどれも回収されない、
  全部を外した後に到達経路は無い。追加の指摘: 検索を閉じた後も `matches` / `origin` が次の `/` まで node を掴む (issue 728 の 3 は「未確認」でなく同じ種類の保持。コードを読んだだけ) /
  `removed` を捨てる時点の条件を方針に書いた
- 2026-10-12: 修正「fix(treefiler): 消したフォルダの部分木と走査の結果を掴み続けない (issue 726 / 727)」(727 と同じ commit)
  - 4 つの経路: 詰めた slice の後ろを nil で埋める `compactTail` (order・realBuf・stepVirt) / `resetVirt` で前の layout のブロックの中身を空に・`refs` を消す /
    その step で仮想化しなかった親の `removed` を `noteStepVirt` で捨てる (`remHolders`)。あわせて同じ仕組みの保持を 2 つ: 検索を閉じたら一致と起点を手放す (`endSearch`。issue 728 の 3) /
    閉じたタイルを残さない (tiles も `compactTail`)
  - 回帰テスト `leak_test.go` (weak pointer)。🚨 最初の版は検査の後で Model を使っておらず、Model ごと回収されて直す前のコードでも緑だった。`runtime.KeepAlive(m)` で直し、
    直す前のコードで red・直した後で緑を確かめた。経路ごとに 1 つずつ戻す変異 7 本でそれぞれ red
  - 敵対レビュー (opus): 描画は壊せず (旧版と新版で全フレームの出力が一致する差分の実験)。P1 は 727 側の 2 乗 (727 に書いた)
  - `TREEFILER_VIRT_SEEDS=300` 緑、`make -C src/treefiler test` / `lint` rc=0。root の `make test` は `_claude` の時間に関わる 2 本 (hook の大きな入力・tool-elapsed) が
    並列の負荷で落ち、単独では通った (今回の変更とは無関係。hook の方は issue 422 の既知の不安定)
