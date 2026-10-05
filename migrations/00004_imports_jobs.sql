-- +goose Up
CREATE TABLE musicgetter.imports (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id uuid NOT NULL,
    request_key text NOT NULL CHECK (length(btrim(request_key)) BETWEEN 1 AND 128),
    source_collection_id uuid NOT NULL,
    profile_id uuid NOT NULL,
    source musicgetter.source NOT NULL,
    destination_collection_id uuid NOT NULL,
    connection_id uuid NOT NULL,
    state text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued','running','needs_attention','completed','completed_with_errors','cancelled','failed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (owner_id,profile_id,source,source_collection_id) REFERENCES musicgetter.source_collections(owner_id,profile_id,source,id),
    FOREIGN KEY (owner_id,connection_id,destination_collection_id) REFERENCES musicgetter.destination_collections(owner_id,connection_id,id),
    UNIQUE (owner_id,request_key),
    UNIQUE (owner_id,id),
    UNIQUE (owner_id,profile_id,source,connection_id,id)
);
CREATE INDEX imports_owner_page ON musicgetter.imports(owner_id,id);
CREATE INDEX imports_source_collection ON musicgetter.imports(source_collection_id);
CREATE INDEX imports_destination_collection ON musicgetter.imports(destination_collection_id);

CREATE TABLE musicgetter.import_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id uuid NOT NULL,
    import_id uuid NOT NULL,
    canonical_track_id uuid NOT NULL,
    profile_id uuid NOT NULL,
    source musicgetter.source NOT NULL,
    connection_id uuid NOT NULL,
    position bigint NOT NULL CHECK (position >= 0),
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','searching','needs_review','matched','ensuring','reconciling','added','already_present','skipped','failed','cancelled')),
    destination_track_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (owner_id,profile_id,source,connection_id,import_id) REFERENCES musicgetter.imports(owner_id,profile_id,source,connection_id,id),
    FOREIGN KEY (owner_id,profile_id,source,canonical_track_id) REFERENCES musicgetter.canonical_tracks(owner_id,profile_id,source,id),
    FOREIGN KEY (owner_id,connection_id,destination_track_id) REFERENCES musicgetter.destination_tracks(owner_id,connection_id,id),
    UNIQUE (import_id,canonical_track_id),
    UNIQUE (owner_id,import_id,id),
    CHECK (state NOT IN ('matched','ensuring','reconciling','added','already_present') OR destination_track_id IS NOT NULL)
);
CREATE INDEX import_items_page ON musicgetter.import_items(owner_id,import_id,id);
CREATE INDEX import_items_state ON musicgetter.import_items(import_id,state,id);
CREATE INDEX import_items_track ON musicgetter.import_items(canonical_track_id);
CREATE INDEX import_items_destination ON musicgetter.import_items(owner_id,connection_id,destination_track_id) WHERE destination_track_id IS NOT NULL;

CREATE TABLE musicgetter.import_jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id uuid NOT NULL,
    import_id uuid NOT NULL,
    item_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN ('match','deliver','reconcile')),
    logical_key text NOT NULL CHECK (length(btrim(logical_key)) BETWEEN 1 AND 128),
    state text NOT NULL DEFAULT 'ready' CHECK (state IN ('ready','leased','completed','failed')),
    available_at timestamptz NOT NULL DEFAULT now(),
    lease_until timestamptz,
    worker_id text CHECK (length(btrim(worker_id)) BETWEEN 1 AND 128),
    generation bigint NOT NULL DEFAULT 0 CHECK (generation >= 0),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts integer NOT NULL DEFAULT 8 CHECK (max_attempts BETWEEN 1 AND 100),
    last_error_code text NOT NULL DEFAULT '' CHECK (last_error_code ~ '^[a-z0-9_]{0,128}$'),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (owner_id,import_id,item_id) REFERENCES musicgetter.import_items(owner_id,import_id,id),
    UNIQUE (import_id,kind,logical_key),
    UNIQUE (item_id,kind),
    CHECK (attempts <= max_attempts),
    CHECK ((state = 'leased' AND worker_id IS NOT NULL AND lease_until IS NOT NULL)
        OR (state <> 'leased' AND worker_id IS NULL AND lease_until IS NULL))
);
CREATE INDEX import_jobs_ready ON musicgetter.import_jobs(available_at,id) WHERE state = 'ready';
CREATE INDEX import_jobs_expired ON musicgetter.import_jobs(lease_until,id) WHERE state = 'leased';
CREATE INDEX import_jobs_owner ON musicgetter.import_jobs(owner_id,import_id,id);

-- +goose Down
-- Destructive for imports and durable work. Stop workers and back up before rollback.
DROP TABLE musicgetter.import_jobs;
DROP TABLE musicgetter.import_items;
DROP TABLE musicgetter.imports;
