# 947 (retro): dupscan で 4 module の重複を寄せ、dupscan に取り込んだセッション (2026-10-28)

起票日: 2026-10-28

## 概要

941〜943 の実装と効果測定、lockman・mac-lan-manager・ratelimit・restartable の重複 helper の統合 (21 件)、dupscan への取り込み (小さい単位の未比較の表示・L1m)。関連: 941 / 942 / 943 / 945 / 946。

## どこで踏んだか / 回りくどかったか

- **隔離したつもりで隔離できていなかった** (945): `go test` の `t.TempDir()` に `GOTMPDIR` が使われることを知らず、`GOTMPDIR` を小さい dir にして「実 $TMPDIR では再現しない」と読みかけた。ハングの再現が取れたのは `GOTMPDIR` を外してから。
- **打ち切りの既定がある出力を全件と読んだ** (mac-lan-manager): `dupscan callers or` が既定の 20 件で打ち切られ、件数ヘッダが出ていたのに `grep '^src'` で自分で捨てた。`lanmgr` の 5 ファイルの呼び出しを見落としたまま置き換えかけ、コンパイラの `undefined: or` で気づいた。
- **変異ハーネスを毎回手で組み、ビルドできない変異を 8 回作った**: `bin/mutate-verify-list` を見ずに python で組んだ。ビルド不能は「赤」でも「緑」でもない第 3 の結果で、そのつど当て直した。
- **push の前に lint の rc を確かめなかった** (mac-lan-manager): 新しい package を足したのに `src/PACKAGES.md` を再生成せず、`make test-lint` の失敗を見ずに push した。直後の commit で直した。
- **未解明**: ratelimit の統合で `make test-lint` が 17 分近くかかった (`shellcheck` 2 本が 16 分 CPU 100%)。再実行すると全体で 9 秒で、再現しない。

## 提案

1. **隔離で「再現しない」と結論する前に、検査対象が隔離した値を実際に読んだ証拠を 1 回取る** (一般形。環境変数・設定 dir・一時 dir で環境を隔離した調査すべて)。切り出し先: 既存 `_claude/rules/verify-execution-not-just-exit-code.md` の「切り分けで動かした変数を数える」の項へ 1 文追記 (例: `go test` は `GOTMPDIR` をテストの一時 dir にも使う。隔離したパスが検査対象の中で現れたか、一時 dir の実パスを出して確かめる)。新規ルールは立てない (発動点が既存の項と同じ)。
2. **置き換え・削除の前の呼び出し元の列挙は、上限の無い取得 (`-limit 0`) か、コンパイラで網羅を確かめる。出力の「他 N 件」の行を絞り込みで捨てない**。切り出し先: skill dupscan の 2 節 (`callers`) へ 1 行 (SKILL.md の改修なので、整合チェック 3 周と `bin/skill-eval` を伴う。ユーザーの判断を待つ)。

## 却下

- **push の前に gate の rc を `&&` で確かめる**: 却下。既存の `verify-execution-not-just-exit-code.md` に既出で、守れなかった事例。後半は `&&` で連結して守った。
- **変異ハーネスで、ビルドできない変異を赤と数えない**: 却下。`mutation-verify-new-tests.md` の手順 1.5 と「手で組む前に `bin/mutate-verify-list` を見る」に既出。これも守れなかった事例。
- **乱数歩行のテストが fixture の外 (親 dir・$TMPDIR) へ出られないことを固定する**: 却下。局所で、945 の「残るリスク」に同型 (virt_test の歩行) を観測点として載せた。ほかのテストで同じ形が出たときに一般形へ上げる。
- **`make test-lint` が 17 分かかった件**: 未確認リスク。再現しない。再び 1 分を超えたとき、`ps` で `shellcheck` の引数のファイルを控えてから止める (今回は控える前に終わった)。
