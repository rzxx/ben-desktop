import { afterAll } from "vitest";

const activeIntervals = new Set<number>();
const setBrowserInterval = window.setInterval.bind(window);
const clearBrowserInterval = window.clearInterval.bind(window);

window.setInterval = ((handler: TimerHandler, timeout?: number) => {
  const interval = setBrowserInterval(handler, timeout);
  activeIntervals.add(interval);
  return interval;
}) as typeof window.setInterval;

window.clearInterval = ((interval?: number) => {
  if (interval !== undefined) {
    activeIntervals.delete(interval);
  }
  clearBrowserInterval(interval);
}) as typeof window.clearInterval;

afterAll(() => {
  for (const interval of activeIntervals) {
    clearBrowserInterval(interval);
  }
  activeIntervals.clear();
});
