/**
 * Answers ssh's prompts for one ssh run: ssh runs SSH_ASKPASS, a helper that
 * relays each prompt to the Hub over a unix socket in a private directory.
 */
import * as crypto from 'crypto';
import { EventEmitter } from 'events';
import * as fs from 'fs';
import * as net from 'net';
import * as path from 'path';
import { nodeWrapperScript } from '../nodeWrapper';
import { checkPrivate, makePrivateDir } from './privateDir';
import { RemoteError, sanitizeRemoteText } from './types';

const MAX_REQUEST_BYTES = 8 * 1024;
const MAX_PROMPT_CHARS = 2000;
/** ssh reads no more of an answer. */
const MAX_ANSWER_BYTES = 1023;
/** macOS's sun_path holds 104 bytes, with the NUL. */
const MAX_SOCKET_PATH_BYTES = 103;
/** For the helper's request line, which it sends at once. */
const REQUEST_TIMEOUT_MS = 10000;

/** Classification of an ssh askpass prompt. */
export type PromptKind = 'secret' | 'confirm' | 'notice';

/** Details for a prompt relayed from ssh to the Hub UI. */
export interface AskpassPrompt {
  kind: PromptKind;
  message: string;
  retry: boolean;
  signal: AbortSignal;
}

/** User or fallback outcome returned to the askpass relay for a prompt. */
export type AskpassAnswer =
  | { kind: 'secret'; text: string }
  | { kind: 'confirm'; yes: boolean }
  | { kind: 'cancel' };

/** Configuration for {@link AskpassRelay.start}. */
export interface AskpassOptions {
  nodeBinary: string;
  ask: (prompt: AskpassPrompt) => Promise<AskpassAnswer>;
}

/** Typed event map emitted by {@link AskpassRelay}. */
export interface AskpassRelayEvents {
  prompt: [open: boolean];
  cancel: [byUser: boolean];
}

type AskpassSocketReply = { answer: string } | { cancel: true };

/** One request per run; prints the answer, or exits 1. */
const HELPER = `'use strict';
const socket = require('net').connect(process.env.ANTIGRAVITY_SSH_ASKPASS_SOCKET);
let reply = '';
socket.setEncoding('utf8');
socket.on('connect', () =>
  socket.write(JSON.stringify({
    token: process.env.ANTIGRAVITY_SSH_ASKPASS_TOKEN,
    message: process.argv[2] || '',
    kind: process.env.SSH_ASKPASS_PROMPT || '',
    ppid: process.ppid,
  }) + '\\n'));
socket.on('data', (chunk) => {
  reply += chunk;
  const end = reply.indexOf('\\n');
  if (end < 0) return;
  socket.destroy();
  let answer;
  try { answer = JSON.parse(reply.slice(0, end)).answer; } catch {}
  if (typeof answer === 'string') process.stdout.write(answer + '\\n');
  else process.exitCode = 1;
});
socket.on('error', () => (process.exitCode = 1));
socket.on('close', () => { if (!reply.includes('\\n')) process.exitCode = 1; });
`;

/** SSH_ASKPASS must name a program: on Windows a follow-up. */
export function canPrompt(): boolean {
  return process.platform !== 'win32';
}

/** SSH_ASKPASS_PROMPT and ssh's raw text: what kind of question it is. */
export function promptKind(hint: unknown, message: string): PromptKind {
  if (hint === 'none') {
    return 'notice';
  }
  return hint === 'confirm' ||
    /\(yes\/no(?:\/\[fingerprint\])?\)[?:]\s*$/.test(message)
    ? 'confirm'
    : 'secret';
}

const cantPrompt = (why: string): RemoteError =>
  new RemoteError('UNKNOWN', `Can't answer ssh's prompts: ${why}`);

/**
 * Writes the helper and its sh wrapper into `userDataDir`/bin unless they're
 * there already; returns the wrapper, ssh's SSH_ASKPASS. ssh runs what's
 * there, so only you may write to that dir or to userData.
 */
async function installAskpass(
  userDataDir: string,
  nodeBinary: string,
): Promise<string> {
  const { real, why } = await checkPrivate(userDataDir);
  if (why) {
    throw cantPrompt(`${userDataDir}: ${why}.`);
  }
  const binDir = path.join(real, 'bin');
  const helper = path.join(binDir, 'agy-askpass.js');
  const wrapper = path.join(binDir, 'agy-askpass');
  await fs.promises.mkdir(binDir, { recursive: true, mode: 0o700 });
  const stat = await fs.promises.lstat(binDir);
  if (!stat.isDirectory() || stat.uid !== process.getuid?.()) {
    throw cantPrompt(`${binDir} must be a folder of yours.`);
  }
  // E.g. made under umask 002.
  if ((stat.mode & 0o022) !== 0) {
    await fs.promises.chmod(binDir, stat.mode & 0o7755);
  }
  try {
    await writeIfChanged(helper, HELPER, 0o600);
    await writeIfChanged(
      wrapper,
      nodeWrapperScript(nodeBinary, helper),
      0o700,
    );
  } catch (err) {
    // E.g. a folder in the way (EISDIR).
    const why = err instanceof Error && 'code' in err ? err.code : err;
    throw cantPrompt(`couldn't write the helper in ${binDir} (${String(why)}).`);
  }
  // E.g. userData on a noexec mount: ssh would get no answers.
  await fs.promises.access(wrapper, fs.constants.X_OK).catch(() => {
    throw cantPrompt(`${wrapper} can't run (is its folder mounted noexec?).`);
  });
  return wrapper;
}

/**
 * By rename, as ssh may be running the old file; chmod, as umask applies.
 * Keeps only a regular file of yours: a link, say, is replaced.
 */
async function writeIfChanged(
  file: string,
  content: string,
  mode: number,
): Promise<void> {
  const stat = await fs.promises.lstat(file).catch(() => undefined);
  const current =
    stat?.isFile() && stat.uid === process.getuid?.()
      ? await fs.promises.readFile(file, 'utf8').catch(() => null)
      : null;
  if (current !== content) {
    const temp = `${file}.${crypto.randomBytes(6).toString('hex')}`;
    try {
      await fs.promises.writeFile(temp, content, { mode, flag: 'wx' });
      await fs.promises.rename(temp, file);
    } finally {
      await fs.promises.rm(temp, { force: true });
    }
  }
  await fs.promises.chmod(file, mode);
}

/** What ssh reads as the answer to a `kind` prompt; undefined for Cancel. */
function answerText(
  kind: PromptKind,
  answer: AskpassAnswer,
): string | undefined {
  if (answer.kind === 'confirm' && kind === 'confirm') {
    return answer.yes ? 'yes' : 'no';
  }
  if (
    answer.kind === 'secret' &&
    kind === 'secret' &&
    Buffer.byteLength(answer.text) <= MAX_ANSWER_BYTES &&
    !/[\r\n\0]/.test(answer.text)
  ) {
    return answer.text;
  }
  return undefined;
}

function reply(socket: net.Socket, answer: AskpassSocketReply): void {
  socket.end(`${JSON.stringify(answer)}\n`);
}

/**
 * Events: `prompt` (true as one opens, false as it closes) and `cancel` (a
 * prompt got no answer: true if the user cancelled it, false if it couldn't
 * show or its answer was refused; it comes before ssh hears of it).
 */
export class AskpassRelay extends EventEmitter<AskpassRelayEvents> {
  private readonly token = crypto.randomBytes(32).toString('hex');
  private readonly server: net.Server = net.createServer((socket) =>
    this.accept(socket),
  );
  private readonly sockets = new Set<net.Socket>();
  /** One prompt at a time. */
  private queue: Promise<void> = Promise.resolve();
  /** Each asking ssh process's last secret prompt. */
  private readonly lastSecrets = new Map<number | undefined, string>();
  private disposing?: Promise<void>;

  constructor(
    /** The wrapper: ssh's SSH_ASKPASS. */
    readonly program: string,
    private readonly dir: string,
    private readonly ask: (prompt: AskpassPrompt) => Promise<AskpassAnswer>,
  ) {
    super();
  }

  /**
   * Installs the helper in `userDataDir`/bin, and listens in a new private
   * dir.
   */
  static async start(
    { nodeBinary, ask }: AskpassOptions,
    userDataDir: string,
  ): Promise<AskpassRelay> {
    const program = await installAskpass(userDataDir, nodeBinary);
    const dir = await makePrivateDir(
      userDataDir,
      "answer ssh's prompts",
      (d) =>
        Buffer.byteLength(path.join(d, 's')) > MAX_SOCKET_PATH_BYTES
          ? `too long a path for a socket (over ${MAX_SOCKET_PATH_BYTES} bytes)`
          : undefined,
    );
    const relay = new AskpassRelay(program, dir, ask);
    try {
      await new Promise<void>((resolve, reject) => {
        relay.server.once('error', reject);
        relay.server.listen(relay.socketPath, () => {
          relay.server.off('error', reject);
          resolve();
        });
      });
    } catch (err) {
      await relay.dispose();
      throw err;
    }
    // E.g. EMFILE on accept: it costs that helper, whose ssh gets no answer.
    relay.server.on('error', () => {});
    return relay;
  }

  get socketPath(): string {
    return path.join(this.dir, 's');
  }

  /** `base` plus what makes ssh ask through the helper. */
  env(base: NodeJS.ProcessEnv): NodeJS.ProcessEnv {
    const env: NodeJS.ProcessEnv = {
      ...base,
      SSH_ASKPASS: this.program,
      SSH_ASKPASS_REQUIRE: 'force',
      // OpenSSH before 8.4 asks only with a DISPLAY; macOS has later ones.
      ...(process.platform === 'linux' && base.DISPLAY === undefined
        ? { DISPLAY: '.' }
        : {}),
      ANTIGRAVITY_SSH_ASKPASS_SOCKET: this.socketPath,
      ANTIGRAVITY_SSH_ASKPASS_TOKEN: this.token,
    };
    // ssh sets it only for some prompts: an inherited one would misname the
    // rest.
    delete env.SSH_ASKPASS_PROMPT;
    return env;
  }

  /** Cancels the prompts, closes the socket and deletes its dir. */
  dispose(): Promise<void> {
    this.disposing ??= (async () => {
      // Each prompt ends with its helper's connection.
      for (const socket of this.sockets) {
        socket.destroy();
      }
      await new Promise<void>((resolve) => this.server.close(() => resolve()));
      await fs.promises
        .rm(this.dir, { recursive: true, force: true })
        .catch(() => {});
    })();
    return this.disposing;
  }

  private accept(socket: net.Socket): void {
    if (this.disposing) {
      socket.destroy();
      return;
    }
    this.sockets.add(socket);
    socket.on('close', () => this.sockets.delete(socket));
    // E.g. ECONNRESET: a helper that died before reading its answer.
    socket.on('error', () => {});
    socket.setTimeout(REQUEST_TIMEOUT_MS, () => socket.destroy());
    let request: Buffer = Buffer.alloc(0);
    const onData = (chunk: Buffer): void => {
      request = Buffer.concat([request, chunk]);
      const end = request.indexOf('\n');
      if ((end < 0 ? request.length : end) > MAX_REQUEST_BYTES) {
        socket.destroy();
      } else if (end >= 0) {
        socket.off('data', onData);
        socket.setTimeout(0);
        this.receive(socket, request.subarray(0, end).toString('utf8'));
      }
    };
    socket.on('data', onData);
  }

  private receive(socket: net.Socket, line: string): void {
    let request: unknown;
    try {
      request = JSON.parse(line);
    } catch {
      request = undefined;
    }
    const field = (name: string): unknown =>
      typeof request === 'object' && request !== null && name in request
        ? Reflect.get(request, name)
        : undefined;
    const token = field('token');
    const expected = Buffer.from(this.token);
    const given = Buffer.from(typeof token === 'string' ? token : '');
    if (
      given.length !== expected.length ||
      !crypto.timingSafeEqual(given, expected)
    ) {
      socket.destroy();
      return;
    }
    const raw = field('message');
    const text = typeof raw === 'string' ? raw : '';
    const ppid = field('ppid');
    // Before capping: a long host key prompt still ends with its question.
    const kind = promptKind(field('kind'), text);
    const message = sanitizeRemoteText(text, MAX_PROMPT_CHARS, {
      keepNewlines: true,
    });
    const asker = typeof ppid === 'number' ? ppid : undefined;
    this.queue = this.queue
      .then(() => this.prompt(socket, kind, message, asker))
      .catch(() => void socket.destroy());
  }

  private async prompt(
    socket: net.Socket,
    kind: PromptKind,
    message: string,
    asker: number | undefined,
  ): Promise<void> {
    if (this.disposing || socket.destroyed) {
      return;
    }
    const retry = kind === 'secret' && this.lastSecrets.get(asker) === message;
    if (kind === 'secret') {
      this.lastSecrets.set(asker, message);
    }
    const ended = new AbortController();
    const onGone = (): void => ended.abort();
    socket.once('close', onGone);
    // Unset if the prompt couldn't show.
    let answer: AskpassAnswer | undefined;
    try {
      this.emit('prompt', true);
      answer = await this.ask({ kind, message, retry, signal: ended.signal });
    } catch {
      // Answered below as a cancel, but not the user's.
    } finally {
      socket.off('close', onGone);
      this.emit('prompt', false);
    }
    if (ended.signal.aborted) {
      return;
    }
    const text = answer && answerText(kind, answer);
    if (text === undefined) {
      // First, so ssh is stopped before it acts on it (an empty password).
      this.emit('cancel', answer?.kind === 'cancel');
      reply(socket, { cancel: true });
      return;
    }
    reply(socket, { answer: text });
  }
}
