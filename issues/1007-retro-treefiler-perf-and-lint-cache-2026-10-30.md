# 1007 (retro): treefiler の打鍵の遅さ (995・998・1002) と lint の規則・キャッシュ (1005・1006) の振り返り

起票日: 2026-10-30

## 概要

dotfiles-2e のセッションで、CI の runner と paths の更新、treefiler の打鍵の計測の道具 (tfperf) と tick の待ちの修正 (995)、
glogx への横展開 (998)、フォルダの読み込みの非同期化 (1002)、`raw` の直書きを止める ruleguard の規則 (1005)、
golangci-lint の結果キャッシュが ruleguard の規則の中身を鍵に入れない問題 (1006) をやった。
ここには、どこで踏んだかと、他の作業でも効く改善だけを書く。

## どこで踏んだか

1. **lint の結果キャッシュが赤を持ち越し、原因の切り分けに時間を使った** (1005 の途中)。壊れた規則で一度 lint を回したあと、規則を戻しても
   同じ checkout で `empty rule set` が出続けた。コードを疑って読み直してから、空の `GOLANGCI_LINT_CACHE` で回して初めてキャッシュだと分かった (それが 1006 になった)
2. **同期の処理を goroutine へ移したら、テストの差し替えが data race になった** (1002)。テストは package 変数の `readBulk` を差し替えていて、
   読み込みを別の goroutine へ移すと、走っている walker と差し替えが競合した。`Model.loadRead` (instance の seam) に移して直した
3. **コメントの経緯の語 (issue の番号) で `test-comment-provenance` に 4 回落ちた** (load.go・tickchain・glogx・test_golangci_ruleguard_key.sh)。
   どれも commit の前の root の `make test` で見つかったが、そのたびに直して回し直した。retro 1004 の 1 も同じ検査で、同じ形が 2 セッション続いた
4. 変異が 2 回、そのままでは何も証明しなかった: `tickMsg{}` の変異は `seq` が未使用になってビルドが落ち (第 3 の結果)、`seq - seq` で当て直した。
   古い tick を捨てる assert は、ease-in の 1 フレーム目で位置が動かないので退行でも緑になり、フレーム数を見る形に直した
5. pinned のテストに足した fixture の段が、手元の `.golangci.local.yml` を書き換えたまま戻さず、後の段 (8) を壊した。退避して戻す形に直した
6. 配線の grep がコメントの行に当たって空振りしていた (変異で見つけ、呼び出しの行に当てる形に直した)
7. メモリ `serial-agents-only` があるのに、レビューのサブエージェントを走らせたまま `make test` を重ねた。気づいてから直列に戻した
8. 局所: `issue_done.sh` が 4 桁の番号を拒んだ (1002 は手で移した。他のセッションが直した)。zsh の alias `lint` と同名の関数を定義しかけて壊した

## 提案

| # | 気づき (一般形) | 切り出し先 | 状態 |
|---|---|---|---|
| A | **キャッシュは緑だけでなく赤も持ち越す**。入力 (設定・規則・生成物) を戻しても失敗が消えないときは、コードを読み直す前に空のキャッシュ dir で 1 回回す。あわせて、道具の設定が**パスで指すだけのファイル**が鍵に入っているかを確かめる (入っていなければ鍵に入れる経路を作る。1006 の形) | `_claude/rules/verify-execution-not-just-exit-code.md` の「キャッシュされた緑と path filter は、鍵に数えない入力の変化を見ない」に追記 (今は緑の側と Go の testcache だけを書いている) | 未着手 |
| B | **処理を別の goroutine・thread・task へ移すときは、テストが差し替える package 変数・グローバルの seam を先に列挙し、instance の seam に移してから移す**。移したあとの差し替えは、走っている処理と競合する (race detector はその経路を通るテストを `-race` で回したときしか見えない) | `_claude/rules/survey-receiver-guards-before-passing-new-values.md` の「既存の呼び出しの下に新しい待ちを足すとき」の隣に 1 項 (発動点は「処理の実行の文脈を変えるとき」で、同じ節の拡張で足りる) | 未着手 |
| C | **同じ機械の検査に 2 セッション続けて落ちるなら、検査を書く時点に寄せる** (編集した直後に、そのファイルだけを同じ検査に通して知らせる)。commit の前の一括で見つけると、直しと回し直しが落ちた回数だけ増える | 新規 issue: `src/` などの Write / Edit の後に、そのファイルだけを `scripts/check_comment_provenance.sh` に通して知らせる PostToolUse hook (既存の `suggest-dupscan-for-go-edit.sh` と同じ置き方) | 未着手 |
| — | 変異がビルドできない・assert が見る量が退行で動かない (4) | 却下: `mutation-verify-new-tests.md` の手順 1.5 と「その assert が見ている量は、壊れたときに動くのか」で足りる。守れて、気づけた | — |
| — | 後の段が使う fixture を書き換えたまま戻さない (5) | 却下: `mutation-verify-new-tests.md` の「A-B のケース間で状態を共有しない」で足りる | — |
| — | 配線の grep がコメントに当たる (6) | 却下: 変異で見つかった (仕組みが働いた) | — |
| — | サブエージェントと検査を重ねた (7) | 却下: メモリ `serial-agents-only` で足りる。守れなかっただけ | — |
| — | 局所の 2 件 (8) | 却下: `issue_done.sh` は直済み。alias は局所 | — |

## 進捗

- 起票のみ。A〜C の実行はユーザーの判断を待つ
