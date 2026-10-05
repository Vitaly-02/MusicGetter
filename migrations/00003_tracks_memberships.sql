-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION musicgetter.valid_artists(values_ text[]) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT COALESCE(array_ndims(values_) = 1 AND cardinality(values_) BETWEEN 1 AND 32
        AND array_position(values_, NULL) IS NULL
        AND (SELECT bool_and(length(btrim(v)) BETWEEN 1 AND 512) FROM unnest(values_) AS v), false);
$$;
-- +goose StatementEnd

CREATE TABLE musicgetter.canonical_tracks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id uuid NOT NULL,
    profile_id uuid NOT NULL,
    source musicgetter.source NOT NULL,
    source_track_key text CHECK (length(source_track_key) BETWEEN 1 AND 512 AND source_track_key = btrim(source_track_key)),
    source_url text CHECK (length(source_url) <= 2048 AND (
        (source = 'spotify' AND source_url ~ '^https://open[.]spotify[.]com/[^?#]*$') OR
        (source = 'yandex' AND source_url ~ '^https://music[.]yandex[.](ru|com|kz)/[^?#]*$') OR
        (source = 'vk' AND source_url ~ '^https://(music[.])?vk[.]com/[^?#]*$')
    )),
    title text NOT NULL CHECK (length(btrim(title)) BETWEEN 1 AND 1024),
    artists text[] NOT NULL CHECK (musicgetter.valid_artists(artists)),
    album text NOT NULL DEFAULT '' CHECK (length(album) <= 1024),
    duration_ms bigint CHECK (duration_ms BETWEEN 0 AND 9007199254740991),
    edition text NOT NULL DEFAULT '' CHECK (length(edition) <= 512),
    normalized_title text NOT NULL CHECK (length(btrim(normalized_title)) > 0),
    normalized_artists text[] NOT NULL CHECK (musicgetter.valid_artists(normalized_artists)),
    fingerprint text NOT NULL CHECK (fingerprint ~ '^v1:[0-9a-f]{64}$'),
    identity_key text GENERATED ALWAYS AS
        (CASE WHEN source_track_key IS NULL THEN 'fp:' || fingerprint ELSE 'key:' || source_track_key END) STORED,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (owner_id,source,profile_id) REFERENCES musicgetter.source_profiles(owner_id,source,id),
    UNIQUE (profile_id,identity_key),
    UNIQUE (owner_id,id),
    UNIQUE (owner_id,profile_id,source,id)
);
CREATE INDEX canonical_tracks_fingerprint ON musicgetter.canonical_tracks(owner_id,source,fingerprint);

CREATE TABLE musicgetter.destination_tracks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id uuid NOT NULL,
    connection_id uuid NOT NULL,
    external_key text NOT NULL CHECK (length(btrim(external_key)) BETWEEN 1 AND 512),
    title text NOT NULL CHECK (length(btrim(title)) BETWEEN 1 AND 1024),
    artists text[] NOT NULL CHECK (musicgetter.valid_artists(artists)),
    album text NOT NULL DEFAULT '' CHECK (length(album) <= 1024),
    duration_ms bigint CHECK (duration_ms BETWEEN 0 AND 9007199254740991),
    edition text NOT NULL DEFAULT '' CHECK (length(edition) <= 512),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (owner_id,connection_id) REFERENCES musicgetter.destination_connections(owner_id,id),
    UNIQUE (connection_id,external_key),
    UNIQUE (owner_id,connection_id,id)
);

CREATE TABLE musicgetter.track_mappings (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id uuid NOT NULL,
    canonical_track_id uuid NOT NULL,
    connection_id uuid NOT NULL,
    destination_track_id uuid NOT NULL,
    origin text NOT NULL CHECK (origin IN ('manual','automatic')),
    policy_version text NOT NULL CHECK (length(btrim(policy_version)) BETWEEN 1 AND 128),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (owner_id,canonical_track_id) REFERENCES musicgetter.canonical_tracks(owner_id,id),
    FOREIGN KEY (owner_id,connection_id,destination_track_id) REFERENCES musicgetter.destination_tracks(owner_id,connection_id,id),
    UNIQUE (canonical_track_id,connection_id)
);
CREATE INDEX track_mappings_destination ON musicgetter.track_mappings(owner_id,connection_id,destination_track_id);

CREATE TABLE musicgetter.destination_memberships (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id uuid NOT NULL,
    connection_id uuid NOT NULL,
    collection_id uuid NOT NULL,
    destination_track_id uuid NOT NULL,
    operation_key uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    state text NOT NULL DEFAULT 'reserved' CHECK (state IN ('reserved','unknown','applied')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (owner_id,connection_id,collection_id) REFERENCES musicgetter.destination_collections(owner_id,connection_id,id),
    FOREIGN KEY (owner_id,connection_id,destination_track_id) REFERENCES musicgetter.destination_tracks(owner_id,connection_id,id),
    UNIQUE (collection_id,destination_track_id)
);
CREATE INDEX memberships_owner ON musicgetter.destination_memberships(owner_id,collection_id,id);
CREATE INDEX memberships_track ON musicgetter.destination_memberships(owner_id,connection_id,destination_track_id);

-- +goose Down
-- Destructive: removes persistent identity and deduplication ledgers. Back up first.
DROP TABLE musicgetter.destination_memberships;
DROP TABLE musicgetter.track_mappings;
DROP TABLE musicgetter.destination_tracks;
DROP TABLE musicgetter.canonical_tracks;
DROP FUNCTION musicgetter.valid_artists(text[]);
