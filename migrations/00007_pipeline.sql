-- +goose Up
-- Stop API/worker/bot during upgrade: public state vocabulary changes atomically.
ALTER TABLE musicgetter.imports DROP CONSTRAINT imports_state_check;
UPDATE musicgetter.imports SET state=CASE state WHEN 'collecting' THEN 'receiving' WHEN 'running' THEN 'processing' WHEN 'needs_attention' THEN 'processing' ELSE state END;
ALTER TABLE musicgetter.imports ADD CONSTRAINT imports_state_check CHECK(state IN ('created','receiving','queued','processing','completed','completed_with_errors','cancelled','failed'));
ALTER TABLE musicgetter.import_items DROP CONSTRAINT import_items_state_check;
ALTER TABLE musicgetter.import_items DROP CONSTRAINT import_items_check;
ALTER TABLE musicgetter.import_items ADD COLUMN error_code text NOT NULL DEFAULT '' CHECK(error_code ~ '^[a-z0-9_]{0,128}$');
UPDATE musicgetter.import_items SET error_code='import_cancelled' WHERE state='cancelled';
UPDATE musicgetter.import_items SET state=CASE state WHEN 'needs_review' THEN 'ambiguous' WHEN 'ensuring' THEN 'matched' WHEN 'reconciling' THEN 'matched' WHEN 'skipped' THEN 'not_found' WHEN 'cancelled' THEN 'failed' ELSE state END;
ALTER TABLE musicgetter.import_items ADD CONSTRAINT import_items_state_check CHECK(state IN ('pending','searching','matched','ambiguous','not_found','already_present','added','failed'));
ALTER TABLE musicgetter.import_items ADD CONSTRAINT import_items_check CHECK(state NOT IN ('matched','already_present','added') OR destination_track_id IS NOT NULL);
CREATE INDEX import_jobs_failed_repair ON musicgetter.import_jobs(import_id,item_id) WHERE state='failed';
-- +goose Down
-- Stop processes. Unknown membership intents MUST be retained across rollback.
DROP INDEX musicgetter.import_jobs_failed_repair;
ALTER TABLE musicgetter.import_items DROP CONSTRAINT import_items_state_check;
UPDATE musicgetter.import_items SET state=CASE WHEN error_code='import_cancelled' THEN 'cancelled' WHEN state='ambiguous' THEN 'needs_review' WHEN state='not_found' THEN 'skipped' ELSE state END;
ALTER TABLE musicgetter.import_items ADD CONSTRAINT import_items_state_check CHECK(state IN ('pending','searching','needs_review','matched','ensuring','reconciling','added','already_present','skipped','failed','cancelled'));
ALTER TABLE musicgetter.import_items DROP CONSTRAINT import_items_check;
ALTER TABLE musicgetter.import_items ADD CONSTRAINT import_items_check CHECK(state NOT IN ('matched','ensuring','reconciling','added','already_present') OR destination_track_id IS NOT NULL);
ALTER TABLE musicgetter.import_items DROP COLUMN error_code;
ALTER TABLE musicgetter.imports DROP CONSTRAINT imports_state_check;
UPDATE musicgetter.imports SET state=CASE state WHEN 'created' THEN 'collecting' WHEN 'receiving' THEN 'collecting' WHEN 'processing' THEN 'running' ELSE state END;
ALTER TABLE musicgetter.imports ADD CONSTRAINT imports_state_check CHECK(state IN ('collecting','queued','running','needs_attention','completed','completed_with_errors','cancelled','failed'));
