# 703 (human): 監査の後に残った判断 3 件 (手元の Go の更新・CI の Go の版・claim の解除の確認)

起票日: 2026-10-09
期限: 2026-10-16

監査の issue 691〜702 を直したときに、コードの判断ではなくユーザーの判断が要るものとして残した 3 件。それぞれの出典の issue は done へ移したので、
ここに集めて決着を待つ。

人が要る理由: 1 は手元の環境 (brew) を変える操作、2 は repo 全体の CI の方針、3 は claim の運用の仕様で、どれもコードからは決められない。

## 何を決めてほしいか

1. **手元の Go を上げるか** (done/696 の 10)。手元の go1.26.0 に stdlib の脆弱性が 2 件ある (GO-2026-6088 encoding/xml・GO-2026-4602 os)。
   1.26.6 で両方直る。`bin/treefiler` などは手元の Go でビルドするので、上げれば消える
   - 確かめ方: `go version` / `cd src/treefiler && go run golang.org/x/vuln/cmd/govulncheck@latest ./...` で 2 件が消えること
2. **CI の Go の版をどう決めるか** (done/696 の 10・11・13、done/701 の 6)。CI は go.mod の `go 1.25.0` ちょうどを setup-go が毎 run ダウンロードする
   (約 13 秒)。go1.25.0 にも同じ 2 件がある (1.25.13 / 1.25.8 で直る。GOTOOLCHAIN=go1.25.0 の govulncheck で実測)。選択肢:
   - a) 全 module の go.mod に `toolchain go1.25.13` を足す (setup-go はその版を入れる。ダウンロードは続く)
   - b) workflow の `go-version` を `1.25.x` にする (runner に入っている版を使い、ダウンロードが減る見込み。go.mod の最小版とずれる)
   - c) 今のまま (CI でビルドしたバイナリは配らないので、脆弱性の実害は無い)
3. **issues viewer の `n` の claim の解除を確認するか** (done/698 の 9)。今は他のマシン (別のホスト名) のバナーが付いた claim も確認なしで外せ、
   他人のバナーが付いたまま自分の claim を付けられる (`issues/banner.go` の removeBanner・MoveToSubdir。ClaimBanner はホスト名を持つが照合しない)。
   選択肢: a) 他のホストのバナーなら外す前に y/N を聞く b) 拒否して案内する c) 今のまま (人が見て押すので足りる)

## 決まったら

- 1: 上げたら、この節に `go version` の結果を書く
- 2: a / b なら workflow (`.github/workflows/_go-project.yml`) と go.mod を変える issue を起こす。c なら理由を書いて閉じる
- 3: a / b なら `src/glogx/issues/banner.go` を直す issue を起こす (テストは issues/banner_test.go)。c なら理由を書いて閉じる

## 進捗

- [x] 2 決定 (2026-10-09。ユーザー回答「ci の goversion は 1.26 にしていいよ」): CI の版の正本を `.github/go-version` (`1.26.x`) に置き、
  setup-go の 10 か所がそれを読む。go.mod の `go 1.25.0` (手元の最小の版) は変えない。版のファイルを変えたら走るよう、Go の workflow の paths に足した
- [ ] 1 / 3 の判断待ち
