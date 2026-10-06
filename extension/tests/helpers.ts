import type { Job, Session, Store } from '../src/core/job';
import type { API } from '../src/api/client';
import type { ChunkReceipt, ImportChunk, CreateImportRequest, CompleteImportRequest } from '../src/core/contracts';
export const origin = 'http://127.0.0.1:8080';
export const importID = '00000000-0000-0000-0000-000000000001';
export const session: Session = { token: 'mge_'+ 'a'.repeat(43), owner: '00000000-0000-0000-0000-000000000002', origin, telegramUser: 42, expiresAt: '2099-01-01T00:00:00Z' };
export function job(): Job { return { id: crypto.randomUUID(), owner: session.owner, origin, create: { client_request_id:crypto.randomUUID(),source:{service:'spotify',profile_key:'demo',collection_key:'demo',kind:'selection',title:'Demo'},destination_collection_id:importID }, stage:'creating',cancelRequested:false,nextSequence:0,accepted:0,added:0,demoTotal:450,attempts:0,retryAt:0,blocked:false }; }
export class MemoryStore implements Store {
  s: Session | undefined = structuredClone(session); j: Job | undefined = job();
  async session() { return structuredClone(this.s); }
  async setSession(s: Session) { this.s=structuredClone(s); }
  async job() { return structuredClone(this.j); }
  async setJob(j: Job) { this.j=structuredClone(j); }
  async updateJob(id: string, fn:(j:Job)=>Job) { if (this.j?.id===id) this.j=structuredClone(fn(structuredClone(this.j))); }
  async clearSession(token?:string) { if (!token || this.s?.token===token) this.s=undefined; }
  async clearAll() { this.s=undefined;this.j=undefined; }
}
export class FakeAPI implements API {
  requests: ImportChunk[]=[]; creates: CreateImportRequest[]=[];completes:CompleteImportRequest[]=[]; cancels=0;
  async create(request:CreateImportRequest) { this.creates.push(structuredClone(request));return {id:importID}; }
  async append(_id:string,chunk:ImportChunk):Promise<ChunkReceipt> { this.requests.push(structuredClone(chunk));return {sequence:chunk.sequence,idempotency_key:chunk.idempotency_key,received:chunk.tracks.length,added:chunk.tracks.length,replay:false}; }
  async complete(_id:string,body:CompleteImportRequest) { this.completes.push(body);return {id:importID,state:'queued',total_tracks:450,added:0,failed:0}; }
  async cancel() { this.cancels++;return {id:importID,state:'cancelled',total_tracks:0,added:0,failed:0}; }
}
