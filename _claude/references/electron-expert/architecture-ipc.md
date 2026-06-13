# Electron Expert リファレンス — Process Architecture & IPC

> electron-expert agent が該当領域を深掘り分析/レビューする際に Read する詳細リファレンス。
> Source of truth: dotfiles/_claude/references/electron-expert/architecture-ipc.md

### 1. Process Architecture (Expert Level)

**Main vs Renderer Process - Deep Understanding**:
```javascript
// Main Process (Node.js environment)
// - ONE main process per app
// - Full Node.js access
// - Manages BrowserWindows
// - Handles app lifecycle
// - Native OS integrations (tray, menus, dialogs)

// main.js
import { app, BrowserWindow, ipcMain } from 'electron';
import path from 'path';

let mainWindow;

app.whenReady().then(() => {
  mainWindow = new BrowserWindow({
    width: 1200,
    height: 800,
    webPreferences: {
      // ✅ Expert: Security best practices
      nodeIntegration: false,      // NEVER enable in production
      contextIsolation: true,      // ALWAYS enable
      sandbox: true,               // Enable sandboxing
      preload: path.join(__dirname, 'preload.js'),
    },
  });

  mainWindow.loadFile('index.html');
});

// Renderer Process (Chromium environment)
// - ONE renderer per BrowserWindow/webview
// - Web APIs only (by default)
// - Sandboxed for security
// - Communicates via IPC through preload
```

**Preload Script - Expert Patterns**:
```javascript
// preload.js - Bridge between main and renderer
// Runs in isolated context but has access to Node.js APIs

import { contextBridge, ipcRenderer } from 'electron';

// ✅ Expert: Expose minimal, specific APIs
contextBridge.exposeInMainWorld('electronAPI', {
  // One-way: renderer -> main
  sendMessage: (channel, data) => {
    const validChannels = ['save-file', 'open-dialog'];
    if (validChannels.includes(channel)) {
      ipcRenderer.send(channel, data);
    }
  },

  // Two-way: renderer -> main -> renderer
  invoke: (channel, data) => {
    const validChannels = ['get-system-info', 'read-file'];
    if (validChannels.includes(channel)) {
      return ipcRenderer.invoke(channel, data);
    }
    return Promise.reject(new Error(`Invalid channel: ${channel}`));
  },

  // Main -> renderer (with cleanup)
  onUpdate: (callback) => {
    const subscription = (event, data) => callback(data);
    ipcRenderer.on('update-available', subscription);
    // Return cleanup function
    return () => ipcRenderer.removeListener('update-available', subscription);
  },
});

// ❌ NEVER do this - exposes full Node.js
// contextBridge.exposeInMainWorld('require', require);
// contextBridge.exposeInMainWorld('fs', require('fs'));
```

### 2. IPC Communication (Expert Level)

**IPC Patterns - Deep Understanding**:
```javascript
// main.js - IPC handlers

import { ipcMain, dialog, BrowserWindow } from 'electron';
import fs from 'fs/promises';

// ✅ Expert: Handle with invoke for request/response
ipcMain.handle('read-file', async (event, filePath) => {
  // Validate sender
  const webContents = event.sender;
  const win = BrowserWindow.fromWebContents(webContents);
  if (!win) throw new Error('Invalid sender');

  // Validate path (prevent directory traversal)
  const safePath = path.resolve(filePath);
  if (!safePath.startsWith(app.getPath('userData'))) {
    throw new Error('Access denied');
  }

  return fs.readFile(safePath, 'utf-8');
});

// ✅ Expert: Bidirectional communication
ipcMain.handle('open-file-dialog', async (event) => {
  const result = await dialog.showOpenDialog({
    properties: ['openFile'],
    filters: [{ name: 'Text', extensions: ['txt', 'md'] }],
  });

  if (result.canceled) return null;

  const filePath = result.filePaths[0];
  const content = await fs.readFile(filePath, 'utf-8');

  return { path: filePath, content };
});

// ✅ Expert: Send from main to renderer
function notifyRenderer(win, channel, data) {
  if (win && !win.isDestroyed()) {
    win.webContents.send(channel, data);
  }
}

// renderer.js - Using the exposed API
async function loadFile() {
  try {
    const result = await window.electronAPI.invoke('open-file-dialog');
    if (result) {
      console.log('File loaded:', result.path);
      editor.setValue(result.content);
    }
  } catch (error) {
    console.error('Failed to load file:', error);
  }
}

// Listen for main process updates
const unsubscribe = window.electronAPI.onUpdate((data) => {
  console.log('Update available:', data.version);
});

// Cleanup on unmount (React example)
useEffect(() => {
  return () => unsubscribe();
}, []);
```

**MessagePort for Performance**:
```javascript
// ✅ Expert: Use MessagePort for high-frequency communication
// Bypasses main process for renderer-to-renderer

// main.js
ipcMain.on('request-channel', (event) => {
  const { port1, port2 } = new MessageChannelMain();

  // Send port to requesting renderer
  event.sender.postMessage('provide-channel', null, [port1]);

  // Send other port to worker renderer
  workerWindow.webContents.postMessage('provide-channel', null, [port2]);
});

// renderer.js (worker)
window.electronAPI.onChannel((port) => {
  port.onmessage = (event) => {
    const result = processData(event.data);
    port.postMessage(result);
  };
  port.start();
});
```

