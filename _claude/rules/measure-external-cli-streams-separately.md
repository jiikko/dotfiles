# 外部 CLI の stdout / stderr / exit code は分離して実測する

> **トリガー型ルール。** 外部コマンドの出力や終了コードを「判定材料」にして、設計の前提を書く /
> 実装を始めようとした瞬間に発動する。過去ログや既存の実測表を根拠に着手する場合も同じ。

## ルール

- **stdout / stderr / exit code を最初から別々に採る** (`cmd >out 2>err; echo $?`)。
  `2>&1` や `| head` を通した結果を「実測事実」として設計に書かない。混ぜた観測では
  **どの stream が判定材料か**を確定できない
- **1 つのログに混ぜた 2 stream の「順序」も判定材料にしない**。stdout (buffered) と stderr (unbuffered) は
  flush のタイミングが違うので、同じログ上の前後関係は実際の発生順を表さない (実例 obaket 645 M6c,
  2026-09-03: Swift Testing の `started` 行 (stdout) と NSLog マーカー (stderr) の順序から hang の位置を
  推理して 2 回誤診。stderr だけに揃えた 5 run 目で確定)。時系列が要るなら**同一 stream に、
  テスト固有の識別子を付けて**出す (同名の行が別テストにもある前提で、関数名でスコープを切る)
- 記録には **CLI のバージョン・関係する環境変数・認証状態**を併記する。stream を分けただけでは
  条件の違う観測が並ぶだけで、再現可能な事実にならない
- **出力があっても契約を確定できない終了形** (timeout / signal / コマンド未発見 / 権限不足) を、
  正常な空出力と分けて扱う。途中まで出た文字列を正常な判定材料にしない
- **個別 CLI の現在仕様をこのルールに転記しない**。ルールが正本として持つのは**測定方法**だけで、
  「どの CLI が何をどこに出すか」は実装側のコメント (例: `src/glogx/cli_health.go`) か測定記録に
  一箇所だけ置く。両方に書くと CLI のバージョンが上がったときにルール側が黙って腐る
- **実 CLI の一時実行は「契約の独立確認」であって seam テストの代替ではない**。seam テストは
  「測定した契約が正しい」前提で判定ロジックと異常系を再現可能に守り、実 CLI の実行は
  **測定表そのものの読み違い**を検出する。役割が違うので、片方があるから他方が要らない、にならない
- 実 CLI を足すのは **副作用を隔離できる場合に限る**。認証情報・ネットワーク・Keychain・daemon・
  設定ファイルを隔離しきれない CLI では強行せず、**確認できていない範囲を明記する**
  (環境変数で HOME や設定ディレクトリを差し替えれば隔離できることが多いが、Keychain や
  daemon 経由の状態は差し替えられない)

## なぜ

起源: retro 100 項目 1・4, 2026-08-24。根拠・起源・実例は `~/dotfiles/_claude/rules-rationale/measure-external-cli-streams-separately.md` に置く（起動時には読まれない。ルールを疑う・改訂するときに読む）。

## やること / やらないこと

- ✓ `>out 2>err` + exit code を別々に採り、バージョン・環境変数・認証状態を同じ記録に残す
- ✓ timeout / signal / 未発見 / 権限不足を「判定不能」として正常系と分ける
- ✓ 副作用を隔離できるなら、実 CLI を 1 回だけ通して測定表を独立に確認する
- ✓ 隔離できないなら実行せず、確認できていない範囲を明記する
- ✗ `2>&1` / `| head` を通した観測を実測事実として設計に書く
- ✗ 個別 CLI の出力文言・終了コードをこのルールに現在仕様として転記する
- ✗ seam テストが green であることを、測定表が正しいことの証拠に数える
- ✗ 副作用を隔離できない CLI に実 CLI の一時実行を無条件で要求する

## 関連

- [`instrument-before-second-fix.md`](instrument-before-second-fix.md) — あちらは「1 回目の修正仮説が
  外れた**後**」に観測を増やす。本ルールは設計の前提を作る**初回観測**が発動点
- [`verify-execution-not-just-exit-code.md`](verify-execution-not-just-exit-code.md) — exit code だけで
  成否を判断しない一般論の正本
- [`mutation-verify-new-tests.md`](mutation-verify-new-tests.md) — 「fake が外部コマンドの exit code を
  模しているか」はあちらの「守っていないテストの形」が正本
- [`adversarial-review-own-safeguards.md`](adversarial-review-own-safeguards.md) — 異常系を実験で作る
  一般論の正本 (本ルールでは CLI の副作用隔離がその前提になる)
- 起源の記録: `issues/100-retro-glogx-cli-health-2026-08-24.md`
