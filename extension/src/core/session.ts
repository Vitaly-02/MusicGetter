import type { Store } from './job';
import { Client } from '../api/client';
import { AppError } from './errors';
export class Sessions {
  constructor(private readonly store: Store, private readonly origin: string, private readonly client: (token?: string) => Client) {}
  async pair(value: string): Promise<void> {
    const code = value.trim().toUpperCase();
    if (!/^[A-Z2-7]{26}$/.test(code)) throw new AppError('invalid_data');
    // A one-time claim is NEVER automatically retried after an uncertain result.
    try {
      const result = await this.client().claim(code);
      const me = await this.client(result.token).me();
      const job = await this.store.job();
      if (job && job.owner !== me.id && job.stage !== 'done' && job.stage !== 'cancelled') throw new AppError('account_mismatch');
      await this.store.setSession({ token: result.token, origin: this.origin, owner: me.id, telegramUser: me.telegram_user_id, expiresAt: result.expires_at });
    } catch (error) { if (error instanceof AppError && error.code === 'account_mismatch') throw error; throw new AppError('pairing_uncertain'); }
  }
  async reconnect(): Promise<void> {
    const session = await this.store.session();
    if (!session || session.origin !== this.origin || Date.parse(session.expiresAt) <= Date.now()) { await this.store.clearSession(); throw new AppError('unauthorized'); }
    try { const me = await this.client(session.token).me(); if (me.id !== session.owner) throw new AppError('account_mismatch'); }
    catch (error) { if (error instanceof AppError && error.code === 'unauthorized') await this.store.clearSession(session.token); throw error; }
  }
  logout(): Promise<void> { return this.store.clearAll(); }
}
