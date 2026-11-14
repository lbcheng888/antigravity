import {
  dialog,
  webContents,
  type BrowserWindow,
  type MessageBoxOptions,
  type MessageBoxReturnValue,
  type WebFrameMain,
} from 'electron';
import {
  getFocusedHubWindow,
  getHubWindows,
  resolveHubWindowFromWebContents,
} from './utils';

export const WEBVIEW_DEVICE_CHOOSER_EVENT_CHANNEL =
  'webview:device-chooser-event';
export const WEBVIEW_DEVICE_CHOOSER_RESPONSE_CHANNEL =
  'webview:device-chooser-response';
export const WEBVIEW_DEVICE_CHOOSER_SUBSCRIBER_STATE_CHANNEL =
  'webview:device-chooser-subscriber-state';
export const CLAIM_TIMEOUT_MS = 500;
export const CHOOSER_SAFETY_TIMEOUT_MS = 5 * 60000;

/** Supported browser device chooser APIs. */
export type DeviceChooserKind = 'hid' | 'usb' | 'serial';

/** Normalized device or port entry presented in the chooser UI. */
export interface DeviceChooserItem {
  id: string;
  label: string;
}

/** Minimal WebHID device surface read by the device chooser. */
export interface DeviceChooserHidDevice {
  deviceId: string;
  name?: string;
  vendorId: number;
  productId: number;
}

/** Minimal WebUSB device surface read by the device chooser. */
export interface DeviceChooserUsbDevice {
  deviceId: string;
  productName?: string;
  vendorId: number;
  productId: number;
}

/** Minimal Web Serial port surface read by the device chooser. */
export interface DeviceChooserSerialPort {
  portId: string;
  portName: string;
  displayName?: string;
}

/** Minimal frame surface carrying a requesting origin. */
export interface DeviceChooserFrame {
  origin: string;
}

/** Minimal preventable Electron event passed to session `select-*` listeners. */
export interface DeviceChooserPreventableEvent {
  preventDefault(): void;
}

/** Minimal `select-hid-device` details payload. */
export interface DeviceChooserSelectHidDetails {
  deviceList: DeviceChooserHidDevice[];
  frame: DeviceChooserFrame | null | undefined;
}

/** Minimal `select-usb-device` details payload. */
export interface DeviceChooserSelectUsbDetails {
  deviceList: DeviceChooserUsbDevice[];
  frame: DeviceChooserFrame | null | undefined;
}

/** Minimal `hid-device-added` / `hid-device-removed` details payload. */
export interface DeviceChooserHidHotPlugDetails {
  device: DeviceChooserHidDevice;
  frame: DeviceChooserFrame | null | undefined;
}

/** Minimal `WebContents` reference passed to USB/serial hot-plug events. */
export interface DeviceChooserHotPlugWebContents {
  id?: number;
}

/** Lifecycle events observed on a subscribed Hub host sender. */
export type DeviceChooserHostEvent =
  | 'destroyed'
  | 'render-process-gone'
  | 'did-navigate';

/** Lifecycle events observed on a requesting guest `WebContents`. */
export type DeviceChooserOwnerEvent =
  | 'destroyed'
  | 'did-navigate'
  | 'render-process-gone';

/** Minimal identity shape used to compare IPC host senders. */
export interface DeviceChooserHostIdentity {
  id?: number;
}

/** IPC payload emitted on {@link WEBVIEW_DEVICE_CHOOSER_EVENT_CHANNEL}. */
export type DeviceChooserEventPayload =
  | {
      type: 'open';
      requestId: string;
      webContentsId: number;
      tabKey?: string;
      kind: DeviceChooserKind;
      origin: string;
      devices: DeviceChooserItem[];
    }
  | {
      type: 'update';
      requestId: string;
      devices: DeviceChooserItem[];
    }
  | {
      type: 'close';
      requestId: string;
    };

/** Minimal Hub host `WebContents` surface subscribed to device chooser events. */
export interface DeviceChooserHostSender {
  id: number;
  isDestroyed(): boolean;
  send(channel: string, payload: DeviceChooserEventPayload): void;
  on?(event: DeviceChooserHostEvent, listener: () => void): unknown;
  removeListener?(event: DeviceChooserHostEvent, listener: () => void): unknown;
}

/** Minimal requesting guest `WebContents` surface handled by the device chooser. */
export interface DeviceChooserOwner {
  id: number;
  hostWebContents?: DeviceChooserHostSender | null;
  mainFrame?: DeviceChooserFrame | null;
  isDestroyed(): boolean;
  once(event: 'destroyed', listener: () => void): unknown;
  on?(event: DeviceChooserOwnerEvent, listener: () => void): unknown;
  removeListener(event: DeviceChooserOwnerEvent, listener: () => void): unknown;
}

/** Minimal parent window surface used for native fallback message boxes. */
export interface DeviceChooserWindowLike {
  isDestroyed?: () => boolean;
}

/** Minimal Electron `Session` surface configured for device chooser interception. */
export interface DeviceChooserSession {
  on(
    event: 'select-hid-device',
    listener: (
      event: DeviceChooserPreventableEvent,
      details: DeviceChooserSelectHidDetails,
      callback: (deviceId?: string | null) => void,
    ) => void,
  ): unknown;
  on(
    event: 'select-usb-device',
    listener: (
      event: DeviceChooserPreventableEvent,
      details: DeviceChooserSelectUsbDetails,
      callback: (deviceId?: string) => void,
    ) => void,
  ): unknown;
  on(
    event: 'select-serial-port',
    listener: (
      event: DeviceChooserPreventableEvent,
      portList: DeviceChooserSerialPort[],
      webContents: DeviceChooserOwner | null | undefined,
      callback: (portId: string) => void,
    ) => void,
  ): unknown;
  on(
    event: 'hid-device-added' | 'hid-device-removed',
    listener: (event: unknown, details: DeviceChooserHidHotPlugDetails) => void,
  ): unknown;
  on(
    event: 'usb-device-added' | 'usb-device-removed',
    listener: (
      event: unknown,
      device: DeviceChooserUsbDevice,
      webContents: DeviceChooserHotPlugWebContents | null | undefined,
    ) => void,
  ): unknown;
  on(
    event: 'serial-port-added' | 'serial-port-removed',
    listener: (
      event: unknown,
      port: DeviceChooserSerialPort,
      webContents: DeviceChooserHotPlugWebContents | null | undefined,
    ) => void,
  ): unknown;
  off?(
    event: 'hid-device-added' | 'hid-device-removed',
    listener: (event: unknown, details: DeviceChooserHidHotPlugDetails) => void,
  ): unknown;
  off?(
    event: 'usb-device-added' | 'usb-device-removed',
    listener: (
      event: unknown,
      device: DeviceChooserUsbDevice,
      webContents: DeviceChooserHotPlugWebContents | null | undefined,
    ) => void,
  ): unknown;
  off?(
    event: 'serial-port-added' | 'serial-port-removed',
    listener: (
      event: unknown,
      port: DeviceChooserSerialPort,
      webContents: DeviceChooserHotPlugWebContents | null | undefined,
    ) => void,
  ): unknown;
}

/** Resolved Hub host context for a guest `WebContents` ID. */
export interface DeviceChooserHostContext {
  hostSender: DeviceChooserHostSender;
  tabKey?: string;
  visible: boolean;
  isAgentStepActive?: boolean;
}

/** Maps a guest `webContentsId` to its owning Hub host context, if any. */
export type DeviceChooserHostResolver = (
  webContentsId: number,
) => DeviceChooserHostContext | null | undefined;

/** Renderer response payload sent over {@link WEBVIEW_DEVICE_CHOOSER_RESPONSE_CHANNEL}. */
export type DeviceChooserResolution =
  | { action: 'claim' | 'unclaimed' | 'cancel' }
  | { action: 'select'; deviceId?: string };

/** Options accepted by {@link createDeviceChooserController}. */
export interface DeviceChooserControllerOptions {
  hostResolver?: DeviceChooserHostResolver;
  isHeadless?: () => boolean;
  getParentWindow?: () => DeviceChooserWindowLike | null | undefined;
  showMessageBox?: (
    win: DeviceChooserWindowLike,
    boxOpts: MessageBoxOptions,
  ) => Promise<Pick<MessageBoxReturnValue, 'response'>>;
  webContentsFromFrame?: (
    frame: DeviceChooserFrame,
  ) => DeviceChooserOwner | null | undefined;
  claimTimeoutMs?: number;
  safetyTimeoutMs?: number;
}

/** Controller interface returned by {@link createDeviceChooserController}. */
export interface DeviceChooserController {
  configureSession: (
    targetSession: DeviceChooserSession | null | undefined,
  ) => void;
  setHostSubscribed: (
    host: DeviceChooserHostSender | null | undefined,
    subscribed: boolean,
  ) => void;
  respond: (
    callerSender:
      | DeviceChooserHostSender
      | DeviceChooserHostIdentity
      | null
      | undefined,
    requestId: string,
    resolution: DeviceChooserResolution,
  ) => boolean;
  cancelForGuest: (webContentsId: number) => void;
  setHostResolver: (resolver?: DeviceChooserHostResolver) => void;
}

interface SubscribedHostRecord {
  sender: DeviceChooserHostSender;
  detach: () => void;
}

interface ActiveChooserEntry {
  hostSender: DeviceChooserHostSender | undefined;
  claimed: boolean;
  pending: DeviceChooserItem[];
  clearClaimTimer: () => void;
  severToFallback: () => void;
  settle: (deviceId: string | undefined, notifyHostClose?: boolean) => void;
}

const KIND_NOUNS: Record<DeviceChooserKind, string> = {
  hid: 'HID device',
  usb: 'USB device',
  serial: 'serial port',
};

const hex4 = (n: number): string => n.toString(16).padStart(4, '0');

const hidItem = (d: DeviceChooserHidDevice): DeviceChooserItem => ({
  id: d.deviceId,
  label: `${d.name?.trim() || 'Unknown device'} (${hex4(d.vendorId)}:${hex4(d.productId)})`,
});

const usbItem = (d: DeviceChooserUsbDevice): DeviceChooserItem => ({
  id: d.deviceId,
  label: `${d.productName ?? 'Unknown device'} (${hex4(d.vendorId)}:${hex4(d.productId)})`,
});

const serialItem = (p: DeviceChooserSerialPort): DeviceChooserItem => ({
  id: p.portId,
  label: p.displayName ?? p.portName,
});

const matchesHost = (
  a: DeviceChooserHostIdentity | null | undefined,
  b: DeviceChooserHostIdentity | null | undefined,
): boolean =>
  Boolean(a && b && (a === b || (typeof b.id === 'number' && a.id === b.id)));

function sendEvent(
  host: DeviceChooserHostSender | undefined,
  payload: DeviceChooserEventPayload,
): void {
  if (host && !host.isDestroyed()) {
    try {
      host.send(WEBVIEW_DEVICE_CHOOSER_EVENT_CHANNEL, payload);
    } catch {
      // Ignore dead renderer frames during teardown.
    }
  }
}

function subscribeHotPlug(
  s: DeviceChooserSession,
  kind: DeviceChooserKind,
  ownerId: number,
  fromFrame: (
    frame: DeviceChooserFrame,
  ) => DeviceChooserOwner | null | undefined,
  onChange: (device: DeviceChooserItem, present: boolean) => void,
): () => void {
  if (kind === 'hid') {
    const fn =
      (present: boolean) =>
      (_e: unknown, d: DeviceChooserHidHotPlugDetails): void => {
        if (d.frame && fromFrame(d.frame)?.id === ownerId) {
          onChange(hidItem(d.device), present);
        }
      };
    const [add, rem] = [fn(true), fn(false)];
    s.on('hid-device-added', add);
    s.on('hid-device-removed', rem);
    return () => {
      s.off!('hid-device-added', add);
      s.off!('hid-device-removed', rem);
    };
  }
  if (kind === 'usb') {
    const fn =
      (present: boolean) =>
      (
        _e: unknown,
        d: DeviceChooserUsbDevice,
        wc: DeviceChooserHotPlugWebContents | null | undefined,
      ): void => {
        if (wc?.id === ownerId) {
          onChange(usbItem(d), present);
        }
      };
    const [add, rem] = [fn(true), fn(false)];
    s.on('usb-device-added', add);
    s.on('usb-device-removed', rem);
    return () => {
      s.off!('usb-device-added', add);
      s.off!('usb-device-removed', rem);
    };
  }
  const fn =
    (present: boolean) =>
    (
      _e: unknown,
      p: DeviceChooserSerialPort,
      wc: DeviceChooserHotPlugWebContents | null | undefined,
    ): void => {
      if (wc?.id === ownerId) {
        onChange(serialItem(p), present);
      }
    };
  const [add, rem] = [fn(true), fn(false)];
  s.on('serial-port-added', add);
  s.on('serial-port-removed', rem);
  return () => {
    s.off!('serial-port-added', add);
    s.off!('serial-port-removed', rem);
  };
}

/**
 * Coordinates WebHID, WebUSB, and Web Serial selection across subscribed Hub
 * renderers, falling back to a native message box when unclaimed.
 */
export function createDeviceChooserController(
  options?: DeviceChooserControllerOptions,
): DeviceChooserController {
  const configuredSessions = new WeakSet<DeviceChooserSession>();
  const subscribedHosts = new Map<number, SubscribedHostRecord>();
  const activeByRequestId = new Map<string, ActiveChooserEntry>();
  const activeByOwnerId = new Map<number, ActiveChooserEntry>();
  let nextSeq = 0;
  let hostResolver: DeviceChooserHostResolver | undefined = options?.hostResolver;

  const isHeadless =
    options?.isHeadless ??
    (() => process.env.ELECTRON_OZONE_PLATFORM_HINT === 'headless');
  const getParentWindow =
    options?.getParentWindow ??
    (() => getFocusedHubWindow() ?? getHubWindows()[0] ?? null);
  const showMessageBox =
    options?.showMessageBox ??
    ((win, boxOpts) => dialog.showMessageBox(win as BrowserWindow, boxOpts));
  const fromFrame =
    options?.webContentsFromFrame ??
    ((frame) => {
      try {
        return webContents.fromFrame(frame as WebFrameMain);
      } catch {
        return undefined;
      }
    });
  const claimTimeoutMs = options?.claimTimeoutMs ?? CLAIM_TIMEOUT_MS;
  const safetyTimeoutMs = options?.safetyTimeoutMs ?? CHOOSER_SAFETY_TIMEOUT_MS;

  const dropHost = (
    host: DeviceChooserHostSender,
    notifyClose: boolean,
  ): void => {
    const rec = subscribedHosts.get(host.id);
    if (rec) {
      subscribedHosts.delete(host.id);
      rec.detach();
    }
    for (const entry of [...activeByRequestId.values()]) {
      if (matchesHost(entry.hostSender, host)) {
        entry.settle(undefined, notifyClose);
      }
    }
  };

  const setHostSubscribed = (
    host: DeviceChooserHostSender | null | undefined,
    subscribed: boolean,
  ): void => {
    if (!host) {
      return;
    }
    if (!subscribed) {
      dropHost(host, true);
      return;
    }
    if (host.isDestroyed() || subscribedHosts.get(host.id)?.sender === host) {
      return;
    }
    subscribedHosts.get(host.id)?.detach();
    const onLost = (): void => dropHost(host, false);
    host.on?.('destroyed', onLost);
    host.on?.('render-process-gone', onLost);
    host.on?.('did-navigate', onLost);
    subscribedHosts.set(host.id, {
      sender: host,
      detach: () => {
        host.removeListener?.('destroyed', onLost);
        host.removeListener?.('render-process-gone', onLost);
        host.removeListener?.('did-navigate', onLost);
      },
    });
  };

  const cancelForGuest = (webContentsId: number): void => {
    activeByOwnerId.get(webContentsId)?.settle(undefined, true);
  };

  const startRequest = (
    targetSession: DeviceChooserSession,
    kind: DeviceChooserKind,
    owner: DeviceChooserOwner,
    origin: string,
    devices: DeviceChooserItem[],
    respondToElectron: (deviceId: string | undefined) => void,
  ): void => {
    const wcv = hostResolver?.(owner.id) ?? null;
    if (wcv && (!wcv.visible || wcv.isAgentStepActive)) {
      cancelForGuest(owner.id);
      respondToElectron(undefined);
      return;
    }
    const hostCtx: {
      hostSender: DeviceChooserHostSender;
      tabKey: string | undefined;
    } | null = wcv
      ? { hostSender: wcv.hostSender, tabKey: wcv.tabKey }
      : owner.hostWebContents && !owner.hostWebContents.isDestroyed()
        ? { hostSender: owner.hostWebContents, tabKey: undefined }
        : null;

    cancelForGuest(owner.id);
    const requestId = `device-chooser-${++nextSeq}`;
    const pending = [...devices];
    let settled = false;
    let inFallback = false;
    let refreshRequested = false;
    let fallbackController = new AbortController();
    let claimTimer: ReturnType<typeof setTimeout> | undefined;
    let safetyTimer: ReturnType<typeof setTimeout> | undefined;
    const inPaneHost =
      hostCtx &&
      !hostCtx.hostSender.isDestroyed() &&
      subscribedHosts.has(hostCtx.hostSender.id)
        ? hostCtx.hostSender
        : undefined;
    let unsubscribeHotPlug: () => void = () => {};

    const clearTimers = (): void => {
      clearTimeout(claimTimer);
      clearTimeout(safetyTimer);
      claimTimer = safetyTimer = undefined;
    };

    const hasOwnerOn = typeof owner.on === 'function';
    const onOwnerAbort = (): void => settle(undefined, true);

    function settle(
      deviceId: string | undefined,
      notifyHostClose = false,
    ): void {
      if (settled) {
        return;
      }
      settled = true;
      clearTimers();
      const wasInPane = activeByRequestId.delete(requestId);
      if (activeByOwnerId.get(owner.id) === entry) {
        activeByOwnerId.delete(owner.id);
      }
      unsubscribeHotPlug();
      owner.removeListener('destroyed', onOwnerAbort);
      if (hasOwnerOn) {
        owner.removeListener('did-navigate', onOwnerAbort);
        owner.removeListener('render-process-gone', onOwnerAbort);
      }
      fallbackController.abort();
      if (notifyHostClose && wasInPane) {
        sendEvent(inPaneHost, { type: 'close', requestId });
      }
      respondToElectron(deviceId);
    }

    const runNativeFallback = async (): Promise<void> => {
      if (settled) {
        return;
      }
      const win = resolveHubWindowFromWebContents(owner) ?? getParentWindow();
      if (!win || win.isDestroyed?.()) {
        settle(undefined, false);
        return;
      }
      inFallback = true;
      try {
        for (;;) {
          const snapshot = Object.freeze([...pending]);
          refreshRequested = false;
          fallbackController = new AbortController();
          const cancelId = snapshot.length;
          const { response } = await showMessageBox(win, {
            type: 'question',
            title: 'Connect a device',
            message: `${origin} wants to connect to a ${KIND_NOUNS[kind]}`,
            detail: cancelId
              ? 'Select a device:'
              : 'No compatible devices found.',
            buttons: [...snapshot.map(({ label }) => label), 'Cancel'],
            cancelId,
            defaultId: cancelId,
            signal: fallbackController.signal,
          });
          if (settled) {
            return;
          }
          if (
            refreshRequested &&
            response === cancelId &&
            !owner.isDestroyed() &&
            !win.isDestroyed?.()
          ) {
            continue;
          }
          settle(snapshot[response]?.id, false);
          return;
        }
      } catch (err) {
        if (!settled) {
          console.warn('[DeviceChooser] Failed to choose a device:', err);
          settle(undefined, false);
        }
      }
    };

    const entry: ActiveChooserEntry = {
      hostSender: inPaneHost,
      claimed: false,
      pending,
      clearClaimTimer: () => {
        clearTimeout(claimTimer);
        claimTimer = undefined;
      },
      severToFallback: () => {
        if (settled || !activeByRequestId.delete(requestId)) {
          return;
        }
        clearTimers();
        sendEvent(inPaneHost, { type: 'close', requestId });
        void runNativeFallback();
      },
      settle,
    };

    unsubscribeHotPlug = subscribeHotPlug(
      targetSession,
      kind,
      owner.id,
      fromFrame,
      (device, present) => {
        if (settled) {
          return;
        }
        const idx = pending.findIndex(({ id }) => id === device.id);
        if (!present && idx < 0) {
          return;
        }
        if (present) {
          if (idx >= 0) {
            pending[idx] = device;
          } else {
            pending.push(device);
          }
        } else {
          pending.splice(idx, 1);
        }
        if (activeByRequestId.has(requestId)) {
          sendEvent(inPaneHost, {
            type: 'update',
            requestId,
            devices: [...pending],
          });
        } else if (inFallback) {
          refreshRequested = true;
          fallbackController.abort();
        }
      },
    );

    owner.once('destroyed', onOwnerAbort);
    if (hasOwnerOn) {
      owner.on!('did-navigate', onOwnerAbort);
      owner.on!('render-process-gone', onOwnerAbort);
    }
    if (settled) {
      return;
    }

    activeByOwnerId.set(owner.id, entry);
    if (inPaneHost && hostCtx) {
      activeByRequestId.set(requestId, entry);
      sendEvent(inPaneHost, {
        type: 'open',
        requestId,
        webContentsId: owner.id,
        ...(hostCtx.tabKey ? { tabKey: hostCtx.tabKey } : {}),
        kind,
        origin,
        devices: [...pending],
      });
      claimTimer = setTimeout(() => {
        if (!entry.claimed) {
          entry.severToFallback();
        }
      }, claimTimeoutMs);
      safetyTimer = setTimeout(() => settle(undefined, true), safetyTimeoutMs);
      return;
    }
    void runNativeFallback();
  };

  const configureSession = (
    targetSession: DeviceChooserSession | null | undefined,
  ): void => {
    if (
      !targetSession ||
      typeof targetSession.on !== 'function' ||
      configuredSessions.has(targetSession)
    ) {
      return;
    }
    configuredSessions.add(targetSession);
    const onFrameDevice = <T>(
      details: {
        frame: DeviceChooserFrame | null | undefined;
        deviceList: T[];
      },
      cb: (deviceId?: string) => void,
      kind: DeviceChooserKind,
      toItem: (device: T) => DeviceChooserItem,
    ): void => {
      const owner = details.frame ? fromFrame(details.frame) : undefined;
      if (!details.frame || !owner || owner.isDestroyed() || isHeadless()) {
        cb();
        return;
      }
      startRequest(
        targetSession,
        kind,
        owner,
        details.frame.origin,
        details.deviceList.map(toItem),
        (id) => (id === undefined ? cb() : cb(id)),
      );
    };

    targetSession.on('select-hid-device', (event, details, cb) => {
      event.preventDefault();
      onFrameDevice(details, cb, 'hid', hidItem);
    });
    targetSession.on('select-usb-device', (event, details, cb) => {
      event.preventDefault();
      onFrameDevice(details, cb, 'usb', usbItem);
    });
    targetSession.on('select-serial-port', (event, portList, owner, cb) => {
      event.preventDefault();
      if (!owner || owner.isDestroyed() || !owner.mainFrame || isHeadless()) {
        cb('');
        return;
      }
      startRequest(
        targetSession,
        'serial',
        owner,
        owner.mainFrame.origin,
        portList.map(serialItem),
        (id) => cb(id ?? ''),
      );
    });
  };

  return {
    configureSession,
    setHostSubscribed,
    respond: (callerSender, requestId, resolution) => {
      const entry = activeByRequestId.get(requestId);
      if (!entry || !matchesHost(entry.hostSender, callerSender)) {
        return false;
      }
      if (resolution.action === 'claim') {
        entry.clearClaimTimer();
        entry.claimed = true;
        return true;
      }
      if (resolution.action === 'unclaimed') {
        if (entry.claimed) {
          return false;
        }
        entry.severToFallback();
        return true;
      }
      if (resolution.action === 'cancel') {
        entry.settle(undefined, false);
        return true;
      }
      if (
        resolution.action === 'select' &&
        typeof resolution.deviceId === 'string' &&
        resolution.deviceId.length > 0
      ) {
        const matched = entry.pending.some(
          ({ id }) => id === resolution.deviceId,
        );
        entry.settle(matched ? resolution.deviceId : undefined, false);
        return matched;
      }
      return false;
    },
    cancelForGuest,
    setHostResolver: (resolver) => {
      hostResolver = resolver;
    },
  };
}

/** Creates a standalone session configurator backed by a dedicated chooser controller. */
export function createDeviceChooserSessionConfigurator(
  options?: DeviceChooserControllerOptions,
): (targetSession: DeviceChooserSession | null | undefined) => void {
  return createDeviceChooserController(options).configureSession;
}

export const deviceChooserController: DeviceChooserController =
  createDeviceChooserController();

export const configureSessionDeviceChooser: (
  targetSession: DeviceChooserSession | null | undefined,
) => void = deviceChooserController.configureSession;
