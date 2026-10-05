-- +goose Up
-- Reserve an application namespace without introducing import/domain tables.
CREATE SCHEMA musicgetter;

-- +goose Down
-- RESTRICT intentionally refuses to destroy tables added by later migrations.
DROP SCHEMA musicgetter RESTRICT;
