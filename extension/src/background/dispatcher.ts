import type { BrowserAPI, Sender } from '../core/browser';
import { Commands, trustedPopup } from './commands';
import type { CaptureCommands } from './capture-commands';
/** One mutation lane, including logout/pairing and content selection requests. */
export class Dispatcher {
  private serial: Promise<unknown> = Promise.resolve();
  constructor(private readonly browser: BrowserAPI, private readonly commands: Commands, private readonly captures: CaptureCommands) {}
  run(message: unknown, sender: Sender): Promise<unknown> {
    const operation = this.serial.then(async () => {
      const type = (message as {type?:unknown} | null)?.type;
      if (!trustedPopup(sender,this.browser)) return this.captures.content(message,sender);
      if (typeof type === 'string' && type.startsWith('capture.')) return this.captures.popup(message);
      if (type === 'logout') {
        let cleanupError: unknown;
        try { await this.captures.clear(); } catch (error) { cleanupError = error; }
        const result = await this.commands.run(message); // credential removal must not depend on selection DB health
        if (cleanupError) throw cleanupError;
        return result;
      }
      if (type === 'cancel') await this.captures.close();
      const result = await this.commands.run(message);
      if (type === 'startDemo') await this.captures.clear();
      return result;
    });
    this.serial = operation.catch(() => undefined);
    return operation;
  }
}
