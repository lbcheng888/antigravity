declare module 'vitest' {
  export interface MockFn<TArgs extends unknown[] = unknown[], TReturn = unknown> {
    (...args: TArgs): TReturn;
    mockImplementation(fn: (...args: TArgs) => TReturn): this;
    mockImplementationOnce(fn: (...args: TArgs) => TReturn): this;
    mockReturnValue(value: TReturn): this;
    mockReturnValueOnce(value: TReturn): this;
    mockResolvedValue(value: Awaited<TReturn>): this;
    mockResolvedValueOnce(value: Awaited<TReturn>): this;
    mockRejectedValue(value: unknown): this;
    mockReset(): this;
    mockClear(): this;
    mockRestore(): void;
    mock: {
      calls: TArgs[];
      results: Array<{ type: 'return' | 'throw'; value: unknown }>;
    };
  }

  export const vi: {
    fn<TArgs extends any[] = any[], TReturn = any>(
      impl?: (...args: TArgs) => TReturn,
    ): MockFn<TArgs, TReturn>;
    spyOn<T extends object, K extends keyof T>(
      object: T,
      method: K,
    ): MockFn<any[], any>;
    mock(path: string, factory?: () => unknown): void;
    restoreAllMocks(): void;
    clearAllMocks(): void;
    resetAllMocks(): void;
    useFakeTimers(): void;
    useRealTimers(): void;
  };
}
