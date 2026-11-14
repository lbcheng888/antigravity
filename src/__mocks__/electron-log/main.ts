import { vi } from 'vitest';

const mockLogger = {
  initialize: vi.fn(),
  functions: {} as Record<string, unknown>,
  info: vi.fn(),
  warn: vi.fn(),
  error: vi.fn(),
  debug: vi.fn(),
  log: vi.fn(),
  transports: {
    file: {
      getFile: vi.fn().mockReturnValue({ path: '/mock/logs/main.log' }),
    },
    console: {} as Record<string, unknown>,
  },
};

export default mockLogger;
