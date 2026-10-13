# 753 (risk): glogx の issues viewer: コピーするパスの制御文字 / NextNumber が数える番号の範囲

起票日: 2026-10-13

## 概要

2026-10-13 の監査 (記録は 754) で出た、発火を実行で確かめていない 2 件の記録。どちらも監査体がコードを読んで出した結論で、実行していない。

## 詳細

### 1. コピーするパスが制御文字を含んだままクリップボードへ入る (security。未確認)

- 該当: `src/glogx/issues_view.go` の `copyPath` (`iss.Path`) と `copyReference` (`PathFrom`)。画面に出す側は termsafe を通るが、コピーは生のパスを pbcopy へ渡す
- 起こりうる形: PR などで ESC・改行・bracketed paste の終わり (`ESC[201~`) を含むファイル名が repo に入ると、貼った先の端末で解釈されうる。画面では無害化されているので、利用者には見えない
- 監査体によると、doctor 側は「コピーも生のまま乗る」として制御文字を含むパスを落としている。issues viewer にはその扱いが無い
- 直し方の案: 同一性のパスは書き換えない (`src/glogx/CLAUDE.md` の doctor の `Item.Path` と同じ理由)。制御文字を含むパスはコピーを断り、通知で知らせる
- 確かめ方: 制御文字を含むファイル名の issue を一時 repo に置き、`y` のあとクリップボードの中身をバイトで見る

### 2. `NextNumber` が別の採番系と別の issue ディレクトリの番号まで最大値に入れる (仕様の判断が要る)

- 監査体の読解: `Number` は「英字の接頭辞 + ハイフン + 番号」形式のファイル名からも番号部分を返すので、その番号が 120 なら次の番号が 121 になる。複数の issue ディレクトリ (root の issues と macOS/issues など) も 1 つの最大値にまとめる
- doc の「全ディレクトリ」が状態ディレクトリだけの意味か、意図的に複数の issue ディレクトリをまたぐのかは未確認。どちらが正しいかを決めてから直す

## 関連ファイル

- `src/glogx/issues_view.go` (`copyPath` / `copyReference`)・`src/glogx/issues/parse.go` (`NextNumber`)
- 監査の記録: 754

## 進捗

- [ ] 1 を確かめる (実行で再現するか)
- [ ] 2 の仕様を決める
