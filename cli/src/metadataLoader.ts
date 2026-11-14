import * as fs from 'node:fs';
import * as path from 'node:path';
import type {
  AgyBinaryInfo,
  ArchitecturalLayer,
  CliCommandsSnapshot,
  EmbeddedManifest,
  JetskiFunctionRecord,
  JetskiPackageSummary,
  JetskiPackagesReport,
  LayerStats,
  MachOLayout,
  ProtoServicesReport,
} from './types';

export const CLI_ROOT = path.resolve(__dirname, '..');

function readJson<T>(relativePath: string, cliRoot: string = CLI_ROOT): T {
  const fullPath = path.join(cliRoot, relativePath);
  return JSON.parse(fs.readFileSync(fullPath, 'utf8')) as T;
}

export function loadAgyInfo(cliRoot: string = CLI_ROOT): AgyBinaryInfo {
  return readJson<AgyBinaryInfo>('bin/agy-info.json', cliRoot);
}

export function loadMachOLayout(cliRoot: string = CLI_ROOT): MachOLayout {
  return readJson<MachOLayout>('metadata/macho-segments.json', cliRoot);
}

export function loadPackagesReport(cliRoot: string = CLI_ROOT): JetskiPackagesReport {
  return readJson<JetskiPackagesReport>('metadata/jetski-packages.json', cliRoot);
}

export function loadProtoServices(cliRoot: string = CLI_ROOT): ProtoServicesReport {
  return readJson<ProtoServicesReport>('metadata/proto-services.json', cliRoot);
}

export function loadEmbeddedManifest(cliRoot: string = CLI_ROOT): EmbeddedManifest {
  return readJson<EmbeddedManifest>('metadata/embedded-manifest.json', cliRoot);
}

export function loadCliCommands(cliRoot: string = CLI_ROOT): CliCommandsSnapshot {
  return readJson<CliCommandsSnapshot>('metadata/cli-commands.json', cliRoot);
}

export function loadLineList(relativePath: string, cliRoot: string = CLI_ROOT): string[] {
  const content = fs.readFileSync(path.join(cliRoot, relativePath), 'utf8').trim();
  return content ? content.split('\n') : [];
}

/**
 * Classifies a recovered Go import path into one of the high-level jetski layers.
 */
export function classifyPackageLayer(pkgPath: string): ArchitecturalLayer {
  if (
    pkgPath.includes('_pb/') ||
    pkgPath.endsWith('_pb') ||
    pkgPath.endsWith('_go_proto') ||
    pkgPath.endsWith('_go_grpc') ||
    pkgPath.endsWith('_go_proto_connect') ||
    pkgPath.endsWith('_go_proto_stdiorpc')
  ) {
    return 'protobuf-rpc';
  }
  if (pkgPath.startsWith('google3/third_party/jetski/cli')) {
    return 'cli-frontend';
  }
  if (pkgPath.startsWith('google3/third_party/jetski/language_server')) {
    return 'language-server';
  }
  if (pkgPath.startsWith('google3/third_party/jetski/cortex')) {
    return 'cortex-engine';
  }
  if (pkgPath.includes('devtools/jetski') || pkgPath.includes('telemetry')) {
    return 'telemetry-devtools';
  }
  return 'other';
}

export function summarizeByLayer(packages: JetskiPackageSummary[]): LayerStats[] {
  const byLayer = new Map<ArchitecturalLayer, JetskiPackageSummary[]>();
  for (const pkg of packages) {
    const layer = classifyPackageLayer(pkg.package);
    const bucket = byLayer.get(layer) ?? [];
    bucket.push(pkg);
    byLayer.set(layer, bucket);
  }

  const stats: LayerStats[] = [];
  for (const [layer, items] of byLayer.entries()) {
    const sorted = [...items].sort((a, b) => b.code_bytes - a.code_bytes);
    stats.push({
      layer,
      package_count: items.length,
      func_count: items.reduce((acc, p) => acc + p.func_count, 0),
      code_bytes: items.reduce((acc, p) => acc + p.code_bytes, 0),
      top_packages: sorted.slice(0, 8),
    });
  }
  stats.sort((a, b) => b.code_bytes - a.code_bytes);
  return stats;
}

/**
 * Scans cli/metadata/jetski-functions.jsonl and returns functions matching a substring or regex.
 */
export function searchJetskiFunctions(
  query: string | RegExp,
  limit: number = 100,
  cliRoot: string = CLI_ROOT,
): JetskiFunctionRecord[] {
  const jsonlPath = path.join(cliRoot, 'metadata/jetski-functions.jsonl');
  const lines = fs.readFileSync(jsonlPath, 'utf8').split('\n');
  const matcher = typeof query === 'string'
    ? (s: string) => s.toLowerCase().includes(query.toLowerCase())
    : (s: string) => query.test(s);

  const matches: JetskiFunctionRecord[] = [];
  for (const line of lines) {
    if (!line) continue;
    const rec = JSON.parse(line) as JetskiFunctionRecord;
    if (matcher(rec.name) || matcher(rec.file)) {
      matches.push(rec);
      if (matches.length >= limit) break;
    }
  }
  return matches;
}
