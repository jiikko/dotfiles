# Electron Expert リファレンス — Native Integration & Packaging/Distribution

> electron-expert agent が該当領域を深掘り分析/レビューする際に Read する詳細リファレンス。
> Source of truth: dotfiles/_claude/references/electron-expert/native-packaging.md

### 4. Native Integration (Expert Level)

**System Tray**:
```javascript
// ✅ Expert: System tray with context menu
import { app, Tray, Menu, nativeImage } from 'electron';
import path from 'path';

let tray = null;

app.whenReady().then(() => {
  // Use template image for macOS dark mode support
  const icon = nativeImage.createFromPath(
    path.join(__dirname, 'assets/trayTemplate.png')
  );
  icon.setTemplateImage(true);

  tray = new Tray(icon);

  const contextMenu = Menu.buildFromTemplate([
    { label: 'Show App', click: () => mainWindow.show() },
    { label: 'Settings', click: () => openSettings() },
    { type: 'separator' },
    {
      label: 'Status',
      enabled: false,
      id: 'status',
    },
    { type: 'separator' },
    { label: 'Quit', click: () => app.quit() },
  ]);

  tray.setToolTip('My App');
  tray.setContextMenu(contextMenu);

  // Update status dynamically
  function updateStatus(status) {
    const item = contextMenu.getMenuItemById('status');
    item.label = `Status: ${status}`;
  }

  // Handle click (macOS: left click shows menu, Windows: right click)
  tray.on('click', () => {
    if (process.platform === 'darwin') {
      mainWindow.show();
    }
  });
});
```

**Native Menus**:
```javascript
// ✅ Expert: Application menu with keyboard shortcuts
import { Menu, app } from 'electron';

const isMac = process.platform === 'darwin';

const template = [
  // macOS app menu
  ...(isMac ? [{
    label: app.name,
    submenu: [
      { role: 'about' },
      { type: 'separator' },
      { label: 'Preferences...', accelerator: 'Cmd+,', click: openPreferences },
      { type: 'separator' },
      { role: 'services' },
      { type: 'separator' },
      { role: 'hide' },
      { role: 'hideOthers' },
      { role: 'unhide' },
      { type: 'separator' },
      { role: 'quit' },
    ],
  }] : []),

  // File menu
  {
    label: 'File',
    submenu: [
      { label: 'New', accelerator: 'CmdOrCtrl+N', click: createNew },
      { label: 'Open...', accelerator: 'CmdOrCtrl+O', click: openFile },
      { type: 'separator' },
      { label: 'Save', accelerator: 'CmdOrCtrl+S', click: save },
      { label: 'Save As...', accelerator: 'Shift+CmdOrCtrl+S', click: saveAs },
      { type: 'separator' },
      isMac ? { role: 'close' } : { role: 'quit' },
    ],
  },

  // Edit menu with clipboard
  {
    label: 'Edit',
    submenu: [
      { role: 'undo' },
      { role: 'redo' },
      { type: 'separator' },
      { role: 'cut' },
      { role: 'copy' },
      { role: 'paste' },
      { role: 'selectAll' },
    ],
  },

  // View menu
  {
    label: 'View',
    submenu: [
      { role: 'reload' },
      { role: 'forceReload' },
      { role: 'toggleDevTools' },
      { type: 'separator' },
      { role: 'resetZoom' },
      { role: 'zoomIn' },
      { role: 'zoomOut' },
      { type: 'separator' },
      { role: 'togglefullscreen' },
    ],
  },
];

const menu = Menu.buildFromTemplate(template);
Menu.setApplicationMenu(menu);
```

### 5. Packaging and Distribution (Expert Level)

**electron-builder Configuration**:
```javascript
// electron-builder.config.js
module.exports = {
  appId: 'com.company.appname',
  productName: 'My App',

  directories: {
    output: 'dist',
    buildResources: 'build',
  },

  files: [
    'dist/**/*',
    'package.json',
  ],

  // macOS
  mac: {
    category: 'public.app-category.productivity',
    target: [
      { target: 'dmg', arch: ['x64', 'arm64'] },
      { target: 'zip', arch: ['x64', 'arm64'] },
    ],
    hardenedRuntime: true,
    gatekeeperAssess: false,
    entitlements: 'build/entitlements.mac.plist',
    entitlementsInherit: 'build/entitlements.mac.plist',
  },

  // Windows
  win: {
    target: [
      { target: 'nsis', arch: ['x64'] },
      { target: 'portable', arch: ['x64'] },
    ],
    certificateFile: process.env.WIN_CERT_FILE,
    certificatePassword: process.env.WIN_CERT_PASSWORD,
  },

  // Linux
  linux: {
    target: ['AppImage', 'deb', 'rpm'],
    category: 'Utility',
  },

  // Auto-update
  publish: {
    provider: 'github',
    owner: 'your-org',
    repo: 'your-app',
    releaseType: 'release',
  },

  // Code signing for macOS notarization
  afterSign: 'scripts/notarize.js',
};
```

**Auto-Updates**:
```javascript
// ✅ Expert: Proper auto-update implementation
import { autoUpdater } from 'electron-updater';
import { app, dialog, BrowserWindow } from 'electron';
import log from 'electron-log';

// Configure logging
autoUpdater.logger = log;
autoUpdater.logger.transports.file.level = 'info';

// Disable auto-download for user control
autoUpdater.autoDownload = false;
autoUpdater.autoInstallOnAppQuit = true;

export function initAutoUpdater(mainWindow) {
  // Check for updates on startup (with delay)
  app.whenReady().then(() => {
    setTimeout(() => {
      autoUpdater.checkForUpdates();
    }, 3000);
  });

  autoUpdater.on('checking-for-update', () => {
    log.info('Checking for update...');
  });

  autoUpdater.on('update-available', (info) => {
    log.info('Update available:', info.version);

    // Notify renderer
    mainWindow.webContents.send('update-available', {
      version: info.version,
      releaseNotes: info.releaseNotes,
    });

    // Or show native dialog
    dialog.showMessageBox(mainWindow, {
      type: 'info',
      title: 'Update Available',
      message: `Version ${info.version} is available. Download now?`,
      buttons: ['Download', 'Later'],
    }).then((result) => {
      if (result.response === 0) {
        autoUpdater.downloadUpdate();
      }
    });
  });

  autoUpdater.on('update-not-available', () => {
    log.info('Update not available');
  });

  autoUpdater.on('download-progress', (progress) => {
    mainWindow.webContents.send('download-progress', {
      percent: progress.percent,
      bytesPerSecond: progress.bytesPerSecond,
    });

    // Update dock/taskbar progress
    mainWindow.setProgressBar(progress.percent / 100);
  });

  autoUpdater.on('update-downloaded', (info) => {
    log.info('Update downloaded');
    mainWindow.setProgressBar(-1); // Remove progress

    dialog.showMessageBox(mainWindow, {
      type: 'info',
      title: 'Update Ready',
      message: 'Restart now to apply the update?',
      buttons: ['Restart', 'Later'],
    }).then((result) => {
      if (result.response === 0) {
        autoUpdater.quitAndInstall();
      }
    });
  });

  autoUpdater.on('error', (error) => {
    log.error('Update error:', error);
    dialog.showErrorBox('Update Error', error.message);
  });
}
```

