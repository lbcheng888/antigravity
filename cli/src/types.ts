/**
 * Strict TypeScript schema definitions for offline Antigravity CLI (agy)
 * metadata and extracted resource manifests under cli/metadata/ and cli/bin/.
 */

export interface GoBuildInfo {
  offset: number;
  raw_strings: string[];
  go_version: string;
  build_flags: string;
}

export interface AgyBinaryInfo {
  binary_path: string;
  version: string;
  size_bytes: number;
  sha256: string;
  format: string;
  go_buildinfo: GoBuildInfo;
  linked_libraries: string[];
  node_sea_present: boolean;
  app_asar_present: boolean;
}

export interface MachOSection {
  sectname: string;
  segname: string;
  vmaddr: string;
  size: number;
  fileoff: number;
}

export interface MachOSegment {
  segname: string;
  vmaddr: string;
  vmaddr_int: number;
  vmsize: number;
  fileoff: number;
  filesize: number;
  sections: MachOSection[];
}

export interface MachOLayout {
  magic: string;
  cputype: string;
  filetype: number;
  ncmds: number;
  flags: string;
  text_section_vmaddr: number;
  segments: MachOSegment[];
}

export interface GopclntabHeader {
  offset: number;
  magic: string;
  min_lc: number;
  ptr_size: number;
  nfunc: number;
  nfiles: number;
}

export interface JetskiPackageSummary {
  package: string;
  func_count: number;
  file_count: number;
  code_bytes: number;
  files: string[];
  sample_funcs: string[];
}

export interface JetskiPackagesReport {
  header: GopclntabHeader;
  total_go_files: number;
  jetski_file_count: number;
  cli_file_count: number;
  jetski_func_count: number;
  package_count: number;
  packages: JetskiPackageSummary[];
}

export interface JetskiFunctionRecord {
  index: number;
  vmaddr: string;
  text_offset: number;
  size: number;
  start_line: number;
  file: string;
  package: string;
  name: string;
}

export interface ProtoServiceDescriptor {
  service: string;
  full_name: string;
  go_packages: string[];
  method_count: number;
  methods: string[];
}

export interface ProtoServicesReport {
  service_count: number;
  services: ProtoServiceDescriptor[];
}

export interface EmbedFsManifestEntry {
  header_offset: number;
  entries: number;
  files: number;
  dirs: number;
  total_bytes: number;
  tree_sha256: string;
}

export interface ZipManifestEntry {
  offset_start: number;
  offset_end: number;
  archive_size: number;
  archive_sha256: string;
  files: number;
  dirs: number;
  extracted_bytes: number;
}

export interface TarGzManifestEntry {
  offset_start: number;
  compressed_size: number;
  compressed_sha256: string;
  tar_size: number;
  files: number;
  dirs: number;
  symlinks: number;
  extracted_bytes: number;
}

export interface BlobManifestEntry {
  file: string;
  offset: number;
  size: number;
  sha256: string;
  proto?: string;
}

export interface EmbeddedManifest {
  source_binary: string;
  source_sha256: string;
  source_size: number;
  embed_fs: Record<string, EmbedFsManifestEntry>;
  zips: Record<string, ZipManifestEntry>;
  targz: Record<string, TarGzManifestEntry>;
  fonts: BlobManifestEntry[];
  proto_descriptors: BlobManifestEntry[];
}

export interface CliSubcommandHelp {
  help: string;
  subcommands: Record<string, string>;
}

export interface CliCommandsSnapshot {
  version: string;
  root_help: string;
  subcommands: Record<string, CliSubcommandHelp>;
}

export type ArchitecturalLayer =
  | 'cli-frontend'
  | 'language-server'
  | 'cortex-engine'
  | 'protobuf-rpc'
  | 'telemetry-devtools'
  | 'other';

export interface LayerStats {
  layer: ArchitecturalLayer;
  package_count: number;
  func_count: number;
  code_bytes: number;
  top_packages: JetskiPackageSummary[];
}
