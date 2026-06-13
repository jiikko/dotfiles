# Electron Expert リファレンス — Multi-Window Coordination & Data Persistence

> electron-expert agent が該当領域を深掘り分析/レビューする際に Read する詳細リファレンス。
> Source of truth: dotfiles/_claude/references/electron-expert/multiwindow-persistence.md

### 10. Multi-Window Coordination (Expert Level)

> For basic IPC patterns, see Section 2. This section covers multi-window specific challenges.

**Main Process as Single Source of Truth**:
```typescript
// ✅ Expert: メインプロセス集中型の状態管理
// src/main/services/app-state.ts

import { BrowserWindow, ipcMain } from 'electron';
import { EventEmitter } from 'events';

interface AppState {
  theme: 'light' | 'dark';
  currentProject: string | null;
  recentFiles: string[];
}

class AppStateManager extends EventEmitter {
  private state: AppState = {
    theme: 'light',
    currentProject: null,
    recentFiles: [],
  };

  constructor() {
    super();
    this.setMaxListeners(50); // ウィンドウ数 + 内部リスナー分
    this.setupIPC();
  }

  private setupIPC() {
    ipcMain.handle('state:get', () => ({ ...this.state }));

    ipcMain.handle('state:update', (event, partial: Partial<AppState>) => {
      // ✅ Expert: prototype pollution 防止 + 許可キーのみ受け付け
      if (partial == null || typeof partial !== 'object' || Array.isArray(partial)) {
        throw new Error('Invalid state update');
      }
      const allowed = new Set<string>(['theme', 'currentProject', 'recentFiles']);
      const sanitized: Partial<AppState> = {};
      for (const key of Object.keys(partial)) {
        if (allowed.has(key) && key !== '__proto__' && key !== 'constructor') {
          (sanitized as any)[key] = (partial as any)[key];
        }
      }
      this.update(sanitized);
      return { ...this.state };
    });
  }

  update(partial: Partial<AppState>) {
    const prev = { ...this.state };
    this.state = { ...this.state, ...partial };

    // 変更されたキーのみ全ウィンドウにブロードキャスト
    for (const key of Object.keys(partial) as (keyof AppState)[]) {
      if (prev[key] !== this.state[key]) {
        this.broadcast(`state:${key}`, this.state[key]);
      }
    }
  }

  private broadcast(channel: string, data: unknown) {
    for (const win of BrowserWindow.getAllWindows()) {
      if (!win.isDestroyed()) {
        win.webContents.send(channel, data);
      }
    }
  }
}

export const appState = new AppStateManager();
```

```typescript
// ✅ Expert: renderer 側の共有状態フック
// src/renderer/hooks/useSharedState.ts

import { useState, useEffect, useCallback } from 'react';

export function useSharedState<K extends keyof AppState>(key: K) {
  const [value, setValue] = useState<AppState[K] | null>(null);

  useEffect(() => {
    let cancelled = false; // ✅ Expert: key 変更時の stale Promise 防止
    window.electronAPI.invoke('state:get')
      .then((state: AppState) => {
        if (!cancelled) setValue(state[key]);
      })
      .catch((err: unknown) => {
        if (!cancelled) console.error('Failed to get shared state:', err);
      });
    return () => { cancelled = true; };
  }, [key]);

  useEffect(() => {
    return window.electronAPI.on(`state:${key}`, (newValue: AppState[K]) => {
      setValue(newValue);
    });
  }, [key]);

  const update = useCallback(async (newValue: AppState[K]) => {
    await window.electronAPI.invoke('state:update', { [key]: newValue });
  }, [key]);

  return [value, update] as const;
}
```

**MessagePort for Direct Window Communication**:
```typescript
// ✅ Expert: メインプロセス経由せずウィンドウ間直接通信
// 高頻度データ転送に最適（リアルタイムコラボ、ストリーミング等）

// src/main/windows/channel-broker.ts
import { BrowserWindow, MessageChannelMain, ipcMain } from 'electron';

export function setupChannelBroker() {
  ipcMain.handle(
    'channel:create',
    (event, { targetWindowId, channelName }) => {
      const source = BrowserWindow.fromWebContents(event.sender);
      const target = BrowserWindow.fromId(targetWindowId);

      if (!source || !target || target.isDestroyed()) {
        throw new Error('Invalid window');
      }

      const { port1, port2 } = new MessageChannelMain();
      source.webContents.postMessage('channel:port', { channelName }, [port1]);
      target.webContents.postMessage('channel:port', { channelName }, [port2]);

      return { success: true };
    },
  );
}
```

**WindowManager with Lifecycle & Position Restore**:
```typescript
// ✅ Expert: ウィンドウプールによるライフサイクル管理

import { BrowserWindow, screen } from 'electron';
import path from 'path';

interface WindowConfig {
  id: string;
  url: string;
  width?: number;
  height?: number;
  parent?: BrowserWindow;
  modal?: boolean;
  restorePosition?: boolean;
}

class WindowManager {
  private windows = new Map<string, BrowserWindow>();
  private positions = new Map<string, Electron.Rectangle>();

  create(config: WindowConfig): BrowserWindow {
    // 既存ウィンドウがあればフォーカス
    const existing = this.windows.get(config.id);
    if (existing && !existing.isDestroyed()) {
      existing.focus();
      return existing;
    }

    const savedBounds = config.restorePosition
      ? this.positions.get(config.id)
      : undefined;

    // ✅ Expert: ディスプレイ検証（外部モニタ切断対策）
    const validBounds = savedBounds && this.isWithinDisplay(savedBounds)
      ? savedBounds
      : undefined;

    const win = new BrowserWindow({
      width: validBounds?.width ?? config.width ?? 800,
      height: validBounds?.height ?? config.height ?? 600,
      x: validBounds?.x,
      y: validBounds?.y,
      parent: config.parent,
      modal: config.modal ?? false,
      show: false, // ready-to-show まで非表示（白フラッシュ防止）
      webPreferences: {
        preload: path.join(__dirname, '../../preload/index.js'),
        contextIsolation: true,
        sandbox: true,
      },
    });

    win.once('ready-to-show', () => win.show());

    // 位置保存（debounce 付き）
    let saveTimer: NodeJS.Timeout;
    const saveBounds = () => {
      clearTimeout(saveTimer);
      saveTimer = setTimeout(() => {
        if (!win.isDestroyed() && !win.isMinimized()) {
          this.positions.set(config.id, win.getBounds());
        }
      }, 500);
    };
    win.on('resize', saveBounds);
    win.on('move', saveBounds);

    win.on('closed', () => {
      clearTimeout(saveTimer);
      this.windows.delete(config.id);
    });

    // ✅ Expert: loadURL バリデーション（リモート URL ブロック）
    const parsed = new URL(config.url, 'file://');
    if (!['file:', 'http:', 'https:'].includes(parsed.protocol)) {
      throw new Error(`Blocked protocol: ${parsed.protocol}`);
    }
    if (parsed.protocol !== 'file:' && !parsed.hostname.match(/^(localhost|127\.0\.0\.1)$/)) {
      throw new Error('Remote URLs not allowed; use file:// or localhost only');
    }

    win.loadURL(config.url);
    this.windows.set(config.id, win);
    return win;
  }

  get(id: string): BrowserWindow | undefined {
    const win = this.windows.get(id);
    return win && !win.isDestroyed() ? win : undefined;
  }

  private isWithinDisplay(bounds: Electron.Rectangle): boolean {
    return screen.getAllDisplays().some(({ workArea }) =>
      bounds.x >= workArea.x - 100 &&
      bounds.y >= workArea.y - 100 &&
      bounds.x + bounds.width <= workArea.x + workArea.width + 100 &&
      bounds.y + bounds.height <= workArea.y + workArea.height + 100
    );
  }
}

export const windowManager = new WindowManager();
```

```typescript
// ❌ NEVER: ウィンドウ参照をグローバル変数で管理し、isDestroyed() チェックなし
let settingsWin: BrowserWindow;
function openSettings() {
  settingsWin = new BrowserWindow({ ... });
  // settingsWin.isDestroyed() のチェックなしで後から参照 → クラッシュ
}

// ❌ NEVER: ウィンドウ位置を復元する際にディスプレイ存在確認をしない
// 外部モニタ切断時にウィンドウが画面外に表示される
```

**BroadcastChannel for Simple Sync**:
```typescript
// ✅ Expert: BroadcastChannel による簡易ブロードキャスト
// メインプロセス不要、renderer 間で直接通信

const channel = new BroadcastChannel('app-sync');

// 送信
channel.postMessage({ type: 'state-update', data: newState });

// 受信
channel.onmessage = (event) => {
  if (event.data.type === 'state-update') {
    updateLocalState(event.data.data);
  }
};

// ⚠️ Expert warning: event.origin は file:// になるため検証不可
// セキュリティが重要な通信には Main プロセス経由 IPC を使用
```

### 11. Data Persistence Strategy (Expert Level)

**Storage Decision Matrix**:
```
┌─────────────────────┬──────────────────┬─────────────────────┬──────────────┐
│ Criteria            │ electron-store   │ better-sqlite3      │ IndexedDB    │
├─────────────────────┼──────────────────┼─────────────────────┼──────────────┤
│ データ構造           │ Key-Value (JSON) │ リレーショナル       │ Key-Value    │
│ データ量             │ < 10MB           │ 無制限 (GB級対応)    │ 中規模       │
│ クエリ性能           │ 全件読み込み      │ ✅ インデックス検索   │ カーソル走査  │
│ プロセス             │ Main             │ Main (IPC経由)       │ Renderer     │
│ ネイティブモジュール  │ 不要             │ 必要 (electron-rebuild) │ 不要      │
│ 推奨用途             │ 設定、少量状態    │ 業務データ、検索      │ キャッシュ   │
│ バックアップ         │ ファイルコピー     │ ✅ .backup() API    │ 手動エクスポート│
│ 暗号化              │ safeStorage 連携  │ SQLCipher           │ なし         │
└─────────────────────┴──────────────────┴─────────────────────┴──────────────┘
```

**electron-store -- Expert Usage with Type Safety**:
```typescript
// ✅ Expert: 型安全 + JSON Schema バリデーション + マイグレーション
// src/main/services/config-store.ts
// ⚠️ Note: electron-store v9+ は ESM-only。CJS プロジェクトでは v8 を使用するか、
// package.json に "type": "module" を設定すること。

import Store from 'electron-store';

interface AppConfig {
  theme: 'light' | 'dark' | 'system';
  language: string;
  editor: {
    fontSize: number;
    tabSize: number;
    wordWrap: boolean;
  };
  windowBounds: { x?: number; y?: number; width: number; height: number };
}

const configStore = new Store<AppConfig>({
  name: 'config',
  schema: {
    theme: { type: 'string', enum: ['light', 'dark', 'system'], default: 'system' },
    language: { type: 'string', default: 'en' },
    editor: {
      type: 'object',
      properties: {
        fontSize: { type: 'number', minimum: 8, maximum: 72, default: 14 },
        tabSize: { type: 'number', enum: [2, 4, 8], default: 2 },
        wordWrap: { type: 'boolean', default: true },
      },
      default: {},
    },
    windowBounds: {
      type: 'object',
      properties: {
        width: { type: 'number', default: 1200 },
        height: { type: 'number', default: 800 },
      },
      default: { width: 1200, height: 800 },
    },
  },
  // ✅ Expert: バージョン間マイグレーション
  migrations: {
    '1.1.0': (store) => {
      const oldFontSize = (store as any).get('fontSize');
      if (oldFontSize !== undefined) {
        store.set('editor.fontSize', oldFontSize);
        (store as any).delete('fontSize');
      }
    },
    '2.0.0': (store) => {
      if (store.get('theme') === ('auto' as any)) {
        store.set('theme', 'system');
      }
    },
  },
});

export { configStore };
```

**better-sqlite3 -- Expert Usage with WAL & FTS5**:
```typescript
// ✅ Expert: WAL モード + マイグレーション + 全文検索
// src/main/services/database.ts
//
// ⚠️ Expert warning: better-sqlite3 は同期 API のため、大量データ操作時は
// メインプロセスの event loop をブロックする。重い処理は Worker Thread に委譲：
//   import { Worker } from 'worker_threads';
//   const dbWorker = new Worker('./db-worker.js');

import Database from 'better-sqlite3';
import { app } from 'electron';
import path from 'path';

class DatabaseManager {
  private db: Database.Database;
  private stmtCache = new Map<string, Database.Statement>();

  constructor() {
    const dbPath = path.join(app.getPath('userData'), 'app-data.db');
    this.db = new Database(dbPath);

    // ✅ Expert: パフォーマンス最適化
    this.db.pragma('journal_mode = WAL');    // 読み書き並行
    this.db.pragma('synchronous = NORMAL');   // WAL 推奨値
    this.db.pragma('foreign_keys = ON');
    this.db.pragma('cache_size = -64000');    // 64MB キャッシュ

    this.runMigrations();
  }

  private runMigrations() {
    this.db.exec(`
      CREATE TABLE IF NOT EXISTS _migrations (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        version TEXT NOT NULL UNIQUE,
        applied_at TEXT DEFAULT (datetime('now'))
      );
    `);

    const migrations = [
      {
        version: '001',
        up: `
          CREATE TABLE documents (
            id TEXT PRIMARY KEY,
            title TEXT NOT NULL,
            content TEXT NOT NULL DEFAULT '',
            created_at TEXT DEFAULT (datetime('now')),
            updated_at TEXT DEFAULT (datetime('now'))
          );
          CREATE INDEX idx_documents_updated ON documents(updated_at);
        `,
      },
      {
        version: '002',
        up: `
          CREATE VIRTUAL TABLE documents_fts USING fts5(
            title, content, content=documents, content_rowid=rowid
          );
          CREATE TRIGGER documents_ai AFTER INSERT ON documents BEGIN
            INSERT INTO documents_fts(rowid, title, content)
            VALUES (new.rowid, new.title, new.content);
          END;
          CREATE TRIGGER documents_au AFTER UPDATE ON documents BEGIN
            INSERT INTO documents_fts(documents_fts, rowid, title, content)
            VALUES ('delete', old.rowid, old.title, old.content);
            INSERT INTO documents_fts(rowid, title, content)
            VALUES (new.rowid, new.title, new.content);
          END;
          CREATE TRIGGER documents_ad AFTER DELETE ON documents BEGIN
            INSERT INTO documents_fts(documents_fts, rowid, title, content)
            VALUES ('delete', old.rowid, old.title, old.content);
          END;
        `,
      },
    ];

    const applied = new Set(
      this.db.prepare('SELECT version FROM _migrations').all()
        .map((r: any) => r.version),
    );

    this.db.transaction(() => {
      for (const m of migrations) {
        if (!applied.has(m.version)) {
          this.db.exec(m.up);
          this.db.prepare('INSERT INTO _migrations (version) VALUES (?)')
            .run(m.version);
        }
      }
    })();
  }

  // ✅ Expert: Prepared Statement キャッシュ（上限付き）
  private static readonly MAX_CACHE_SIZE = 100;

  private prepare(sql: string): Database.Statement {
    let stmt = this.stmtCache.get(sql);
    if (!stmt) {
      stmt = this.db.prepare(sql);
      if (this.stmtCache.size >= DatabaseManager.MAX_CACHE_SIZE) {
        const oldest = this.stmtCache.keys().next().value!;
        this.stmtCache.delete(oldest);
      }
      this.stmtCache.set(sql, stmt);
    }
    return stmt;
  }

  searchDocuments(query: string, limit = 20) {
    // ✅ Expert: FTS5 クエリインジェクション防止
    // ユーザー入力をダブルクォートで囲みメタ文字を無効化
    const sanitized = `"${query.replace(/"/g, '""')}"`;
    return this.prepare(`
      SELECT d.id, d.title,
        snippet(documents_fts, 1, '<mark>', '</mark>', '...', 32) AS snippet
      FROM documents_fts
      JOIN documents d ON d.rowid = documents_fts.rowid
      WHERE documents_fts MATCH ?
      ORDER BY rank LIMIT ?
    `).all(sanitized, limit);
  }

  async backup(destPath: string): Promise<void> {
    return this.db.backup(destPath);
  }

  close() {
    this.stmtCache.clear();
    this.db.close();
  }
}

export const database = new DatabaseManager();
// ✅ Expert: will-quit を使用（before-quit はキャンセル可能なため DB close が呼ばれない場合がある）
app.on('will-quit', () => database.close());
```

**Secure Storage with safeStorage API**:
```typescript
// ✅ Expert: OS ネイティブ暗号化でシークレットを管理
// macOS: Keychain, Windows: DPAPI, Linux: libsecret
// ⚠️ Linux warning: libsecret 未インストール時は basic_text にフォールバックし
// 実質平文保存となる。isEncryptionAvailable() で必ず事前確認すること。

import { safeStorage, ipcMain } from 'electron';
import Store from 'electron-store';

const secureStore = new Store<Record<string, string>>({ name: 'secure-data' });

export const secureStorage = {
  setSecret(key: string, value: string): void {
    if (!safeStorage.isEncryptionAvailable()) {
      throw new Error('Encryption not available on this system');
    }
    const encrypted = safeStorage.encryptString(value);
    secureStore.set(key, encrypted.toString('base64'));
  },

  getSecret(key: string): string | null {
    if (!safeStorage.isEncryptionAvailable()) {
      throw new Error('Encryption not available on this system');
    }
    const encrypted = secureStore.get(key);
    if (!encrypted) return null;
    return safeStorage.decryptString(Buffer.from(encrypted, 'base64'));
  },

  deleteSecret(key: string): void {
    secureStore.delete(key as any);
  },
};

// IPC handler -- ホワイトリスト検証付き
const ALLOWED_SECRET_KEYS = ['api-token', 'refresh-token'];

ipcMain.handle('secure:get', (event, key: string) => {
  // ✅ Expert: sender 検証（WebContents が既知のウィンドウか確認）
  const win = BrowserWindow.fromWebContents(event.sender);
  if (!win) throw new Error('Unknown sender');
  if (!ALLOWED_SECRET_KEYS.includes(key)) throw new Error('Access denied');
  return secureStorage.getSecret(key);
});

ipcMain.handle('secure:set', (event, key: string, value: string) => {
  const win = BrowserWindow.fromWebContents(event.sender);
  if (!win) throw new Error('Unknown sender');
  if (!ALLOWED_SECRET_KEYS.includes(key)) throw new Error('Access denied');
  if (typeof value !== 'string' || value.length > 10000) throw new Error('Invalid value');
  secureStorage.setSecret(key, value);
});
```

```typescript
// ❌ NEVER: API トークンを electron-store に平文で保存
const store = new Store();
store.set('apiToken', 'sk-live-abc123');     // JSON ファイルで誰でも読める

// ❌ NEVER: renderer から直接 better-sqlite3 を操作
// (nodeIntegration が必要になりセキュリティ崩壊)
import Database from 'better-sqlite3';  // renderer では絶対にやらない

// ❌ NEVER: DB ファイルをアプリバンドル内に配置
// asar は読み取り専用。app.getPath('userData') を使う
const dbPath = path.join(__dirname, 'data.db');  // 書き込み不可
```

