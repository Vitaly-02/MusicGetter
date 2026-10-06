import { browserAPI } from '../core/browser';
import { backendOrigin, backendPermission } from '../core/config';
import type { ErrorCode } from '../core/errors';
import type { Destination } from '../api/client';
const browser = browserAPI();
const element = <T extends HTMLElement>(id: string): T => document.getElementById(id) as T;
const button = (id: string) => element<HTMLButtonElement>(id);
const input = (id: string) => element<HTMLInputElement>(id);
const labels: Record<ErrorCode,string> = {
  network:'Временная ошибка сети. Очередь сохранена; повторим позже.', unauthorized:'Подключение истекло или отозвано. Получите новый /connect в боте.',
  conflict:'Состояние импорта изменилось. Обновите статус; неизменный пакет нельзя заменить другим.', invalid_data:'Проверьте код и выбранные значения.', unavailable:'Сервис временно недоступен.',
  pairing_uncertain:'Код мог быть использован, но подключение не завершилось. Получите новый /connect и повторите.', permission:'Нужен доступ к вашему backend или текущей вкладке.', unsupported:'Сбор этого источника ещё не реализован.', account_mismatch:'Задача принадлежит другому аккаунту. Подключите прежний аккаунт или выйдите, чтобы очистить локальную задачу.', storage:'Не удалось сохранить очередь. Проверьте доступное место в профиле браузера.', retry_exhausted:'Попытки исчерпаны. Нажмите «Повторить».'
};
interface State { connected: boolean; telegramUser: number | null; expiresAt: string | null; job: null | { stage: string; accepted: number; added: number; total: number; retryAt: number; blocked: boolean; error?: ErrorCode; cancelRequested: boolean; serverState?: string; importId?: string }; }
let state: State | undefined, cursor = '', busy = false, loadedDestinations = false;
async function send<T>(message: unknown): Promise<T> {
  const reply = await browser.runtime.sendMessage(message) as { ok: boolean; data?: T; error?: ErrorCode };
  if (!reply.ok) throw reply.error ?? 'unavailable'; return reply.data as T;
}
function notice(error: unknown): void { element('notice').textContent = labels[error as ErrorCode] ?? labels.unavailable; }
function controls(): void {
  const active = state?.job && !['done','cancelled'].includes(state.job.stage);
  button('start').disabled = busy || !state?.connected || !!active || !input('demo').checked || !element<HTMLSelectElement>('destination').value;
  button('cancel').disabled = busy || !state?.connected || !state.job || state.job.stage === 'cancelled';
  button('retry').disabled = busy || !state?.connected || !state.job || (!state.job.blocked && !state.job.retryAt);
  button('refresh').disabled = busy || !state?.connected || !state.job?.importId;
  button('reconnect').disabled = busy || !state?.connected; button('logout').disabled = busy;
}
async function refreshState(): Promise<void> {
  state = await send<State>({ type:'state' });
  element('connection').textContent = state.connected ? `Подключено · Telegram ${state.telegramUser}` : 'Расширение не подключено';
  const job = state.job;
  element<HTMLProgressElement>('progress').value = job?.accepted ?? 0;
  element('progress-text').textContent = job ? `${job.accepted} / ${job.total} тестовых треков принято` : 'Импорт ещё не запущен';
  const stages: Record<string,string> = { creating:'Создаём импорт', collecting:'Готовим пакет', uploading:'Отправляем пакет', completing:'Завершаем загрузку', done:'Загрузка завершена', cancelled:'Импорт отменён' };
  element('details').textContent = job ? (job.cancelRequested ? 'Ожидает отмены на сервере. ' : '') + (job.error ? (labels[job.error] ?? labels.unavailable) + (job.blocked ? ' Нажмите «Повторить» после исправления.' : ` Следующая попытка: ${new Date(job.retryAt).toLocaleTimeString()}.`) : `${stages[job.stage] ?? job.stage}. Уникальных добавлено в импорт: ${job.added}. ${job.serverState ? 'Состояние backend: '+job.serverState : ''}`) : '';
  controls();
  if (state.connected && !loadedDestinations) { loadedDestinations = true; await destinations(''); }
  if (!state.connected) { loadedDestinations = false; element<HTMLSelectElement>('destination').replaceChildren(new Option('Сначала подключитесь','')); }
}
async function destinations(next: string): Promise<void> {
  const result = await send<{items:Destination[];next_cursor?:string}>({type:'destinations', ...(next ? {cursor:next} : {})});
  const select = element<HTMLSelectElement>('destination');
  select.replaceChildren(new Option(result.items.length ? 'Выберите коллекцию' : 'Коллекций пока нет',''));
  for (const item of result.items) select.add(new Option(`${item.title.slice(0,80)} · ${item.kind}`,item.id));
  cursor = result.next_cursor ?? ''; button('next').hidden = !cursor; controls();
}
async function action(task: () => Promise<unknown>): Promise<void> {
  if (busy) return; busy = true; controls(); element('notice').textContent = '';
  try { await task(); await refreshState(); } catch(error) { notice(error); } finally { busy = false; controls(); }
}
element('backend').textContent = backendOrigin;
element('pair-form').addEventListener('submit', event => {
  event.preventDefault();
  // Permission request starts directly in the user's gesture, before messaging.
  const permission = browser.permissions.request({origins:[backendPermission]});
  void action(async () => { if (!await permission) throw 'permission'; const code = input('code').value; input('code').value = ''; await send({type:'pair',code}); loadedDestinations = false; });
});
button('reconnect').onclick = () => { void action(() => send({type:'reconnect'})); };
button('logout').onclick = () => { void action(() => send({type:'logout'})); };
button('destinations').onclick = () => { void action(() => destinations('')); };
button('next').onclick = () => { void action(() => destinations(cursor)); };
button('start').onclick = () => { void action(() => send({type:'startDemo',destinationId:element<HTMLSelectElement>('destination').value,profileKey:input('profile').value})); };
button('cancel').onclick = () => { void action(() => send({type:'cancel'})); };
button('retry').onclick = () => { void action(() => send({type:'resume'})); };
button('refresh').onclick = () => { void action(() => send({type:'refresh'})); };
input('demo').onchange = controls; element('destination').onchange = controls;
void send<{page:null|{source:string}}>({type:'page'}).then(result => { element('source').textContent = result.page ? `${result.page.source} · адаптер-заглушка` : 'Откройте Spotify, Яндекс Музыку или VK Музыку'; }).catch(notice);
void refreshState().catch(notice);
const timer = setInterval(() => { if (!busy) void refreshState().catch(notice); },2000);
window.addEventListener('pagehide',()=>clearInterval(timer));
