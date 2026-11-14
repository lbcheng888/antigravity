/** Running Node scripts with the app's own binary (ELECTRON_RUN_AS_NODE=1). */
import * as fs from 'fs';
import * as path from 'path';

let nodeBinary: string | undefined;

/**
 * The binary to run Node scripts with: on macOS the Helper app's
 * (LSUIElement=true), so no Dock icon bounces.
 */
export function electronNodeBinary(): string {
  nodeBinary ??= findNodeBinary();
  return nodeBinary;
}

function findNodeBinary(): string {
  if (process.platform !== 'darwin') {
    return process.execPath;
  }
  const appName = path.basename(process.execPath);
  const helperPath = path.join(
    path.dirname(process.execPath),
    '..',
    'Frameworks',
    `${appName} Helper.app`,
    'Contents',
    'MacOS',
    `${appName} Helper`,
  );
  return fs.existsSync(helperPath) ? helperPath : process.execPath;
}

/** A single-quoted sh word. */
export function shellQuote(word: string): string {
  return `'${word.replace(/'/g, "'\\''")}'`;
}

/** An sh script that runs `binary` as Node, with `args` before its own. */
export function nodeWrapperScript(binary: string, ...args: string[]): string {
  const words = [binary, ...args].map(shellQuote).join(' ');
  return `#!/bin/sh\nELECTRON_RUN_AS_NODE=1 exec ${words} "$@"\n`;
}
