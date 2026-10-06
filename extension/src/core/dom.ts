/** Generic rendered-DOM helpers. Never read application state or hidden payloads. */
export function rendered(element: Element): boolean {
  const view = element.ownerDocument.defaultView;
  if (!view || !element.isConnected || !element.getClientRects().length) return false;
  for (let node: Element | null = element; node; node = node.parentElement) {
    if (['SCRIPT', 'STYLE', 'TEMPLATE', 'NOSCRIPT'].includes(node.tagName) || node.hasAttribute('hidden') || node.getAttribute('aria-hidden') === 'true') return false;
    const style = view.getComputedStyle(node);
    if (style.display === 'none' || style.visibility === 'hidden' || style.visibility === 'collapse' || style.opacity === '0' || style.contentVisibility === 'hidden') return false;
  }
  return true;
}
export function inViewport(element: Element): boolean {
  if (!rendered(element)) return false;
  const view = element.ownerDocument.defaultView!;
  const box = element.getBoundingClientRect();
  let top = 0, left = 0, bottom = view.innerHeight, right = view.innerWidth;
  for (let node = element.parentElement; node; node = node.parentElement) {
    const style = view.getComputedStyle(node);
    const rect = node.getBoundingClientRect();
    if (/(auto|scroll|hidden|clip)/.test(style.overflowY || style.overflow)) { top = Math.max(top, rect.top); bottom = Math.min(bottom, rect.bottom); }
    if (/(auto|scroll|hidden|clip)/.test(style.overflowX || style.overflow)) { left = Math.max(left, rect.left); right = Math.min(right, rect.right); }
  }
  return box.bottom > top && box.top < bottom && box.right > left && box.left < right;
}
export function renderedText(element: Element | null): string {
  if (!element || !rendered(element)) return '';
  const parts: string[] = [];
  const walk = (node: Node): void => {
    if (node.nodeType === 3) parts.push(node.nodeValue ?? '');
    else if (node.nodeType === 1 && rendered(node as Element)) for (const child of node.childNodes) walk(child);
  };
  walk(element);
  return parts.join('').replace(/\s+/gu, ' ').trim();
}
/** Observe before scrolling; wait for DOM quiet, with a hard deadline and abort cleanup. */
export function waitForDOM(root: Element, signal: AbortSignal, delayMs: number, action: () => void): Promise<void> {
  return new Promise((resolve, reject) => {
    const view = root.ownerDocument.defaultView!;
    let quiet: ReturnType<typeof setTimeout>;
    let deadline: ReturnType<typeof setTimeout>;
    const finish = (error?: unknown): void => {
      observer.disconnect(); clearTimeout(quiet); clearTimeout(deadline);
      signal.removeEventListener('abort', abort);
      if (error) reject(error); else resolve();
    };
    const abort = (): void => finish(new DOMException('Capture cancelled', 'AbortError'));
    const observer = new view.MutationObserver(() => { clearTimeout(quiet); quiet = setTimeout(finish, delayMs); });
    if (signal.aborted) { abort(); return; }
    observer.observe(root, { childList: true, subtree: true, characterData: true, attributes: true });
    signal.addEventListener('abort', abort, { once: true });
    quiet = setTimeout(finish, delayMs); deadline = setTimeout(finish, delayMs * 4);
    try { action(); } catch (error) { finish(error); }
  });
}
