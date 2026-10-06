import type { ImportChunk, CreateImportRequest } from './contracts';
import type { ErrorCode } from './errors';
export interface Session { token: string; origin: string; owner: string; telegramUser: number; expiresAt: string; }
export interface Job {
  id: string; owner: string; origin: string;
  create: CreateImportRequest;
  importId?: string;
  stage: 'creating' | 'collecting' | 'uploading' | 'completing' | 'done' | 'cancelled';
  cancelRequested: boolean;
  pending?: ImportChunk;
  nextSequence: number; accepted: number; added: number;
  demoTotal: number;
  attempts: number; retryAt: number; blocked: boolean; error?: ErrorCode;
  serverState?: string;
}
export interface Store {
  session(): Promise<Session | undefined>;
  setSession(session: Session): Promise<void>;
  job(): Promise<Job | undefined>;
  setJob(job: Job): Promise<void>;
  updateJob(id: string, update: (job: Job) => Job): Promise<void>;
  clearSession(expectedToken?: string): Promise<void>;
  clearAll(): Promise<void>;
}
export function finished(job: Job): boolean { return (job.stage === 'done' && !job.cancelRequested) || job.stage === 'cancelled'; }
