import {
  CLI_ROOT,
  loadAgyInfo,
  loadCliCommands,
  loadEmbeddedManifest,
  loadMachOLayout,
  loadPackagesReport,
  loadProtoServices,
  searchJetskiFunctions,
  summarizeByLayer,
} from './metadataLoader';

function formatBytes(bytes: number): string {
  if (bytes >= 1024 * 1024) {
    return `${(bytes / (1024 * 1024)).toFixed(2)} MB`;
  }
  if (bytes >= 1024) {
    return `${(bytes / 1024).toFixed(1)} KB`;
  }
  return `${bytes} B`;
}

function main(): void {
  const args = process.argv.slice(2);
  const queryArg = args.find((a) => !a.startsWith('--'));

  if (queryArg) {
    const matches = searchJetskiFunctions(queryArg, 50, CLI_ROOT);
    console.log(`Matched ${matches.length} functions for query "${queryArg}" (showing up to 50):`);
    for (const fn of matches) {
      console.log(`  ${fn.vmaddr} (${fn.size} B) ${fn.file}:${fn.start_line} -> ${fn.name}`);
    }
    return;
  }

  const info = loadAgyInfo(CLI_ROOT);
  const macho = loadMachOLayout(CLI_ROOT);
  const pkgReport = loadPackagesReport(CLI_ROOT);
  const protoReport = loadProtoServices(CLI_ROOT);
  const manifest = loadEmbeddedManifest(CLI_ROOT);
  const commands = loadCliCommands(CLI_ROOT);
  const layers = summarizeByLayer(pkgReport.packages);

  console.log('=== Antigravity CLI (agy) Offline Binary Analysis ===');
  console.log(`Binary:      ${info.binary_path}`);
  console.log(`Version:     ${info.version} (${info.format}, ${formatBytes(info.size_bytes)})`);
  console.log(`SHA-256:     ${info.sha256}`);
  console.log(`Go Build:    ${info.go_buildinfo.go_version}`);
  console.log(`Segments:    ${macho.segments.map((s) => `${s.segname}(${formatBytes(s.filesize)})`).join(', ')}`);
  console.log(
    `Go Metadata: ${pkgReport.total_go_files} Go files (${pkgReport.jetski_file_count} jetski/antigravity, ${pkgReport.cli_file_count} CLI), ` +
      `${pkgReport.jetski_func_count} jetski funcs across ${pkgReport.package_count} packages`,
  );

  console.log('\n=== Architectural Layers (Compiled __text Code Size) ===');
  for (const layer of layers) {
    console.log(
      `  ${layer.layer.padEnd(20)} | pkgs=${String(layer.package_count).padStart(3)} | ` +
        `funcs=${String(layer.func_count).padStart(5)} | code=${formatBytes(layer.code_bytes).padStart(8)}`,
    );
  }

  console.log('\n=== RPC / Protobuf Services ===');
  for (const svc of protoReport.services) {
    console.log(`  ${svc.full_name} (${svc.method_count} methods)`);
  }

  console.log('\n=== Extracted Resources ===');
  for (const [name, entry] of Object.entries(manifest.embed_fs)) {
    console.log(`  embed.FS/${name}: ${entry.files} files (${formatBytes(entry.total_bytes)})`);
  }
  for (const [name, entry] of Object.entries(manifest.zips)) {
    console.log(`  zip/${name}: ${entry.files} files (${formatBytes(entry.extracted_bytes)})`);
  }
  for (const [name, entry] of Object.entries(manifest.targz)) {
    console.log(`  tar.gz/${name}: ${entry.files} files, ${entry.symlinks} symlinks (${formatBytes(entry.extracted_bytes)})`);
  }
  console.log(`  gzip/fonts: ${manifest.fonts.length} files`);
  console.log(`  gzip/proto-descriptors: ${manifest.proto_descriptors.length} files`);
  console.log(`  CLI subcommands captured: ${Object.keys(commands.subcommands).join(', ')}`);
}

main();
