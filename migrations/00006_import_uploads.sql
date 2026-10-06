-- +goose Up
ALTER TABLE musicgetter.imports DROP CONSTRAINT imports_state_check;
ALTER TABLE musicgetter.imports ADD CONSTRAINT imports_state_check CHECK (state IN ('collecting','queued','running','needs_attention','completed','completed_with_errors','cancelled','failed'));
CREATE TABLE musicgetter.import_uploads (
 import_id uuid PRIMARY KEY,
 owner_id uuid NOT NULL,
 create_digest bytea NOT NULL CHECK (octet_length(create_digest)=32),
 capture_state text NOT NULL DEFAULT 'collecting' CHECK (capture_state IN ('collecting','sealed_partial','sealed_complete','aborted')),
 complete_digest bytea CHECK (octet_length(complete_digest)=32),
 received_chunks bigint NOT NULL DEFAULT 0 CHECK (received_chunks BETWEEN 0 AND 10000),
 received_observations bigint NOT NULL DEFAULT 0 CHECK (received_observations BETWEEN 0 AND 100000),
 contiguous_through bigint NOT NULL DEFAULT -1 CHECK (contiguous_through BETWEEN -1 AND 9999),
 last_sequence bigint CHECK (last_sequence BETWEEN -1 AND 9999),
 completion_reason text CHECK (completion_reason IN ('visible_end_confirmed','user_stopped','dom_changed','unknown_end')),
 FOREIGN KEY(owner_id,import_id) REFERENCES musicgetter.imports(owner_id,id),
 UNIQUE(owner_id,import_id)
);
CREATE TABLE musicgetter.import_chunks (
 import_id uuid NOT NULL,
 owner_id uuid NOT NULL,
 sequence bigint NOT NULL CHECK (sequence BETWEEN 0 AND 9999),
 idempotency_key text NOT NULL CHECK (idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$'),
 payload_digest bytea NOT NULL CHECK (octet_length(payload_digest)=32),
 received integer NOT NULL CHECK (received BETWEEN 1 AND 200),
 added integer NOT NULL CHECK (added BETWEEN 0 AND received),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(import_id,sequence),
 UNIQUE(import_id,idempotency_key),
 FOREIGN KEY(owner_id,import_id) REFERENCES musicgetter.import_uploads(owner_id,import_id)
);
-- +goose Down
-- Stop writers first. Receipt history is lost; never resume an old upload after rollback.
DROP TABLE musicgetter.import_chunks;
DROP TABLE musicgetter.import_uploads;
UPDATE musicgetter.imports SET state='cancelled' WHERE state='collecting';
ALTER TABLE musicgetter.imports DROP CONSTRAINT imports_state_check;
ALTER TABLE musicgetter.imports ADD CONSTRAINT imports_state_check CHECK (state IN ('queued','running','needs_attention','completed','completed_with_errors','cancelled','failed'));
