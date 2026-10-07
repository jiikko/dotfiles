# 654 (refactor): zundamon-kaisetsu のファイル構成と重複の整理

起票日: 2026-10-07

## 詳細 (どれも P3。挙動は変えない)

- **mp4.go に性質の違う責務が同居している**: build 全体の流れ (`cmdBuild`)・出力ファイルの置き換え (`partPath` / `replaceKeepingMode` / `fileUmask`。synth.go も使う)・
  外部の道具の探索 (`findChrome` / `pngSize`。show_mermaid.go も使う)・CPU の負荷の待ち・mp4 の組み立て
  - 案: `cmdBuild` を build.go へ、出力の置き換え (+ synth.go の `writeAtomic` / `writeOutput`) を `outfile.go`、`findChrome` / `pngSize` を `chrome.go` へ。
    build.go の wav の入出力 (`parseWav`〜`writeWav`) を `wav.go` へ。良くなるのは探すコストと参照の局所性 (同じパッケージ内の移動で、循環も共有状態の分割も無い)
- **faces.json の読み込みが 2 か所** (`script.go:faceList` / `build.go:faceCredits`)、`facesDir` は 3 回パスを解き直す。`faceCredits` は「確認済み」を前提に失敗を捨てる
- **台本の場所から相対パスを解く処理が 2 か所** (`script.go:facesDir` / `show_image.go:imageRealPath`)。`resolveScriptRelative` にまとめる
- **query.json を読んで decode する処理が 2 か所** (`build.go:assemble` / `synth.go:cmdKana`)
- **`showData.Src` / `Type` の意味が処理の段階で変わる** (parse 後は実パス、`embedShowImages` 後は data URI。mermaid は image に書き換わる)。
  今の呼び出し元は `assemble` だけなので未発火。埋め込み済みの型を分けるかは、触るときに判断する
- **player.html**: 話者名の span を作るコードが 2 か所 / 図解のカードを作る手順が種類ごとに繰り返される (650 の showAt の表と一緒に直す)

## 判断の注記

- `main.go` の argparse の再現 (約 200 行) は変更理由が 1 つなので分けない (監査の結論)
- 台本の行が `map[string]any` のまま渡るのは、Python 版と同じ順に値を引くための意図的な選択 (Script のコメント)。型付きの行に替えるのは見送る

## 関連ファイル

- `src/zundamon-kaisetsu/*.go` / `_claude/skills/zundamon-kaisetsu/templates/player.html`。監査の記録: issue 656

## 進捗

- [x] ファイルの分割 (挙動を変えない commit として単独で) — refactor(zundamon-kaisetsu): mp4.go / build.go / synth.go に同居していた責務をファイルに分ける
- [x] 重複の解消 — refactor(zundamon-kaisetsu): 台本の場所からの相対パスと faces.json の読み込みを 1 つにする

## 結果 (2026-10-07)

- 分割: wav.go / outfile.go / chrome.go を新設し、cmdBuild を build.go へ。移動の前後で package 行と import を除いた全行の多重集合が一致することを確かめた
- 重複: `resolveScriptRelative` (立ち絵の faces と図解の image.src) / `readFacesMeta` (faceList と faceCredits) / player.html の `whoLabel` (書き起こしと字幕)。
  testdata の台本で HTML を作り、headless の Chrome の DOM で話者名が行数ぶん出ることを確かめた
- **見送り (理由)**:
  - query.json の読み込み (assemble / cmdKana): 意味が違う (assemble は読めなければ失敗、cmdKana はエンジンに問い合わせ直す)。共通にできるのは decode の 3 行だけで、寄せても複雑性が下がらない
  - `showData` の段階ごとの型分け: 呼び出し元が assemble の 1 つだけで、今は未発火。図解を assemble 以外から PlayerData に入れる呼び出し元を足すときに分ける
  - player.html の図解のカードを表にする: showAt の分岐は 650 のテストが種類ごとに検査している形で、表にしても分岐の数は変わらない
- 敵対的レビューは省略した (挙動を変えない移動と機械的な置き換え。テストを変えずに go test / make lint が通り、描画も実物で確かめた)
