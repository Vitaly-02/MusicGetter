-- +goose Up
-- New nullable effective target preserves the immutable requested collection and
-- upload receipts. No data backfill. ALTER TABLE briefly takes ACCESS EXCLUSIVE.
ALTER TABLE musicgetter.imports ADD COLUMN resolved_destination_collection_id uuid;
ALTER TABLE musicgetter.imports ADD CONSTRAINT imports_resolved_target_fk FOREIGN KEY(owner_id,connection_id,resolved_destination_collection_id) REFERENCES musicgetter.destination_collections(owner_id,connection_id,id);
CREATE INDEX imports_resolved_target ON musicgetter.imports(resolved_destination_collection_id) WHERE resolved_destination_collection_id IS NOT NULL;
CREATE INDEX import_items_position ON musicgetter.import_items(import_id,position,id);
CREATE TABLE musicgetter.destination_target_bindings (
 requested_collection_id uuid PRIMARY KEY,
 owner_id uuid NOT NULL,
 connection_id uuid NOT NULL,
 operation_key uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 title text NOT NULL CHECK(length(btrim(title)) BETWEEN 1 AND 1024),
 resolved_collection_id uuid,
 created_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(owner_id,connection_id,requested_collection_id) REFERENCES musicgetter.destination_collections(owner_id,connection_id,id),
 FOREIGN KEY(owner_id,connection_id,resolved_collection_id) REFERENCES musicgetter.destination_collections(owner_id,connection_id,id)
);
CREATE INDEX destination_bindings_resolved ON musicgetter.destination_target_bindings(resolved_collection_id) WHERE resolved_collection_id IS NOT NULL;
-- +goose Down
-- Fail closed: dropping live creation keys could duplicate remote playlists after
-- another upgrade. Rollback requires an empty ledger (otherwise forward repair).
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM musicgetter.destination_target_bindings) OR EXISTS(SELECT 1 FROM musicgetter.imports WHERE resolved_destination_collection_id IS NOT NULL) THEN
  RAISE EXCEPTION 'destination target bindings exist; forward repair required';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE musicgetter.destination_target_bindings;
DROP INDEX musicgetter.import_items_position;
DROP INDEX musicgetter.imports_resolved_target;
ALTER TABLE musicgetter.imports DROP CONSTRAINT imports_resolved_target_fk;
ALTER TABLE musicgetter.imports DROP COLUMN resolved_destination_collection_id;
