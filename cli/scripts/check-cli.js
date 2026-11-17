#!/usr/bin/env node
/**
 * Verification script for cli/ offline extraction and TypeScript analysis project.
 * Checks required metadata/docs files, extracted directory counts against
 * metadata/embedded-manifest.json, JSONL integrity, and binary info parity.
 */

const fs = require('node:fs');
const path = require('node:path');
const { execFileSync } = require('node:child_process');

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
    'docs/deep-reverse-plan.md',
    'docs/reconstructed-subsystems.md',
    'metadata/macho-segments.json',
    'metadata/go-files.txt',
    'metadata/jetski-files.txt',
    'metadata/cli-files.txt',
    'metadata/jetski-packages.json',
    'metadata/jetski-functions.jsonl',
    'metadata/proto-services.json',
    'metadata/cli-commands.json',
    'metadata/embedded-manifest.json',
    'metadata/recovered-protos.json',
    'metadata/recovered-gotypes.json',
    'metadata/callgraph-summary.json',
    'src/types.ts',
    'src/metadataLoader.ts',
    'src/cliAnalyzer.ts',
    'src/deepAnalyzer.ts',
    'build/types.js',
    'build/metadataLoader.js',
    'build/cliAnalyzer.js',
    'build/deepAnalyzer.js',
    'scripts/extract-agy.py',
    'scripts/recover-protos.py',
    'scripts/recover-gotypes.py',
    'scripts/decompile-workbench.py',
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

  // Verify L3-A recovered protos
  const recoveredProtos = JSON.parse(fs.readFileSync(path.join(CLI_ROOT, 'metadata/recovered-protos.json'), 'utf8'));
  assert(recoveredProtos.summary && recoveredProtos.summary.problems.length === 0, 'recovered-protos.json reported problems');
  const protoCounts = countRegularFilesAndSymlinks(path.join(CLI_ROOT, 'reconstructed/proto'));
  assert(
    protoCounts.files === recoveredProtos.summary.total_protos && recoveredProtos.summary.total_protos === 400,
    `Recovered proto count mismatch: expected 400, summary=${recoveredProtos.summary.total_protos}, disk=${protoCounts.files}`,
  );

  // Verify L3-B recovered Go types
  const recoveredGoTypes = JSON.parse(fs.readFileSync(path.join(CLI_ROOT, 'metadata/recovered-gotypes.json'), 'utf8'));
  const goTypeCounts = countRegularFilesAndSymlinks(path.join(CLI_ROOT, 'reconstructed/go-types'));
  assert(
    goTypeCounts.files === recoveredGoTypes.summary.go_files_written && recoveredGoTypes.summary.go_files_written === 396,
    `Recovered Go types count mismatch: expected 396, summary=${recoveredGoTypes.summary.go_files_written}, disk=${goTypeCounts.files}`,
  );

  // Verify L4-A callgraph summary
  const callgraph = JSON.parse(fs.readFileSync(path.join(CLI_ROOT, 'metadata/callgraph-summary.json'), 'utf8'));
  assert(
    Array.isArray(callgraph.unresolved_targets) && callgraph.unresolved_targets.length === 0,
    `callgraph-summary.json has unresolved targets: ${callgraph.unresolved_targets && callgraph.unresolved_targets.length}`,
  );
  assert(callgraph.nodes && callgraph.nodes.length > 50000, 'callgraph-summary.json node count unexpectedly small');

  // Verify L4-B reconstructed Go sources
  const srcCounts = countRegularFilesAndSymlinks(path.join(CLI_ROOT, 'reconstructed/src'));
  assert(srcCounts.files >= 82, `Expected >= 82 reconstructed source files under reconstructed/src, found ${srcCounts.files}`);

  // Verify L5-L7 runnable Go SDK, Cortex Agent, Full-Stack Subsystems & CLI (go-sdk)
  const goSdkRequiredPaths = [
    'go-sdk/go.mod',
    'go-sdk/pkg/auth',
    'go-sdk/pkg/aicode',
    'go-sdk/pkg/cortex',
    'go-sdk/pkg/swarm',
    'go-sdk/pkg/acp',
    'go-sdk/pkg/mcp',
    'go-sdk/pkg/customizations',
    'go-sdk/pkg/sessionstore',
    'go-sdk/pkg/planexec',
    'go-sdk/pkg/sandbox',
    'go-sdk/pkg/swarmrun',
    'go-sdk/pkg/printmode',
    'go-sdk/cmd/agy-sdk/main.go',
    'go-sdk/bin/agy-sdk',
  ];
  const missingGoSdk = goSdkRequiredPaths.filter((rel) => !fs.existsSync(path.join(CLI_ROOT, rel)));
  assert(missingGoSdk.length === 0, `Missing required go-sdk paths: ${missingGoSdk.join(', ')}`);

  const goBin = '/Users/mac/.local/bin/go';
  let goSdkStatus = 'go-sdk (12 pkgs, layout ok, go vet skipped: go binary missing)';
  if (fs.existsSync(goBin)) {
    execFileSync(goBin, ['-C', 'go-sdk', 'vet', './...'], { cwd: CLI_ROOT, stdio: 'pipe' });
    goSdkStatus = 'go-sdk (12 pkgs, vet ok, bin/agy-sdk present)';
  }

  console.log(
    `OK: agy v${info.version} (${pkgReport.jetski_file_count} jetski files, ${pkgReport.cli_file_count} CLI files, ` +
      `${pkgReport.jetski_func_count} funcs, ${protoReport.service_count} RPC services; ` +
      `L3/L4: ${protoCounts.files} protos, ${recoveredGoTypes.summary.total_types} Go types/${goTypeCounts.files} pkgs, ` +
      `${callgraph.nodes.length} callgraph nodes, ${srcCounts.files} reconstructed files; ` +
      `L5-L7: ${goSdkStatus}) verified cleanly.`,
  );
}

main();
