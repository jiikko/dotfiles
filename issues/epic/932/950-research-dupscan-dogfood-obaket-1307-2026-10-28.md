# 950 (research): dupscan dogfood 所見 — obaket issue 1307 のテスト helper の統合 (2026-10-28)

起票日: 2026-10-28

## 概要

obaket の issue 1307 (テストの類似メソッドの統合) のうち 3 組 (`ViewModelTestContext` から `StorageLocation` を組む 8 つの private helper) を dupscan を使って寄せたときの所見。
**自分の所見だけを書いた記録で、既存 issue (942 / 943 / 948 等) との重複は調べていない。反証レビューも通していない** (観察の記録のため)。

## 使った場面と数字 (obaket、worktree、Swift)

| 場面 | コマンド | 結果 |
|---|---|---|
| 寄せ先の定義を引く | `dupscan def ViewModelTestContext` | 3,404 B (stdout) + 87 B (stderr)。メンバーの一覧と位置が出て、寄せ先の追加位置を決められた |
| 参照を引く | `dupscan refs location` | 4,246 B。出したが**読まなかった** (定義を引いた後は呼び出しの書き方を rg で見たほうが早かった。下の「grep に戻した場面」) |
| 呼び出し元 | `dupscan callers -limit 0 location \| grep -c MoveObjectsUseCaseTests` | 26 行 (その 1 ファイルの分を数えただけ。出力全体の byte 数は測っていない) |
| 実装後 | `dupscan gate -v -base origin/master` | 1 回目 6,755 B (rc=1)、2 回目 (`-v` なし) 4,215 B (rc=0) |

grep / rg に戻した場面: 変更前後の**呼び出しの書き方の種類**を数えるとき (`Self.location(of: context, path: ` や `uploadTarget(context)` の字面ごとの件数)。字面そのものを数える作業で、dupscan の守備範囲ではないと判断した (不満ではない)。

## 役に立った場面

- **`dupscan gate` が、commit の前に新しい重複を止めた**。寄せ先を最初 `ViewModelTestContext` の中に直接書いたところ、shared の `MoveObjectsUseCaseTests.Fixture.location(path:)` と同じ形の新しい組になり、gate が「新しい 1 (止める)」と相手の位置まで出した。`StorageConnection` の拡張に寄せ直して解消 (rc=0)。人の目では気づかない別 target の同形だった
- `gate -v` の「消えた組」で、寄せた結果 (3→0・3→0・2→0) をそのまま確認できた

## 所見

- **F1 (雑音・出力が大きい)**: `gate -v` の出力 6,755 B の大半は「読めないファイル (base)」「読めないファイル (head)」の列挙 (各 20 行 + 「他 37 件」。base と head で同じ 57 個)。この run の関心は「新しい重複」と「消えた組」の数行だけ。base と head で同一なら 1 回にまとめる・差分だけ出す、が考えられる
- **F2 (雑音)**: 今回触っていない `DelayedDebugScreenCredentialRepository` / `DelayedSidebarCredentialRepository` の `fetch` の除外が「効いていない除外 1」として毎回出る。obaket 側の除外が古くなっている可能性がある (未調査。1307 の進捗に観測として書いた)。dupscan 側の問題ではなく obaket の `.dupscan.json` の問題かもしれない

## 提案

- F1: gate の「読めないファイル」を base と head で重複して出さない (同一なら 1 回)。直し方の判断が要るので、ここでは提案だけ

## 見送り

- 特になし
