/** Rendered DOM only. These types are not runtime input validation. */
export type Source = "spotify" | "yandex" | "vk";
export type CollectionKind = "favorites" | "playlist" | "album" | "selection";

export interface SourceKey {
  key: string;
  provisional: boolean;
}

export interface TrackObservation {
  ref: SourceKey;
  title: string;
  artists: readonly string[];
  album?: string;
  durationMs?: number;
  version?: string;
  position: number;
}

export interface CollectionObservation {
  ref: SourceKey;
  kind: CollectionKind;
  title: string;
}

export interface CaptureChunk {
  items: readonly TrackObservation[];
  warnings: readonly ("missing_metadata" | "unstable_identity" | "dom_changed")[];
}

export interface CaptureSummary {
  completeness: "partial" | "complete";
  reason: "visible_end_confirmed" | "user_stopped" | "dom_changed" | "unknown_end";
  observedCount: number;
  renderedExpectedCount?: number;
}

// Each adapter sees only its own host's document. No network clients or credentials.
// Capture buffers at most one bounded chunk; core persists it before requesting next.
export interface SourceAdapter {
  readonly source: Source;
  supports(location: URL): boolean;
  inspect(document: Document): CollectionObservation | null;
  capture(
    document: Document,
    options: { readonly signal: AbortSignal; readonly maxItems: number },
  ): AsyncGenerator<CaptureChunk, CaptureSummary, void>;
}

// HTTP v1 wire DTOs. OpenAPI source of truth: docs/openapi.json.
// SourceAdapter observations are mapped to these fields by future extension core.
export interface CreateImportRequest {
  client_request_id: string;
  source: {
    service: Source;
    profile_key: string;
    collection_key: string;
    kind: CollectionKind;
    title: string;
    provisional?: boolean;
  };
  destination_collection_id: string;
}

export interface ImportTrack {
  title: string;
  artists: readonly string[];
  album?: string;
  duration_ms?: number | null;
  version?: string;
  source_track_key?: string | null;
  source_url?: string | null;
  position: number;
}

export interface ImportChunk {
  schema_version: 1;
  idempotency_key: string;
  sequence: number;
  tracks: readonly ImportTrack[]; // 1..200; JSON body <=512 KiB
}

export interface ChunkReceipt {
  sequence: number;
  idempotency_key: string;
  received: number;
  added: number;
  replay: boolean;
}

export interface CompleteImportRequest {
  last_sequence: number; // -1 for an empty capture
  observed_count: number; // sum of observations, before track identity dedup
  completeness: "partial" | "complete";
  reason: CaptureSummary["reason"];
}

// Transport implementation and trusted-context token storage are a future step.
export interface ImportTransport {
  create(request: CreateImportRequest, signal: AbortSignal): Promise<{
    id: string; replay: boolean;
  }>;
  append(importId: string, chunk: ImportChunk, signal: AbortSignal): Promise<ChunkReceipt>;
  complete(importId: string, request: CompleteImportRequest, signal: AbortSignal): Promise<void>;
  cancel(importId: string, signal: AbortSignal): Promise<void>;
}
