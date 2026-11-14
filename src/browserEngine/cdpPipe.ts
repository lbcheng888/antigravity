import { EventEmitter } from 'events';

/** Event envelope emitted by {@link CdpPipeConnection} for CDP notifications. */
export interface CdpEvent {
  method: string;
  params: Record<string, unknown>;
  sessionId?: string;
}

interface PendingRequest {
  method: string;
  resolve: (value: unknown) => void;
  reject: (err: CdpError) => void;
}

interface CdpRawMessage {
  id?: number;
  result?: Record<string, unknown>;
  error?: {
    message: string;
    code?: number;
  };
  method?: string;
  params?: Record<string, unknown>;
  sessionId?: string;
}

/** Thrown for CDP error responses and for calls cut off by disconnect. */
export class CdpError extends Error {
  constructor(
    public readonly method: string,
    message: string,
    public readonly code?: number,
  ) {
    super(`${method}: ${message}`);
    this.name = 'CdpError';
  }
}

/**
 * JSON-RPC client for Chrome's `--remote-debugging-pipe` transport: Chrome
 * reads NUL-terminated JSON messages on fd 3 and writes them on fd 4. Uses
 * flattened sessions (`sessionId` on the envelope) so one pipe carries every
 * target; no TCP endpoint exists that another local process could attach to.
 *
 * Emits `event` (CdpEvent) and, once, `disconnect` when the read side ends.
 */
export class CdpPipeConnection extends EventEmitter {
  private nextId = 1;
  private pending = new Map<number, PendingRequest>();
  private readBuffer: Buffer[] = [];
  private closed = false;

  constructor(
    private readonly toBrowser: NodeJS.WritableStream,
    fromBrowser: NodeJS.ReadableStream,
  ) {
    super();
    fromBrowser.on('data', (chunk: Buffer) => this.onData(chunk));
    fromBrowser.on('close', () => this.onDisconnect());
    fromBrowser.on('end', () => this.onDisconnect());
    fromBrowser.on('error', () => this.onDisconnect());
    toBrowser.on('close', () => this.onDisconnect());
    toBrowser.on('error', () => this.onDisconnect());
  }

  get isClosed(): boolean {
    return this.closed;
  }

  send<T = Record<string, unknown>>(
    method: string,
    params: Record<string, unknown> = {},
    sessionId?: string,
  ): Promise<T> {
    if (this.closed) {
      return Promise.reject(new CdpError(method, 'pipe disconnected'));
    }
    const id = this.nextId++;
    const message = JSON.stringify(
      sessionId ? { id, method, params, sessionId } : { id, method, params },
    );
    return new Promise<T>((resolve, reject) => {
      this.pending.set(id, {
        method,
        resolve: resolve as (value: unknown) => void,
        reject,
      });
      this.toBrowser.write(`${message}\0`);
    });
  }

  private onData(chunk: Buffer): void {
    let start = 0;
    let end = chunk.indexOf(0);
    while (end !== -1) {
      this.readBuffer.push(chunk.subarray(start, end));
      const raw = Buffer.concat(this.readBuffer).toString('utf8');
      this.readBuffer = [];
      this.dispatch(raw);
      start = end + 1;
      end = chunk.indexOf(0, start);
    }
    if (start < chunk.length) {
      this.readBuffer.push(chunk.subarray(start));
    }
  }

  private dispatch(raw: string): void {
    let parsed: unknown;
    try {
      parsed = JSON.parse(raw);
    } catch {
      console.error('[browserEngine] dropped malformed CDP frame');
      return;
    }
    if (!parsed || typeof parsed !== 'object') {
      console.error('[browserEngine] dropped malformed CDP frame');
      return;
    }
    const message = parsed as CdpRawMessage;
    if (message.id !== undefined) {
      const pending = this.pending.get(message.id);
      if (!pending) {
        return;
      }
      this.pending.delete(message.id);
      if (message.error) {
        pending.reject(
          new CdpError(pending.method, message.error.message, message.error.code),
        );
      } else {
        pending.resolve(message.result ?? {});
      }
      return;
    }
    if (message.method) {
      const event: CdpEvent = {
        method: message.method,
        params: message.params ?? {},
      };
      if (message.sessionId) {
        event.sessionId = message.sessionId;
      }
      this.emit('event', event);
    }
  }

  private onDisconnect(): void {
    if (this.closed) {
      return;
    }
    this.closed = true;
    for (const pending of this.pending.values()) {
      pending.reject(new CdpError(pending.method, 'pipe disconnected'));
    }
    this.pending.clear();
    this.emit('disconnect');
  }
}
