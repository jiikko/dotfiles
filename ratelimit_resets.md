# Weekly Rate Limit リセット情報の確認方法

## Codex CLI

セッションログにリセット時刻が記録されている。

### ログの場所

`~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl`

### データ形式

```json
{
  "type": "token_count",
  "rate_limits": {
    "primary": {
      "used_percent": 3.0,
      "window_minutes": 300,
      "resets_at": 1770133434
    },
    "secondary": {
      "used_percent": 1.0,
      "window_minutes": 10080,
      "resets_at": 1770720234
    }
  }
}
```

- `primary`: 5時間ウィンドウ (300分)
- `secondary`: weekly ウィンドウ (10080分 = 7日)
- `resets_at`: Unix timestamp (JST変換: `date -r <timestamp>`)

### 最新のリセット時刻を取得するワンライナー

```bash
grep -h 'rate_limits' ~/.codex/sessions/$(date +%Y)/*/*/*.jsonl | tail -1 | python3 -c "
import json,sys; d=json.loads(sys.stdin.read()); r=d['payload']['rate_limits']['secondary']
print(f\"weekly: {r['used_percent']}% used, resets at: $(date -r {r['resets_at']})\" if False else ''); import subprocess, datetime
ts=d['payload']['rate_limits']['secondary']['resets_at']
print(f\"weekly used: {r['used_percent']}%\")
print(f\"resets_at:   {datetime.datetime.fromtimestamp(ts)}\")"
```

もっとシンプルに:

```bash
grep -rh 'rate_limits' ~/.codex/sessions/ | tail -1 | python3 -mjson.tool | grep -A2 secondary
```

## Claude Code

### 確認方法

- CLI内で `/usage` コマンドを実行
- `~/.claude` 内のログにもリセット情報がある可能性あるが、具体的なフィールド名・ファイルは未特定

### わかっていること

- `~/.claude/stats-cache.json`: 日次の利用統計（メッセージ数、トークン数）はあるが、リセット時刻は含まれない
- `~/.claude/debug/*.txt`: デバッグログ。rate limit 関連のHTTPヘッダ等は未確認
- `~/.claude/projects/*/*.jsonl`: セッションログ。会話内容が主で、rate limit メタデータは未発見

## 共通の注意点

- 両サービスとも固定曜日リセットではなく **ローリング7日間ウィンドウ**
- 使い始めた時点から7日後にリセットされるため、リセット時刻は利用パターンで変動する
- Codex は 00:00 UTC にリセットされるとの情報あり（OpenAI サポート回答）
