import type { SelectionRow } from '../core/capture';
export class SelectionView {
  readonly host: HTMLElement;
  private readonly rows: HTMLElement;
  private readonly status: HTMLElement;
  private readonly count: HTMLElement;
  private readonly buttons: HTMLButtonElement[] = [];
  private readonly inputs = new Map<string, HTMLInputElement>();
  constructor(private readonly doc: Document, actions: { all():void; clear():void; start():void; cancel():void }) {
    this.host = doc.createElement('div');
    this.host.style.cssText = 'position:fixed;inset:0;pointer-events:none;z-index:2147483647';
    const shadow = this.host.attachShadow({mode:'closed'});
    const style = doc.createElement('style');
    style.textContent = ':host{all:initial} *{box-sizing:border-box} .panel{position:fixed;right:16px;top:16px;max-width:370px;background:#fff;color:#172337;border:1px solid #789;border-radius:10px;padding:12px;pointer-events:auto;font:14px system-ui;box-shadow:0 3px 18px #0004} button{font:inherit;margin:4px;padding:6px;cursor:pointer} p{margin:6px 0} input{position:fixed;pointer-events:auto;width:20px;height:20px;accent-color:#1769d2;box-shadow:0 0 0 2px white;cursor:pointer}';
    const panel = doc.createElement('section'); panel.className = 'panel'; panel.setAttribute('aria-label','MusicGetter — выбор треков');
    const title = doc.createElement('strong'); title.textContent = 'MusicGetter';
    this.count = doc.createElement('p'); this.count.setAttribute('aria-live','polite');
    this.status = doc.createElement('p'); this.status.setAttribute('role','status');
    panel.append(title,this.count,this.status);
    for (const [label, action] of [['Выбрать все с прокруткой',actions.all],['Очистить выбор',actions.clear],['Начать импорт',actions.start],['Отмена',actions.cancel]] as const) {
      const button = doc.createElement('button'); button.type = 'button'; button.textContent = label;
      button.onclick = event => { event.stopPropagation(); action(); }; panel.append(button); this.buttons.push(button);
    }
    this.rows = doc.createElement('div'); shadow.append(style,this.rows,panel); doc.documentElement.append(this.host);
    this.update(0,false,'Отметьте треки. Выбор сохраняется при прокрутке.');
  }
  update(count: number, locked: boolean, message: string): void {
    this.count.textContent = `Выбрано: ${count}`; this.status.textContent = message;
    this.buttons.forEach((button,i) => { button.disabled = i !== 3 && (locked || (i === 2 && count === 0)); });
    for (const input of this.inputs.values()) input.disabled = locked;
  }
  render(rows: readonly SelectionRow[], keys: readonly string[], flags: readonly boolean[], locked: boolean, toggle: (row: SelectionRow,key:string,selected:boolean) => void): void {
    const active = new Set<string>();
    rows.forEach((row,i) => {
      const key = keys[i]!; if (active.has(key)) return; active.add(key);
      let input = this.inputs.get(key);
      if (!input) { input = this.doc.createElement('input'); input.type = 'checkbox'; this.inputs.set(key,input); this.rows.append(input); }
      const box = row.element.getBoundingClientRect();
      input.style.left = `${Math.max(2, Math.min(this.doc.defaultView!.innerWidth - 24, box.right - 24))}px`;
      input.style.top = `${Math.max(2,box.top + (box.height - 20) / 2)}px`;
      input.checked = flags[i] ?? false; input.disabled = locked;
      input.setAttribute('aria-label',`Выбрать ${row.track.title}`);
      input.onclick = event => event.stopPropagation();
      input.onchange = () => toggle(row,key,input!.checked);
    });
    for (const [key,input] of this.inputs) if (!active.has(key)) { input.remove(); this.inputs.delete(key); }
  }
  dispose(): void { this.inputs.clear(); this.host.remove(); }
}
