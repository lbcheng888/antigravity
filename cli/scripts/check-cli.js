#!/usr/bin/env node
/**
 * Verification script for cli/ offline extraction and TypeScript analysis project.
 * Checks required metadata/docs files, extracted directory counts against
 * metadata/embedded-manifest.json, JSONL integrity, and binary info parity.
 */

const fs = require('node:fs');
const path = require('node:path');

const CLI_ROOT = path.resolve(__dirname, '..');

function assert(cond, message) {
  if (!cond) {
    throw new Error(message);
  }
}

function countRegularFilesAndSymlinks(dir) {
  let files = 0;
  let symlinks = 0;
  const entries = fs.readdirSync(dir, { withFileTypes: true });
  for (const entry of entries) {
    const full = path.join(dir, entry.name);
    if (entry.isSymbolicLink()) {
      symlinks += 1;
    } else if (entry.isDirectory()) {
      const sub = countRegularFilesAndSymlinks(full);
      files += sub.files;
      symlinks += sub.symlinks;
    } else if (entry.isFile()) {
      files += 1;
    }
  }
  return { files, symlinks };
}

function main() {
  const requiredFiles = [
    'README.md',
    'package.json',
    'tsconfig.json',
    'bin/agy-info.json',
    'docs/architecture.md',
    'docs/auth-and-requests.md',
    'docs/cli-reference.md',
    'docs/package-map.md',
    'metadata/macho-segments.json',
    'metadata/go-files.txt',
    'metadata/jetski-files.txt',
    'metadata/cli-files.txt',
    'metadata/jetski-packages.json',
    'metadata/jetski-functions.jsonl',
    'metadata/proto-services.json',
    'metadata/cli-commands.json',
    'metadata/embedded-manifest.json',
    'src/types.ts',
    'src/metadataLoader.ts',
    'src/cliAnalyzer.ts',
    'build/types.js',
    'build/metadataLoader.js',
    'build/cliAnalyzer.js',
    'scripts/extract-agy.py',
    'scripts/check-cli.js',
  ];

  const missing = requiredFiles.filter((rel) => !fs.existsSync(path.join(CLI_ROOT, rel)));
  assert(missing.length === 0, `Missing required files: ${missing.join(', ')}`);

  const info = JSON.parse(fs.readFileSync(path.join(CLI_ROOT, 'bin/agy-info.json'), 'utf8'));
  assert(info.version && info.sha256 && info.size_bytes > 0, 'Invalid bin/agy-info.json');

  const manifest = JSON.parse(fs.readFileSync(path.join(CLI_ROOT, 'metadata/embedded-manifest.json'), 'utf8'));
  assert(manifest.source_sha256 === info.sha256, 'Manifest SHA-256 mismatch with agy-info.json');

  // Verify extracted embed.FS directories
  for (const [name, entry] of Object.entries(manifest.embed_fs)) {
    const dir = path.join(CLI_ROOT, 'extracted', name);
    assert(fs.existsSync(dir), `Missing extracted embed_fs dir: ${dir}`);
    const counts = countRegularFilesAndSymlinks(dir);
    assert(
      counts.files === entry.files,
      `File count mismatch for embed_fs/${name}: expected ${entry.files}, found ${counts.files}`,
    );
  }

  // Verify extracted ZIP directories
  for (const [name, entry] of Object.entries(manifest.zips)) {
    const dir = path.join(CLI_ROOT, 'extracted', name);
    assert(fs.existsSync(dir), `Missing extracted zip dir: ${dir}`);
    const counts = countRegularFilesAndSymlinks(dir);
    assert(
      counts.files === entry.files,
      `File count mismatch for zips/${name}: expected ${entry.files}, found ${counts.files}`,
    );
  }

  // Verify extracted tar.gz directories
  for (const [name, entry] of Object.entries(manifest.targz)) {
    const dir = path.join(CLI_ROOT, 'extracted', name);
    assert(fs.existsSync(dir), `Missing extracted targz dir: ${dir}`);
    const counts = countRegularFilesAndSymlinks(dir);
    assert(
      counts.files === entry.files && counts.symlinks === entry.symlinks,
      `Count mismatch for targz/${name}: expected ${entry.files}f/${entry.symlinks}l, found ${counts.files}f/${counts.symlinks}l`,
    );
  }

  // Verify fonts & proto descriptors
  for (const f of manifest.fonts) {
    assert(fs.existsSync(path.join(CLI_ROOT, 'extracted/fonts', f.file)), `Missing font: ${f.file}`);
  }
  for (const p of manifest.proto_descriptors) {
    assert(
      fs.existsSync(path.join(CLI_ROOT, 'extracted/proto-descriptors', p.file)),
      `Missing proto descriptor: ${p.file}`,
    );
  }

  // Verify JSONL & package summary counts
  const pkgReport = JSON.parse(fs.readFileSync(path.join(CLI_ROOT, 'metadata/jetski-packages.json'), 'utf8'));
  const jsonlLines = fs
    .readFileSync(path.join(CLI_ROOT, 'metadata/jetski-functions.jsonl'), 'utf8')
    .trim()
    .split('\n');
  assert(
    jsonlLines.length === pkgReport.jetski_func_count,
    `jetski-functions.jsonl count (${jsonlLines.length}) != jetski_func_count (${pkgReport.jetski_func_count})`,
  );
  // Spot check first and last JSONL lines parse cleanly
  JSON.parse(jsonlLines[0]);
  JSON.parse(jsonlLines[jsonlLines.length - 1]);

  const protoReport = JSON.parse(fs.readFileSync(path.join(CLI_ROOT, 'metadata/proto-services.json'), 'utf8'));
  assert(protoReport.service_count === protoReport.services.length && protoReport.service_count > 0, 'Invalid proto-services.json');

  console.log(
    `OK: agy v${info.version} (${pkgReport.jetski_file_count} jetski files, ${pkgReport.cli_file_count} CLI files, ` +
      `${pkgReport.jetski_func_count} funcs, ${protoReport.service_count} RPC services) verified cleanly.`,
  );
}

main();
