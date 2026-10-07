import { browserAPI } from '../core/browser';
import { adapterFor } from '../sources';
import { record, isID } from '../core/validation';
import { SelectionController } from './selection-controller';
const host = globalThis as unknown as { __musicGetterContent?: boolean };
if (!host.__musicGetterContent) {
  host.__musicGetterContent = true;
  const browser = browserAPI(), documentNonce = crypto.randomUUID();
  let selection: SelectionController | undefined; let captureID = '';
  browser.runtime.onMessage.addListener((message,sender,respond) => {
    // Only extension background messages; never page/window messages or other frames.
    if (sender.id !== browser.runtime.id || sender.tab) return;
    try {
      const request = record(message,['type','id','document','mode']);
      const adapter = adapterFor(new URL(location.href));
      if (request.type === 'page.info') { record(request,['type']); respond({page:adapter?.detectPage() ?? null}); }
      else if (request.type === 'capture.describe') { record(request,['type']); respond({source:adapter?.detectPage()?.source,collection:adapter?.getCollectionMetadata() ?? null,document:documentNonce}); }
      else if (request.type === 'capture.init') {
        if (request.document !== documentNonce || !isID(request.id) || !['all','selected'].includes(String(request.mode)) || !adapter?.detectPage()?.supported) throw Error('Unsupported capture');
        selection?.dispose(); captureID = request.id;
        selection = new SelectionController(document,browser,adapter,captureID,documentNonce,request.mode as 'all'|'selected'); respond({ok:true});
      } else if (request.type === 'capture.close') { if (request.id === captureID) { selection?.dispose(); selection = undefined; } respond({ok:true}); }
    } catch { respond({ok:false}); }
  });
  window.addEventListener('pagehide',() => { selection?.dispose(); });
}
