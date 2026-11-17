import { spawnSync } from 'node:child_process';
import * as fs from 'node:fs';
import * as path from 'node:path';
import type { JetskiFunctionRecord } from './types';

export const CLI_ROOT = path.resolve(__dirname, '..');

// ---------------------------------------------------------------------------
// Optional deep-reverse metadata schemas (graceful when files are absent)
// ---------------------------------------------------------------------------

export interface RecoveredProtoMethod {
  name: string;
  input: string;
  output: string;
  client_streaming: boolean;
  server_streaming: boolean;
}

export interface RecoveredProtoService {
  proto: string;
  service: string;
  full_name: string;
  methods: RecoveredProtoMethod[];
}

export interface RecoveredProtoCounts {
  messages: number;
  fields: number;
  enums: number;
  enum_values: number;
  services: number;
  methods: number;
  extensions: number;
  oneofs: number;
  map_fields: number;
}

export interface RecoveredProtoEntry {
  proto_path: string;
  package: string;
  family?: string;
  syntax?: string | null;
  edition?: string | null;
  go_package?: string | null;
  counts: RecoveredProtoCounts;
  output?: string;
  braces_balanced?: boolean;
}

export interface RecoveredProtosReport {
  tool?: string;
  summary?: Record<string, unknown>;
  packages?: Record<string, string[]>;
  services: RecoveredProtoService[];
  protos: RecoveredProtoEntry[];
}

export interface RecoveredGoField {
  name: string;
  type: string;
  offset: number;
  tag?: string;
  embedded?: boolean;
}

export interface RecoveredGoMethod {
  name: string;
  sig?: string;
  ifn?: string;
  tfn?: string;
}

export interface RecoveredGoType {
  va: string;
  name: string;
  ident?: string;
  pkg_path: string;
  kind: string;
  size: number;
  align?: number;
  ptrbytes?: number;
  underlying?: string;
  fields?: RecoveredGoField[];
  methods?: RecoveredGoMethod[];
}

export interface RecoveredGoTypeCompact {
  n: string;
  k: string;
  s: number;
  e?: string;
}

export interface RecoveredGoTypesReport {
  meta?: Record<string, unknown>;
  summary?: Record<string, unknown>;
  kind_counts?: Record<string, number>;
  packages?: Record<string, unknown>;
  type_index?: Record<string, string>;
  types: Record<string, RecoveredGoType>;
  va_to_type?: Record<string, RecoveredGoTypeCompact>;
}

/** [pkg_id, short_name, va, size, scope('j' | 'x')] */
export type CallgraphNodeTuple = [number, string, number, number, string];

/** [caller_node, callee_node, bl_call_sites, tail_branches?] */
export type CallgraphEdgeTuple = [number, number, number] | [number, number, number, number];

export interface CallgraphPackageEdge {
  from: string;
  to: string;
  calls: number;
}

export interface CallgraphSummary {
  schema: number;
  generator?: string;
  stats: {
    binary_functions_total: number;
    scope_functions_jetski: number;
    scanned_code_bytes: number;
    call_sites_bl: number;
    call_sites_resolved: number;
    call_sites_unresolved: number;
    unique_edges: number;
    edges_jetski_to_jetski: number;
    edges_jetski_to_external: number;
    nodes: number;
    packages: number;
    build_seconds?: number;
  };
  top_fan_out: Array<[string, number]>;
  top_fan_in: Array<[string, number]>;
  package_edges: CallgraphPackageEdge[];
  package_summary?: Record<string, { nodes: number; out_edges: number; in_edges: number }>;
  packages: string[];
  nodes: CallgraphNodeTuple[];
  edges: CallgraphEdgeTuple[];
}

export interface DeepMetadataBundle {
  cliRoot: string;
  protos: RecoveredProtosReport | null;
  gotypes: RecoveredGoTypesReport | null;
  callgraph: CallgraphSummary | null;
  functions: JetskiFunctionRecord[];
}

export interface CallgraphNodeMatch {
  nodeId: number;
  fullName: string;
  va: string;
  size: number;
  scope: string;
  callers: Array<{ name: string; calls: number }>;
  callees: Array<{ name: string; calls: number }>;
}

export interface DeepQueryResult {
  query: string;
  protos: RecoveredProtoEntry[];
  services: RecoveredProtoService[];
  gotypes: RecoveredGoType[];
  callgraphNodes: CallgraphNodeMatch[];
  functions: JetskiFunctionRecord[];
}

// ---------------------------------------------------------------------------
// Loaders (missing or unreadable optional files return null / [])
// ---------------------------------------------------------------------------

function readOptionalJson<T>(relativePath: string, cliRoot: string): T | null {
  const fullPath = path.join(cliRoot, relativePath);
  if (!fs.existsSync(fullPath)) {
    return null;
  }
  try {
    return JSON.parse(fs.readFileSync(fullPath, 'utf8')) as T;
  } catch {
    return null;
  }
}

export function loadOptionalProtos(cliRoot: string = CLI_ROOT): RecoveredProtosReport | null {
  return readOptionalJson<RecoveredProtosReport>('metadata/recovered-protos.json', cliRoot);
}

export function loadOptionalGoTypes(cliRoot: string = CLI_ROOT): RecoveredGoTypesReport | null {
  return readOptionalJson<RecoveredGoTypesReport>('metadata/recovered-gotypes.json', cliRoot);
}

export function loadOptionalCallgraph(cliRoot: string = CLI_ROOT): CallgraphSummary | null {
  return readOptionalJson<CallgraphSummary>('metadata/callgraph-summary.json', cliRoot);
}

export function loadJetskiFunctions(cliRoot: string = CLI_ROOT): JetskiFunctionRecord[] {
  const jsonlPath = path.join(cliRoot, 'metadata/jetski-functions.jsonl');
  if (!fs.existsSync(jsonlPath)) {
    return [];
  }
  const lines = fs.readFileSync(jsonlPath, 'utf8').split('\n');
  const records: JetskiFunctionRecord[] = [];
  for (const line of lines) {
    if (!line) continue;
    try {
      records.push(JSON.parse(line) as JetskiFunctionRecord);
    } catch {
      // Skip malformed line if any
    }
  }
  return records;
}

export function loadDeepMetadata(cliRoot: string = CLI_ROOT): DeepMetadataBundle {
  return {
    cliRoot,
    protos: loadOptionalProtos(cliRoot),
    gotypes: loadOptionalGoTypes(cliRoot),
    callgraph: loadOptionalCallgraph(cliRoot),
    functions: loadJetskiFunctions(cliRoot),
  };
}

// ---------------------------------------------------------------------------
// Query engine
// ---------------------------------------------------------------------------

function nodeFullName(cg: CallgraphSummary, node: CallgraphNodeTuple): string {
  const pkg = cg.packages[node[0]] ?? '';
  const short = node[1];
  if (pkg && short && !pkg.startsWith('<')) {
    return `${pkg}.${short}`;
  }
  return `${pkg}${short}`;
}

export function queryDeepMetadata(
  bundle: DeepMetadataBundle,
  query: string,
  limit: number = 15,
): DeepQueryResult {
  const q = query.trim().toLowerCase();
  const matches = (value: string | null | undefined): boolean =>
    typeof value === 'string' && value.toLowerCase().includes(q);

  const protos: RecoveredProtoEntry[] = [];
  const services: RecoveredProtoService[] = [];
  if (bundle.protos) {
    for (const p of bundle.protos.protos ?? []) {
      if (matches(p.proto_path) || matches(p.package) || matches(p.go_package)) {
        protos.push(p);
        if (protos.length >= limit) break;
      }
    }
    for (const svc of bundle.protos.services ?? []) {
      const hitMethod = svc.methods.some((m) => matches(m.name));
      if (matches(svc.full_name) || matches(svc.proto) || matches(svc.service) || hitMethod) {
        services.push(svc);
        if (services.length >= limit) break;
      }
    }
  }

  const gotypes: RecoveredGoType[] = [];
  if (bundle.gotypes && bundle.gotypes.types) {
    for (const t of Object.values(bundle.gotypes.types)) {
      if (matches(t.name) || matches(t.pkg_path) || matches(t.va)) {
        gotypes.push(t);
        if (gotypes.length >= limit) break;
      }
    }
  }

  const callgraphNodes: CallgraphNodeMatch[] = [];
  if (bundle.callgraph) {
    const cg = bundle.callgraph;
    const matchedIds: number[] = [];
    for (let i = 0; i < cg.nodes.length; i++) {
      const full = nodeFullName(cg, cg.nodes[i]);
      const vaHex = `0x${cg.nodes[i][2].toString(16)}`;
      if (matches(full) || vaHex === q) {
        matchedIds.push(i);
        if (matchedIds.length >= limit) break;
      }
    }

    if (matchedIds.length > 0) {
      const matchedSet = new Set<number>(matchedIds);
      const callersMap = new Map<number, Array<{ name: string; calls: number }>>();
      const calleesMap = new Map<number, Array<{ name: string; calls: number }>>();

      for (const edge of cg.edges) {
        const callerId = edge[0];
        const calleeId = edge[1];
        const calls = edge[2] + (edge[3] ?? 0);
        if (matchedSet.has(calleeId)) {
          const arr = callersMap.get(calleeId) ?? [];
          arr.push({ name: nodeFullName(cg, cg.nodes[callerId]), calls });
          callersMap.set(calleeId, arr);
        }
        if (matchedSet.has(callerId)) {
          const arr = calleesMap.get(callerId) ?? [];
          arr.push({ name: nodeFullName(cg, cg.nodes[calleeId]), calls });
          calleesMap.set(callerId, arr);
        }
      }

      for (const id of matchedIds) {
        const nd = cg.nodes[id];
        callgraphNodes.push({
          nodeId: id,
          fullName: nodeFullName(cg, nd),
          va: `0x${nd[2].toString(16)}`,
          size: nd[3],
          scope: nd[4],
          callers: (callersMap.get(id) ?? []).sort((a, b) => b.calls - a.calls),
          callees: (calleesMap.get(id) ?? []).sort((a, b) => b.calls - a.calls),
        });
      }
    }
  }

  const functions: JetskiFunctionRecord[] = [];
  for (const fn of bundle.functions) {
    if (matches(fn.name) || matches(fn.file) || matches(fn.package) || matches(fn.vmaddr)) {
      functions.push(fn);
      if (functions.length >= limit) break;
    }
  }

  return {
    query,
    protos,
    services,
    gotypes,
    callgraphNodes,
    functions,
  };
}

// ---------------------------------------------------------------------------
// Optional integration with scripts/decompile-workbench.py
// ---------------------------------------------------------------------------

export interface DecompileOptions {
  limit?: number;
  pseudoOnly?: boolean;
  cliRoot?: string;
}

export function runDecompileWorkbench(funcQuery: string, opts: DecompileOptions = {}): string {
  const root = opts.cliRoot ?? CLI_ROOT;
  const scriptPath = path.join(root, 'scripts/decompile-workbench.py');
  if (!fs.existsSync(scriptPath)) {
    throw new Error(`decompile-workbench.py not found at ${scriptPath}`);
  }
  const args = [scriptPath, '--dump-func', funcQuery];
  if (opts.limit !== undefined) {
    args.push('--limit', String(opts.limit));
  }
  if (opts.pseudoOnly) {
    args.push('--pseudo-only');
  }
  const res = spawnSync('python3', args, { encoding: 'utf8', maxBuffer: 32 * 1024 * 1024 });
  if (res.error) {
    throw res.error;
  }
  if (res.status !== 0) {
    throw new Error(res.stderr?.trim() || `decompile-workbench.py exited with status ${res.status}`);
  }
  return res.stdout;
}

// ---------------------------------------------------------------------------
// CLI formatting
// ---------------------------------------------------------------------------

function printSummary(bundle: DeepMetadataBundle): void {
  console.log('=== Antigravity CLI (agy) Unified Deep Reverse Summary ===');

  if (bundle.protos) {
    const pCount = bundle.protos.protos?.length ?? 0;
    const sCount = bundle.protos.services?.length ?? 0;
    console.log(`Protos:      ${pCount} recovered .proto files, ${sCount} RPC services`);
  } else {
    console.log('Protos:      (metadata/recovered-protos.json not present)');
  }

  if (bundle.gotypes) {
    const namedCount = Object.keys(bundle.gotypes.types ?? {}).length;
    const vaCount = Object.keys(bundle.gotypes.va_to_type ?? {}).length;
    const pkgCount = Object.keys(bundle.gotypes.packages ?? {}).length;
    console.log(`Go Types:    ${namedCount} target types across ${pkgCount} packages (${vaCount} total va_to_type entries)`);
  } else {
    console.log('Go Types:    (metadata/recovered-gotypes.json not present)');
  }

  if (bundle.callgraph) {
    const st = bundle.callgraph.stats;
    console.log(
      `Callgraph:   ${st.nodes} nodes, ${st.unique_edges} edges (${st.call_sites_resolved}/${st.call_sites_bl} bl sites resolved, ` +
        `${st.scope_functions_jetski} jetski funcs across ${st.packages} packages)`,
    );
    console.log('\nTop Cross-Package Edges:');
    for (const edge of bundle.callgraph.package_edges.slice(0, 10)) {
      console.log(`  ${String(edge.calls).padStart(5)}  ${edge.from} -> ${edge.to}`);
    }
  } else {
    console.log('Callgraph:   (metadata/callgraph-summary.json not present)');
  }

  console.log(`\nFunctions:   ${bundle.functions.length} jetski function symbols loaded`);
}

function printQueryResult(res: DeepQueryResult): void {
  console.log(`=== Deep Query: "${res.query}" ===`);

  console.log(`\n[Recovered Protos] (${res.protos.length} shown)`);
  for (const p of res.protos) {
    console.log(
      `  ${p.proto_path} (pkg=${p.package || '<none>'}, msgs=${p.counts.messages}, enums=${p.counts.enums}, svcs=${p.counts.services})`,
    );
  }

  console.log(`\n[Recovered Proto Services] (${res.services.length} shown)`);
  for (const svc of res.services) {
    console.log(`  ${svc.full_name} (${svc.methods.length} RPCs) [${svc.proto}]`);
  }

  console.log(`\n[Recovered Go Types] (${res.gotypes.length} shown)`);
  for (const t of res.gotypes) {
    const fields = t.fields ? ` fields=${t.fields.length}` : '';
    const methods = t.methods ? ` methods=${t.methods.length}` : '';
    console.log(`  ${t.va}  ${t.kind} ${t.name} (size=${t.size}B${fields}${methods}) [${t.pkg_path}]`);
  }

  console.log(`\n[Callgraph Nodes] (${res.callgraphNodes.length} shown)`);
  for (const n of res.callgraphNodes) {
    console.log(`  ${n.va} (${n.size} B) ${n.fullName}`);
    if (n.callers.length > 0) {
      const topCallers = n.callers.slice(0, 5).map((c) => `${c.name}(x${c.calls})`).join(', ');
      console.log(`    callers(${n.callers.length}): ${topCallers}`);
    }
    if (n.callees.length > 0) {
      const topCallees = n.callees.slice(0, 5).map((c) => `${c.name}(x${c.calls})`).join(', ');
      console.log(`    callees(${n.callees.length}): ${topCallees}`);
    }
  }

  console.log(`\n[Jetski Functions] (${res.functions.length} shown)`);
  for (const fn of res.functions) {
    console.log(`  ${fn.vmaddr} (${fn.size} B) ${fn.file}:${fn.start_line} -> ${fn.name}`);
  }
}

function parseArgs(argv: string[]): {
  query?: string;
  decompile?: string;
  limit: number;
  pseudoOnly: boolean;
  json: boolean;
} {
  let decompile: string | undefined;
  let limit = 15;
  let pseudoOnly = false;
  let json = false;
  const positional: string[] = [];

  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === '--decompile' || arg === '--func') {
      decompile = argv[++i];
    } else if (arg === '--limit') {
      const parsed = Number.parseInt(argv[++i] ?? '15', 10);
      if (!Number.isNaN(parsed) && parsed > 0) {
        limit = parsed;
      }
    } else if (arg === '--pseudo-only') {
      pseudoOnly = true;
    } else if (arg === '--json') {
      json = true;
    } else if (!arg.startsWith('--')) {
      positional.push(arg);
    }
  }

  return {
    query: positional[0],
    decompile,
    limit,
    pseudoOnly,
    json,
  };
}

function main(): void {
  const opts = parseArgs(process.argv.slice(2));

  if (opts.decompile) {
    const out = runDecompileWorkbench(opts.decompile, {
      limit: opts.limit,
      pseudoOnly: opts.pseudoOnly,
      cliRoot: CLI_ROOT,
    });
    process.stdout.write(out);
    return;
  }

  const bundle = loadDeepMetadata(CLI_ROOT);
  if (opts.query) {
    const res = queryDeepMetadata(bundle, opts.query, opts.limit);
    if (opts.json) {
      console.log(JSON.stringify(res, null, 2));
    } else {
      printQueryResult(res);
    }
    return;
  }

  if (opts.json) {
    console.log(
      JSON.stringify(
        {
          protos_count: bundle.protos?.protos?.length ?? 0,
          services_count: bundle.protos?.services?.length ?? 0,
          gotypes_count: bundle.gotypes ? Object.keys(bundle.gotypes.types ?? {}).length : 0,
          callgraph_stats: bundle.callgraph?.stats ?? null,
          functions_count: bundle.functions.length,
        },
        null,
        2,
      ),
    );
  } else {
    printSummary(bundle);
  }
}

if (require.main === module) {
  main();
}
