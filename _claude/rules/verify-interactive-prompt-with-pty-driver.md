# 対話プロンプトの確認は `cmd | script` で流し込まない — pty driver で「待ってから書く」

> **トリガー型ルール。** `read` / `[y/N]` / パスワード入力のような**対話プロンプトを持つ
> コマンドを、自動で動かして確認しようとした瞬間**に発動する。

## ルール

- **回答をパイプで流し込む形 (`printf 'y\n' | script -q /dev/null cmd`) を使わない。**
  パイプが閉じた時点で `script` が pty へ EOF を送り、`read` が EOF を受けて
  **空回答扱い**になる。「y を入れたのに中止された」という結果になる
- 代わりに **pty driver** を使う: pty を開き、**プロンプト文字列が出るのを待ってから**回答を書く
  (`python3` の `pty.fork()` / `pexpect` / `expect`)
- **テストに残せる部分はパイプ (ヒアストリング) で足りる。** pty が要るのは
  **端末でしか成立しない挙動**を見るときだけ (下表)
- **対話の確認が「失敗」したら、実装を疑う前にハーネスを疑う。**
  ハーネス由来の失敗は「実装のバグ」と同じ顔をして出てくる

## なぜ

起源: dotfiles av1ify のクリップボード入力, 2026-08-22。根拠・起源・実例は `~/dotfiles/_claude/rules-rationale/verify-interactive-prompt-with-pty-driver.md` に置く（起動時には読まれない。ルールを疑う・改訂するときに読む）。

## パイプで足りるか / pty が要るか

| 見たいもの | 手段 |
|---|---|
| 回答に応じた分岐 (`y` で実行 / `n`・空で中止) | **パイプ / ヒアストリング**で足りる |
| 一覧・警告文の内容、終了コード | **パイプ**で足りる |
| **先行入力 (typeahead) の破棄** (`read -t 0`) | **pty が要る**。`read -t 0` は端末にだけ「入力が溜まっているか」を答え、パイプでは常に偽 → テストを書いても vacuous |
| **対話シェル限定の発火** (`setopt interactive`) | **pty が要る** (実行時に変更できない) |
| プロンプトの表示タイミング・再描画 | **pty が要る** |

**pty が要る挙動を pty 無しでテストに書かない**。書いても「実装が無い版と区別できない」
テストになる。その場合は**配線を静的に pin する**
(`functions <name>` の中身に呼び出しが在ることを assert する) 方が正直で、
挙動確認は pty の手動検証と human issue に回す。

## やること / やらないこと

- ✓ pty driver は「プロンプトを待ってから書く」形にする
- ✓ パイプで足りる分岐はパイプで書く (pty を全部に使わない)
- ✓ pty でしか成立しない挙動は、テストでは配線の静的 pin に留め、挙動は human issue へ回す
- ✓ 対話の確認が失敗したら、まずハーネス (EOF・タイミング・pty の有無) を疑う
- ✗ `printf 'y\n' | script -q /dev/null cmd` で対話を確認する
- ✗ pty が要る挙動をパイプでテストし、green を根拠にする (vacuous)
- ✗ ハーネス由来の失敗を実装のバグとして修正しにいく

## 関連

- [`adversarial-review-own-safeguards.md`](adversarial-review-own-safeguards.md) 節 2 —
  「テストハーネス自身の失敗を緑に畳まない」。本ルールは**逆向き**の事故
  (ハーネスの失敗が**赤**として出て、実装のバグに見える) を扱う
- [`no-osascript-for-ui-verification.md`](no-osascript-for-ui-verification.md) —
  「精度の低い検証手段で探索ループに入らない」同思想 (あちらは UI、こちらは対話プロンプト)
- 起源の記録: `issues/095-retro-av1ify-clipboard-input-2026-08-22.md` の項目 1 /
  実装側の注記: `tests/zshrc/av1ify/test_av1ify_clipboard.sh` の Test 21
