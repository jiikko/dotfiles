# 611 (risk): disassemble_excel の xlsb 変換が soffice を利用者の profile で・timeout 無しで起動する

起票日: 2026-10-02

## 概要

`src/disassemble_excel/main.go` の `convertXLSB` は `soffice --headless --convert-to ... --outdir <tmp> <src>` を
`exec.Command` (context・timeout 無し) で起動する。`-env:UserInstallation` を渡していないので、利用者の LibreOffice の
profile を共有する。

LibreOffice の一般に知られた挙動 (**この Mac には LibreOffice が入っておらず未実測**):

- GUI の LibreOffice が起動中だと、`--headless --convert-to` は既存のインスタンスへ処理を渡して何も変換せずに終わる、
  または待ち続けることがある
- 初回起動の profile 作成や profile の lock で止まることがある

今のコードは「変換後のファイルが無い」を検出してエラーにするので、前者は誤った成功にはならない。待ち続ける場合は止まらない。

## 対応方針 (未着手。実測してから)

- `-env:UserInstallation=file://<tmpDir>/profile` で一時 profile を渡す
- `exec.CommandContext` + timeout (数分) + `subproc.WaitDelay` 相当
- 🚨 外部の挙動の記述は仮説 (measure-external-cli-streams-separately)。LibreOffice を入れた環境で、GUI 起動中の変換を
  1 回再現してから直す

## trigger

- LibreOffice のある環境で xlsb を変換する機会ができたとき、または変換が止まった報告が出たとき

## 関連ファイル

- `src/disassemble_excel/main.go` (`convertXLSB` / `findSoffice`)

## 進捗

- 2026-10-02: 起票のみ (この Mac に soffice が無く再現できないため、修正は入れていない)
