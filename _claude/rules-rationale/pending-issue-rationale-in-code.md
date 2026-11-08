# Pending / 却下した issue の理由をコードに残す — 実例

ルール本文: `~/dotfiles/_claude/rules/pending-issue-rationale-in-code.md`（`~/.claude/rules/` に link され、毎セッション起動時に読まれる）。
この文書は起動時には読まれない。ルールの実例を保存し、ルールを疑う・改訂するときに読む。

## 本文から移した実例 (2026-11-08。常時読み込みの字数を減らすため、本文の例の節から移した)

### 例 (外部登録値と一致が必要なケース)

```js
// OAuth 認証後のリダイレクトURI（ローカルサーバーで受け取る）
//
// 🚨 ポートを変更しないこと:
// この URI は IdP 側 (App Console) に登録済みの Redirect URI と完全一致する必要があり、
// 動的割当 (port 0) や別の固定値に変えると認証フロー全体が破綻する。
// 変更が必要な場合は IdP の管理画面で Redirect URI も同じポートに更新すること。
redirectUri: getEnv('REDIRECT_URI', 'http://localhost:43823/auth/callback'),
```

何が制約か (外部登録値との一致) / 何が起こるか (OAuth エラー) / どう変えるか (外部側を同期更新) の 3 点が入っている。
