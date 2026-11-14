import { app } from 'electron';

export interface DebugConsoleMenuItem {
  role?: string;
  visible?: boolean;
  submenu?: DebugConsoleMenu;
}

export interface DebugConsoleMenu {
  items?: readonly DebugConsoleMenuItem[];
}

/**
 * Returns whether Chromium DevTools should be enabled for BrowserWindows.
 * Defaults to unpackaged local development builds only.
 */
export function isDebugConsoleEnabled(): boolean {
  return !app.isPackaged;
}

function hideDevTools(menuInstance: DebugConsoleMenu): void {
  // Lowercase comparison handles Electron's runtime role normalization ('toggledevtools').
  menuInstance.items?.forEach((item) => {
    if (item.role?.toLocaleLowerCase() === 'toggledevtools') {
      item.visible = false;
    }
    if (item.submenu) {
      hideDevTools(item.submenu);
    }
  });
}

/**
 * Hides the built-in Toggle Developer Tools menu item across all submenus
 * when the debug console is disabled.
 */
export function configureDebugConsoleMenu(menuInstance: DebugConsoleMenu): void {
  if (isDebugConsoleEnabled()) {
    return;
  }
  hideDevTools(menuInstance);
}
