-- +goose Up
ALTER TABLE musicgetter.extension_pairings ALTER COLUMN challenge_hash DROP NOT NULL;
ALTER TABLE musicgetter.extension_pairings ADD COLUMN revoked_at timestamptz;
ALTER TABLE musicgetter.extension_pairings ADD CONSTRAINT bot_pairing_owner CHECK (challenge_hash IS NOT NULL OR owner_id IS NOT NULL);
CREATE TABLE musicgetter.extension_sessions (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 owner_id uuid NOT NULL REFERENCES musicgetter.users(id),
 token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash)=32),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 expires_at timestamptz NOT NULL DEFAULT (clock_timestamp()+interval '30 days'),
 revoked_at timestamptz,
 CHECK (expires_at>created_at)
);
CREATE INDEX extension_sessions_owner ON musicgetter.extension_sessions(owner_id);
CREATE TABLE musicgetter.telegram_updates (
 bot_id bigint NOT NULL,
 update_id bigint NOT NULL,
 claimed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(bot_id,update_id)
);
CREATE INDEX telegram_updates_age ON musicgetter.telegram_updates(claimed_at);
CREATE INDEX imports_owner_recent ON musicgetter.imports(owner_id,created_at DESC,id DESC);
-- +goose Down
-- Destructive: sessions and bot-issued codes cannot be recovered. Stop bot/server first.
DROP INDEX musicgetter.imports_owner_recent;
DROP TABLE musicgetter.telegram_updates;
DROP TABLE musicgetter.extension_sessions;
DELETE FROM musicgetter.extension_pairings WHERE challenge_hash IS NULL OR revoked_at IS NOT NULL;
ALTER TABLE musicgetter.extension_pairings DROP CONSTRAINT bot_pairing_owner;
ALTER TABLE musicgetter.extension_pairings DROP COLUMN revoked_at;
ALTER TABLE musicgetter.extension_pairings ALTER COLUMN challenge_hash SET NOT NULL;
