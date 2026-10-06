export interface Sender { id?: string; url?: string; tab?: { id?: number; url?: string }; frameId?: number; }
export type Listener = (message: unknown, sender: Sender, respond: (value: unknown) => void) => boolean | void;
export interface BrowserAPI {
  runtime: { id: string; getURL(path: string): string; sendMessage(message: unknown): Promise<unknown>; onMessage: { addListener(listener: Listener): void }; onStartup: { addListener(listener: () => void): void }; onInstalled: { addListener(listener: () => void): void }; };
  tabs: { query(query: { active: boolean; currentWindow: boolean }): Promise<Array<{ id?: number; url?: string }>>; sendMessage(id: number, message: unknown): Promise<unknown>; };
  scripting: { executeScript(options: { target: { tabId: number }; files: string[] }): Promise<unknown>; };
  permissions: { request(options: { origins: string[] }): Promise<boolean>; contains(options: { origins: string[] }): Promise<boolean>; };
  alarms: { create(name: string, options: { periodInMinutes: number }): Promise<void> | void; onAlarm: { addListener(listener: (alarm: { name: string }) => void): void }; };
}
export function browserAPI(): BrowserAPI {
  const host = globalThis as unknown as { browser?: BrowserAPI; chrome?: BrowserAPI };
  const api = host.browser ?? host.chrome; if (!api) throw Error('WebExtension runtime unavailable'); return api;
}
