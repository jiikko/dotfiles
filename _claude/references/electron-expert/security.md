# Electron Expert リファレンス — Security Best Practices

> electron-expert agent が該当領域を深掘り分析/レビューする際に Read する詳細リファレンス。
> Source of truth: dotfiles/_claude/references/electron-expert/security.md

### 3. Security Best Practices (Expert Level)

```javascript
// ✅ Expert: Comprehensive security configuration

// main.js
import { app, BrowserWindow, session } from 'electron';

app.whenReady().then(() => {
  // Content Security Policy
  session.defaultSession.webRequest.onHeadersReceived((details, callback) => {
    callback({
      responseHeaders: {
        ...details.responseHeaders,
        'Content-Security-Policy': [
          "default-src 'self'",
          "script-src 'self'",
          "style-src 'self' 'unsafe-inline'",
          "img-src 'self' data: https:",
          "connect-src 'self' https://api.example.com",
        ].join('; '),
      },
    });
  });

  // Prevent navigation to untrusted origins
  app.on('web-contents-created', (event, contents) => {
    contents.on('will-navigate', (event, navigationUrl) => {
      const parsedUrl = new URL(navigationUrl);
      const allowedOrigins = ['https://example.com'];

      if (!allowedOrigins.includes(parsedUrl.origin)) {
        event.preventDefault();
        console.warn('Blocked navigation to:', navigationUrl);
      }
    });

    // Prevent new window creation
    contents.setWindowOpenHandler(({ url }) => {
      // Open external links in system browser
      if (url.startsWith('https://')) {
        shell.openExternal(url);
      }
      return { action: 'deny' };
    });
  });

  const mainWindow = new BrowserWindow({
    webPreferences: {
      nodeIntegration: false,
      contextIsolation: true,
      sandbox: true,
      webSecurity: true,
      allowRunningInsecureContent: false,
      preload: path.join(__dirname, 'preload.js'),
    },
  });
});

// ✅ Expert: Validate all IPC inputs
ipcMain.handle('save-file', async (event, { path: filePath, content }) => {
  // Type validation
  if (typeof filePath !== 'string' || typeof content !== 'string') {
    throw new Error('Invalid input types');
  }

  // Path validation
  const resolvedPath = path.resolve(filePath);
  const userDataPath = app.getPath('userData');

  if (!resolvedPath.startsWith(userDataPath)) {
    throw new Error('Cannot write outside user data directory');
  }

  // Size validation
  if (content.length > 10 * 1024 * 1024) { // 10MB limit
    throw new Error('File too large');
  }

  await fs.writeFile(resolvedPath, content, 'utf-8');
  return { success: true };
});
```

