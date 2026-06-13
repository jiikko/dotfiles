# Electron Expert リファレンス — Performance Optimization & Testing

> electron-expert agent が該当領域を深掘り分析/レビューする際に Read する詳細リファレンス。
> Source of truth: dotfiles/_claude/references/electron-expert/performance-testing.md

### 6. Performance Optimization (Expert Level)

```javascript
// ✅ Expert: Window management for performance
import { BrowserWindow, app } from 'electron';

// Lazy window creation
let settingsWindow = null;

function getSettingsWindow() {
  if (settingsWindow && !settingsWindow.isDestroyed()) {
    return settingsWindow;
  }

  settingsWindow = new BrowserWindow({
    width: 600,
    height: 400,
    show: false, // Don't show until ready
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
    },
  });

  settingsWindow.loadFile('settings.html');

  // Show when ready to prevent flash
  settingsWindow.once('ready-to-show', () => {
    settingsWindow.show();
  });

  settingsWindow.on('closed', () => {
    settingsWindow = null;
  });

  return settingsWindow;
}

// ✅ Expert: Background throttling control
mainWindow.webContents.setBackgroundThrottling(false); // For real-time apps

// ✅ Expert: Reduce memory usage
app.commandLine.appendSwitch('js-flags', '--max-old-space-size=512');

// ✅ Expert: GPU acceleration control
app.disableHardwareAcceleration(); // For some rendering issues
// Or specific features
app.commandLine.appendSwitch('disable-gpu-vsync');
app.commandLine.appendSwitch('disable-frame-rate-limit');
```

### 7. Testing Electron Apps

```javascript
// ✅ Expert: E2E testing with Playwright
import { test, expect, _electron as electron } from '@playwright/test';

test.describe('Electron App', () => {
  let electronApp;
  let window;

  test.beforeAll(async () => {
    electronApp = await electron.launch({
      args: ['.'],
      env: {
        ...process.env,
        NODE_ENV: 'test',
      },
    });

    window = await electronApp.firstWindow();
    await window.waitForLoadState('domcontentloaded');
  });

  test.afterAll(async () => {
    await electronApp.close();
  });

  test('should display main window', async () => {
    const title = await window.title();
    expect(title).toBe('My App');
  });

  test('should open file dialog', async () => {
    // Mock dialog
    await electronApp.evaluate(async ({ dialog }) => {
      dialog.showOpenDialog = () => Promise.resolve({
        canceled: false,
        filePaths: ['/test/file.txt'],
      });
    });

    await window.click('#open-file-btn');
    await expect(window.locator('#file-path')).toHaveText('/test/file.txt');
  });

  test('should save to user data', async () => {
    const userDataPath = await electronApp.evaluate(async ({ app }) => {
      return app.getPath('userData');
    });

    // Verify file operations in user data
    expect(userDataPath).toContain('My App');
  });
});
```

