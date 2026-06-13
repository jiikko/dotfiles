# Electron Expert リファレンス — Renderer Framework Integration & Modern Toolchain

> electron-expert agent が該当領域を深掘り分析/レビューする際に Read する詳細リファレンス。
> Source of truth: dotfiles/_claude/references/electron-expert/frameworks-toolchain.md

### 8. Renderer Framework Integration (Expert Level)

**React + Electron -- Expert IPC Hooks**:
```typescript
// ✅ Expert: IPC invoke をラップし、アンマウント後のステート更新を防止
// src/renderer/hooks/useElectronIPC.ts

import { useCallback, useEffect, useRef, useState } from 'react';

export function useIPCInvoke<T>(channel: string) {
  const mountedRef = useRef(false);

  useEffect(() => {
    mountedRef.current = true;  // ✅ StrictMode: remount 時にも true にリセット
    return () => { mountedRef.current = false; };
  }, []);

  return useCallback(async (...args: unknown[]): Promise<T | null> => {
    try {
      const result = await window.electronAPI.invoke(channel, ...args);
      if (!mountedRef.current) return null;
      return result as T;
    } catch (error) {
      if (!mountedRef.current) return null;
      throw error;
    }
  }, [channel]);
}

export function useIPCListener<T>(channel: string, handler: (data: T) => void) {
  const handlerRef = useRef(handler);
  handlerRef.current = handler;

  useEffect(() => {
    const unsubscribe = window.electronAPI.on(channel, (data: T) => {
      handlerRef.current(data);
    });
    return unsubscribe; // preload 側の cleanup 関数を使用
  }, [channel]);
}

// Usage
function FileEditor() {
  const [content, setContent] = useState('');
  const readFile = useIPCInvoke<string>('read-file');

  useIPCListener<{ version: string }>('update-available', (data) => {
    console.log('New version:', data.version);
  });

  const handleOpen = async () => {
    const result = await readFile();
    if (result !== null) setContent(result);
  };

  return <Editor value={content} onOpen={handleOpen} />;
}
```

```typescript
// ❌ NEVER: renderer で Node.js モジュールを直接インポート
import fs from 'fs';                     // nodeIntegration 前提 → セキュリティホール
import { exec } from 'child_process';   // 同上

// ❌ NEVER: useEffect のクリーンアップなしで IPC リスナーを登録
useEffect(() => {
  window.electronAPI.on('data-update', (data) => {
    setState(data); // アンマウント後もステート更新 → メモリリーク
  });
  // return cleanup がない！
}, []);

// ❌ NEVER: preload に React/Vue を混入
// preload.js は UI フレームワークから完全に独立させる
```

**Vite / Webpack Integration**:
```typescript
// ✅ Expert: Vite の renderer 設定
// vite.renderer.config.ts

import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  base: './',  // Electron では相対パス必須（file:// プロトコル）
  build: {
    outDir: 'dist/renderer',
    sourcemap: process.env.NODE_ENV === 'development',
  },
  server: {
    port: 5173,
    strictPort: true,
  },
  // ✅ Expert: Node.js ビルトインを renderer バンドルから除外
  resolve: { conditions: ['browser'] },
});

// ❌ NEVER: renderer の Vite で Node.js ビルトインを polyfill
// resolve: { alias: { fs: 'browserify-fs' } }  // セキュリティモデル破壊

// ✅ Expert: Webpack の場合は target: 'web' を厳守
// module.exports = { target: 'web' };  // 'electron-renderer' は使わない
```

**Type-Safe IPC Bridge**:
```typescript
// ✅ Expert: preload の API 型を renderer と共有
// src/shared/electron-api.d.ts

// ⚠️ Expert: 実際の preload 実装では必ず channel ホワイトリスト検証を行うこと
// （Section 2 の validChannels パターン参照）。この型定義は renderer 側の DX 用。
interface ElectronAPI {
  invoke: <T = unknown>(channel: string, ...args: unknown[]) => Promise<T>;
  on: <T = unknown>(channel: string, callback: (data: T) => void) => () => void;
  sendMessage: (channel: string, data: unknown) => void;
}

declare global {
  interface Window {
    electronAPI: ElectronAPI;
  }
}

export {};
```

### 9. Modern Toolchain (Expert Level)

**electron-vite Configuration**:
```typescript
// ✅ Expert: 3プロセス統合ビルド設定
// electron.vite.config.ts

import { defineConfig, externalizeDepsPlugin } from 'electron-vite';
import react from '@vitejs/plugin-react';
import path from 'path';

export default defineConfig({
  main: {
    plugins: [externalizeDepsPlugin()],
    build: {
      outDir: 'out/main',
      rollupOptions: {
        input: { index: path.resolve(__dirname, 'src/main/index.ts') },
      },
    },
  },
  preload: {
    plugins: [externalizeDepsPlugin()],
    build: {
      outDir: 'out/preload',
      rollupOptions: {
        input: { index: path.resolve(__dirname, 'src/preload/index.ts') },
      },
    },
  },
  renderer: {
    plugins: [react()],
    root: path.resolve(__dirname, 'src/renderer'),
    build: {
      outDir: path.resolve(__dirname, 'out/renderer'),
      rollupOptions: {
        input: { index: path.resolve(__dirname, 'src/renderer/index.html') },
      },
    },
    server: {
      // ✅ Expert: 開発時 CSP -- HMR 用に 'unsafe-inline' を許可（本番では厳格化）
      headers: {
        'Content-Security-Policy': process.env.NODE_ENV === 'production'
          ? "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'"
          : "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; connect-src 'self' ws://localhost:*",
      },
    },
  },
});
```

**electron-forge vs electron-builder Decision Matrix**:
```
┌─────────────────────────┬──────────────────┬──────────────────┐
│ Criteria                │ electron-forge   │ electron-builder │
├─────────────────────────┼──────────────────┼──────────────────┤
│ 公式サポート             │ ✅ Electron 公式  │ コミュニティ       │
│ Vite 統合               │ ✅ ビルトイン      │ 手動設定           │
│ カスタムインストーラー    │ 限定的            │ ✅ NSIS/DMG 高度   │
│ ネイティブモジュール再構築│ ✅ 自動           │ 手動 rebuild 必要  │
│ CI クロスプラットフォーム │ ❌ 各OS上で実行   │ ✅ 1台でマルチOS   │
│ 推奨用途                │ 新規・中規模      │ 大規模・複雑ビルド │
└─────────────────────────┴──────────────────┴──────────────────┘
```

**Expert Project Structure**:
```
project-root/
├── electron.vite.config.ts     # 統一ビルド設定
├── src/
│   ├── main/                    # メインプロセス
│   │   ├── index.ts             # エントリーポイント
│   │   ├── ipc/                 # IPC ハンドラー (機能別分割)
│   │   │   ├── file-handlers.ts
│   │   │   ├── dialog-handlers.ts
│   │   │   └── index.ts
│   │   ├── services/            # ビジネスロジック (IPC 非依存)
│   │   └── windows/             # ウィンドウ管理
│   ├── preload/                 # Preload スクリプト
│   │   ├── index.ts
│   │   └── types.ts             # API 型定義
│   ├── renderer/                # Renderer (React/Vue/Svelte)
│   │   ├── index.html
│   │   └── src/
│   │       ├── App.tsx
│   │       ├── hooks/           # IPC フック
│   │       ├── components/
│   │       └── store/
│   └── shared/                  # プロセス間共有
│       ├── ipc-channels.ts      # チャネル名一元管理
│       └── types.ts
├── resources/                   # アイコン、ネイティブアセット
└── build/                       # コード署名、entitlements
```

**Type-Safe IPC Channel Registry**:
```typescript
// ✅ Expert: IPC チャネル名を型安全に一元管理
// src/shared/ipc-channels.ts

export const IPC_CHANNELS = {
  FILE: {
    READ: 'file:read',
    WRITE: 'file:write',
    OPEN_DIALOG: 'file:open-dialog',
  },
  APP: {
    GET_VERSION: 'app:get-version',
    CHECK_UPDATE: 'app:check-update',
  },
  WINDOW: {
    MINIMIZE: 'window:minimize',
    MAXIMIZE: 'window:maximize',
    CLOSE: 'window:close',
  },
} as const;

type DeepValues<T> = T extends string ? T : { [K in keyof T]: DeepValues<T[K]> }[keyof T];
export type IPCChannel = DeepValues<typeof IPC_CHANNELS>;

// ❌ NEVER: チャネル名を文字列リテラルで散在させる
ipcMain.handle('readFile', ...);           // typo しても気づかない
window.electronAPI.invoke('read-file', ...); // 名前不一致！
```

**ESM / CJS Handling**:
```typescript
// ✅ Expert: メインプロセスのモジュール戦略
// ソースは ESM で書き、ビルド時に必要に応じて CJS に変換

// package.json
{
  "type": "module",  // Electron >= 28 で安定
  "main": "out/main/index.js"
}

// ⚠️ Expert warning: ESM 環境では __dirname が未定義
// electron-vite は CJS 互換構文を自動提供するが、
// 手動設定の場合は以下が必要:
import { fileURLToPath } from 'url';
import path from 'path';
const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
```

