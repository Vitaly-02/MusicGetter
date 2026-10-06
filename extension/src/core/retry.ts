import { AppError } from './errors';
export function retryDelay(error: AppError, attempt: number, random: () => number = Math.random): number {
  const exponential = Math.min(60_000, 1000 * 2 ** Math.min(attempt, 6));
  return Math.max(error.retryAfterMs, Math.round(exponential * (0.75 + random() * 0.5)));
}
