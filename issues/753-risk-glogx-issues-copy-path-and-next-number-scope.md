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

- [x] 1 再現した (ESC[201~ を含むファイル名で y を押すと生のままクリップボードへ入り、通知だけ無害化されていた)。copyText と copyLines が `isCopySafe` で断る — `fix(glogx): issues viewer は制御文字を含むパス・名前をクリップボードへ入れない`
  - 判定はタブだけ通す (group 名にタブを含む実例を生でコピーする既存の決定 `TestIssuesViewGroupParentCopiesRawGroupNameButSanitizesNotice` と両立) — `fix(glogx): コピーを断る判定はタブだけ通す`
  - 敵対レビュー (壊す): 「無害化の結果と一致するか」で判定すると VS16 の絵文字まで断っていた → `IsPlain` に寄せてタブだけ許す — `fix(glogx): 敵対レビューの 3 件 (...)`
- [x] 2 仕様の判断と根拠:
  - 接頭辞付きの形式の番号は数えない (接頭辞ごとの別の列)。手元で接頭辞付きを持つ 3 repo (SnapTrim 21 件・baby-note 8 件・DualNote macOS 7 件) はどれも接頭辞ごとに 001 から振っており、番号のみの最大の方が大きいので、今の結果は変わらない (敵対レビュー (回帰) の数え)。
    接頭辞付きしかない repo では N が 001 を返す (N は `NNN-` 形式の次番号なので意図どおり) — `fix(glogx/issues): NextNumber は接頭辞付きの形式の番号を数えない`
  - issue ディレクトリをまたぐ最大値は意図どおりなので変えない: obaket は issues/README.md の「採番」節で issues と macOS/issues の 4 dir を 1 本の連番にしている (実測: 両 dir に同番号が 10 件あり、README が「クロス dir 衝突」として防いでいる)。FindDirs は深さ 1 までで、my-products の apps/<x>/issues のような独立の列は拾わない。根拠は NextNumber の doc に書いた
- 変異 (すべて red): copyText の検査を消す / copyLines の検査を消す / isCopySafe を無害化との一致へ戻す / Prefix の除外を消す
- 範囲外の記録: browse 側の copyWithToast は isCopySafe を通らない (外部由来の値が入るかは未確認。敵対レビュー (素通り) の未確認)
- `make test` / `make lint` rc=0。敵対レビュー 3 体 (壊す / 素通り / 回帰。read-only・直列) を通した。
