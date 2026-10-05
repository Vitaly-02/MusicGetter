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

// Runtime validates exactly these fields and rejects unknown properties.
// Source/profile/collection/target are fixed on the capture created by the backend.
export interface BatchEnvelope {
  schemaVersion: 1;
  captureId: string;
  sequence: number;
  items: readonly TrackObservation[];
}

// Only extension core's MV3 service worker implements the HTTPS transport.
export interface CaptureTransport {
  append(batch: BatchEnvelope, signal: AbortSignal): Promise<{
    sequence: number;
    replay: boolean;
    acceptedItems: number;
  }>;
  seal(captureId: string, lastSequence: number, summary: CaptureSummary,
       signal: AbortSignal): Promise<{ importId: string }>;
}
