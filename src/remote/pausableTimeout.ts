/** A timeout whose clock stops while paused. */
export class PausableTimeout {
  private timer?: NodeJS.Timeout | number;
  private since = 0;
  private done = false;

  constructor(
    private readonly fire: () => void,
    private left: number,
  ) {
    this.resume();
  }

  pause(): void {
    if (this.timer) {
      clearTimeout(this.timer);
      this.timer = undefined;
      // The clock setTimeout uses: wall-clock steps don't count.
      this.left -= performance.now() - this.since;
    }
  }

  resume(): void {
    if (!this.timer && !this.done) {
      this.since = performance.now();
      this.timer = setTimeout(() => {
        this.clear();
        this.fire();
      }, this.left);
    }
  }

  clear(): void {
    this.done = true;
    clearTimeout(this.timer);
    this.timer = undefined;
  }
}
