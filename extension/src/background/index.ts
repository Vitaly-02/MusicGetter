import { Dispatcher } from './dispatcher';
import { browserAPI } from '../core/browser';
import { backendOrigin, backendPermission } from '../core/config';
import { Database } from '../storage/database';
import { Client } from '../api/client';
import { Engine } from '../core/engine';
import { SelectionProducer } from '../core/selection-producer';
import { SelectionDatabase } from '../storage/selection-database';
import { CaptureCommands } from './capture-commands';
import { Sessions } from '../core/session';
import { Commands } from './commands';
import { safeError } from '../core/errors';
const browser = browserAPI();
const store = new Database();
const selections = new SelectionDatabase();
const engine = new Engine(store, backendOrigin, s => new Client(backendOrigin, s.token), new SelectionProducer(selections));
const sessions = new Sessions(store, backendOrigin, token => new Client(backendOrigin, token));
const commands = new Commands(browser, store, engine, sessions, backendOrigin, backendPermission);
const captures = new CaptureCommands(browser,store,selections,engine,backendOrigin,backendPermission);
const dispatcher = new Dispatcher(browser,commands,captures);
const wake = () => { void browser.permissions.contains({ origins: [backendPermission] }).then(allowed => allowed ? engine.tick() : undefined).catch(() => undefined); };
// Listeners registered synchronously on every worker start.
browser.runtime.onMessage.addListener((message, sender, respond) => {
  void dispatcher.run(message,sender).then(data => { respond({ok:true,data}); wake(); }, error => respond({ok:false,error:safeError(error)}));
  return true;
});
browser.alarms.onAlarm.addListener(alarm => { if (alarm.name === 'musicgetter-outbox') wake(); });
const initialize = () => { void Promise.resolve(browser.alarms.create('musicgetter-outbox', { periodInMinutes: 1 })).then(wake).catch(() => undefined); };
browser.runtime.onInstalled.addListener(initialize);
browser.runtime.onStartup.addListener(initialize);
initialize(); // alarms may be cleared across browser restarts; recreate safely
