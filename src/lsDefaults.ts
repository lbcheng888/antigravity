// Every LS's base flags. No Electron imports: pure modules use this file.

export const LS_API_SERVER_URL = 'https://generativelanguage.googleapis.com';
export const LS_CLOUD_CODE_ENDPOINT = 'https://daily-cloudcode-pa.googleapis.com';

export interface BaseLsArgsOptions {
  ideVersion: string;
  httpsServerPort: number;
  csrfToken: string;
  appDataDir: string;
}

export function baseLsArgs(opts: BaseLsArgsOptions): string[] {
  return [
    '--standalone',
    '--override_ide_name',
    'antigravity',
    '--subclient_type',
    'hub',
    '--override_ide_version',
    opts.ideVersion,
    '--override_user_agent_name',
    'antigravity',
    '--https_server_port',
    String(opts.httpsServerPort),
    '--csrf_token',
    opts.csrfToken,
    '--app_data_dir',
    opts.appDataDir,
    '--api_server_url',
    LS_API_SERVER_URL,
    '--cloud_code_endpoint',
    LS_CLOUD_CODE_ENDPOINT,
  ];
}
