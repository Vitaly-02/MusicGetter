-- +goose Up
CREATE DOMAIN musicgetter.source AS text CHECK (VALUE IN ('spotify','yandex','vk'));
CREATE DOMAIN musicgetter.source_collection_kind AS text CHECK (VALUE IN ('favorites','playlist','album','selection'));
CREATE DOMAIN musicgetter.destination_collection_kind AS text CHECK (VALUE IN ('favorites','playlist','album'));

CREATE TABLE musicgetter.users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    telegram_user_id bigint NOT NULL UNIQUE CHECK (telegram_user_id > 0),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE musicgetter.extension_pairings (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id uuid REFERENCES musicgetter.users(id),
    code_hash bytea NOT NULL UNIQUE CHECK (octet_length(code_hash) = 32),
    challenge_hash bytea NOT NULL CHECK (octet_length(challenge_hash) = 32),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (expires_at > created_at),
    CHECK (consumed_at IS NULL OR (owner_id IS NOT NULL AND consumed_at >= created_at AND consumed_at < expires_at))
);
CREATE INDEX pairings_expiry ON musicgetter.extension_pairings(expires_at) WHERE consumed_at IS NULL;
CREATE INDEX pairings_owner ON musicgetter.extension_pairings(owner_id);

CREATE TABLE musicgetter.source_profiles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id uuid NOT NULL REFERENCES musicgetter.users(id),
    source musicgetter.source NOT NULL,
    profile_key text NOT NULL CHECK (length(btrim(profile_key)) BETWEEN 1 AND 256),
    label text NOT NULL CHECK (length(label) <= 1024),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (owner_id,source,profile_key),
    UNIQUE (owner_id,source,id)
);

CREATE TABLE musicgetter.source_collections (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id uuid NOT NULL,
    profile_id uuid NOT NULL,
    source musicgetter.source NOT NULL,
    collection_key text NOT NULL CHECK (length(btrim(collection_key)) BETWEEN 1 AND 512),
    provisional boolean NOT NULL DEFAULT false,
    kind musicgetter.source_collection_kind NOT NULL,
    title text NOT NULL CHECK (length(btrim(title)) BETWEEN 1 AND 1024),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (owner_id,source,profile_id) REFERENCES musicgetter.source_profiles(owner_id,source,id),
    UNIQUE (profile_id,collection_key),
    UNIQUE (owner_id,profile_id,source,id)
);
CREATE INDEX source_collections_owner ON musicgetter.source_collections(owner_id,id);

CREATE TABLE musicgetter.destination_connections (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id uuid NOT NULL REFERENCES musicgetter.users(id),
    adapter text NOT NULL CHECK (length(btrim(adapter)) BETWEEN 1 AND 128),
    account_key text NOT NULL CHECK (length(btrim(account_key)) BETWEEN 1 AND 256),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (owner_id,adapter,account_key),
    UNIQUE (owner_id,id)
);

CREATE TABLE musicgetter.destination_collections (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id uuid NOT NULL,
    connection_id uuid NOT NULL,
    external_key text NOT NULL CHECK (length(btrim(external_key)) BETWEEN 1 AND 512),
    kind musicgetter.destination_collection_kind NOT NULL,
    title text NOT NULL CHECK (length(btrim(title)) BETWEEN 1 AND 1024),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (owner_id,connection_id) REFERENCES musicgetter.destination_connections(owner_id,id),
    UNIQUE (connection_id,external_key),
    UNIQUE (owner_id,connection_id,id)
);
CREATE UNIQUE INDEX destination_one_favorites ON musicgetter.destination_collections(connection_id) WHERE kind = 'favorites';
CREATE INDEX destination_collections_owner ON musicgetter.destination_collections(owner_id,id);

-- +goose Down
-- Removes account/collection records. Back up first; no CASCADE to unknown objects.
DROP TABLE musicgetter.destination_collections;
DROP TABLE musicgetter.destination_connections;
DROP TABLE musicgetter.source_collections;
DROP TABLE musicgetter.source_profiles;
DROP TABLE musicgetter.extension_pairings;
DROP TABLE musicgetter.users;
DROP DOMAIN musicgetter.destination_collection_kind;
DROP DOMAIN musicgetter.source_collection_kind;
DROP DOMAIN musicgetter.source;
