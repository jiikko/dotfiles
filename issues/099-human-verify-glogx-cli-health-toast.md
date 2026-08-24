# glogx の CLI health 警告トーストを実際の popup で目視確認する

起票日: 2026-08-24
期限: 2026-08-31

## 何をしてほしいか

`C-g` で glogx を開き、**警告トーストの見え方**を目視で確認してほしい。判定ロジック自体は
実 CLI を起動して 5 条件すべて検証済み (下記) なので、確認したいのは**見た目と読みやすさ**だけ。

### 手順

1 枚のとき / 2 枚重なったとき の 2 通りを見る。実際にログアウトする必要はない (認証を壊さずに
再現できる):

```sh
# codex だけログアウト状態に見せる (空の CODEX_HOME を渡す)
CODEX_HOME=$(mktemp -d) glogx

# 両方ログアウト状態に見せる = 警告 2 枚
CODEX_HOME=$(mktemp -d) CLAUDE_CONFIG_DIR=$(mktemp -d) glogx

# 未インストールに見せる (PATH から外す)
PATH=/usr/bin:/bin glogx
```

### 見てほしい点

- [ ] 文言が 1 行に収まって読めるか (箱は内容 1 行 + 罫線 + 影の 4 行構成)
  - `claude がログアウト状態です (claude auth login で再ログイン)`
  - `codex がログアウト状態です (codex login で再ログイン)`
  - `claude が見つかりません (Claude Code をインストールしてください)`
  - `codex が見つかりません (codex をインストールしてください)`
- [ ] 2 枚重なったときの並び (新しいものが上 = codex が上、claude が下) が読みやすいか
- [ ] 起動直後の他の通知 (usage 取得中・新バージョン通知・autobuild) と重なって邪魔になっていないか
- [ ] `w` で警告文がクリップボードへコピーできるか (最新 1 件だけ)
- [ ] 3 秒で引っ込むのが早すぎないか (`toastHold`)

## 判定ロジック側で既に検証済みのこと (目視不要)

実 CLI を起動した一時テストで確認済み (2026-08-24。テストには残していない):

| 条件 | 結果 |
|---|---|
| claude / codex 両方ログイン済み | 無通知 |
| `CODEX_HOME` を空にする | codex ログアウトを検出 |
| `CLAUDE_CONFIG_DIR` を空にする | claude ログアウトを検出 |
| `PATH` を空にする | 両方の未インストールを検出 |

## 関連

- commit 603d6b8 (検出とトースト) / 5ccb252 (狭い窓で 2 枚描けるようにした予算の下限)
- 実装: `src/glogx/cli_health.go` / 予算: `src/glogx/tui.go: toastDrawBudget`
