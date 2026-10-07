# 654 (refactor): zundamon-kaisetsu のファイル構成と重複の整理

> 🚨 **担当中: dotfiles-4d**（2026-10-07〜）

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

- [ ] ファイルの分割 (挙動を変えない commit として単独で)
- [ ] 重複の解消
