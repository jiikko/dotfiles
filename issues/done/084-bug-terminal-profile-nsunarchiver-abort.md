# 084 bug: terminal_profile_colors.swift の旧 NSUnarchiver フォールバックが捕捉不能 abort になり診断経路が死んでいる

起票日: 2026-08-21
種別: bug
優先度: **P3** (救う対象 (旧形式 .terminal) が repo 内に存在しない。実害は「壊れたファイルで
エラーメッセージの代わりに SIGABRT が出る」)

出典: 監査 [071](071-research-design-audit-2026-08-20.md)。
**出典 issue には「反証で崩れた (却下)」の一覧がある** — 同じ 2 ファイルについての
「色キーの 2 言語二重定義」指摘は**反証で崩れている** (shell 側の 4 arm は Swift 配列の複製ではなく
Terminal.app の sdef が持つ `type="color"` の全列挙。5 つ目は Apple が sdef を拡張するまで
書けない)。**その項目を再提案しないこと**。

## 確認できた事実 (2026-08-21)

`scripts/lib/terminal_profile_colors.swift:23-27`:

```swift
func decodeColor(_ data: Data) -> NSColor? {
    if let c = try? NSKeyedUnarchiver.unarchivedObject(ofClass: NSColor.self, from: data) {
        return c
    }
    return NSUnarchiver.unarchiveObject(with: data) as? NSColor
}
```

- 2 段目の `NSUnarchiver` は **ObjC 例外を投げる**ので Swift 側では捕捉できない
  (`try?` は効かない)。監査のエージェントが Swift 6.2.4 / SDK 26.2 で
  「truncate 済み `streamtyped` blob → SIGABRT」を実測再現している (main agent 未再測)
- その結果 :30-35 の診断メッセージ経路 (`✗ <key> をデコードできない` + `exit 1`) が
  **旧形式の壊れた blob に対してはデッドパス**になる。呼び出し側
  (`terminal_profile_restore.sh`) は非 0 終了を見る作りなので、abort だと診断が出ない
- フォールバックが救う対象 = 旧 NSArchiver (`streamtyped`) 形式の `.terminal`。
  **repo 内の唯一の .terminal (`mac/ClaudeWarm.terminal`) は 4 キーすべて `bplist00`**
  (2026-08-21 に plistlib で確認) なので、1 段目だけで足りている。旧形式は repo 外の
  古い書き出しファイルにしか存在しない

## 対応方針

第一候補は **フォールバックの削除**: 現行 Terminal.app は `bplist00` (NSKeyedArchiver) を書き、
import 検証も旧形式を「ファイルが壊れています」で拒否する (ファイル冒頭のコメントに実測記録あり)。
救う対象が repo 外の古いファイルだけなので、削除すれば「壊れた blob → 診断 + `exit 1`」に揃う。

残す場合は ObjC 側で例外を捕まえる薄いラッパを噛ませる (Swift だけでは閉じない)。
**削除する / 残す のどちらを選んでも、壊れた blob を渡して「診断が出て exit 1」になることを
テストで pin する**こと。この 2 ファイルは `f6c5efd` でテストが入るまで 0 本だったので、
既存テストの被覆を先に見る。

## trigger

Terminal プロファイルの復元経路を次に触るとき。単独でも小さい (削除 + テスト 1 本)。
