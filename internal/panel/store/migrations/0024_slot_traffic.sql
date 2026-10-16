-- +goose Up
-- Traffic by slot since it was handed out: a bound device owns a slot, so this
-- is what each device used (the shared place of apps without a device id
-- included). A table of its own, so counting never waits on a device row an
-- unbind is deleting. Counted from this version on: no backfill.
CREATE TABLE slot_traffic (
  slot_id INTEGER PRIMARY KEY REFERENCES slots(id) ON DELETE CASCADE,
  up      INTEGER NOT NULL DEFAULT 0,
  down    INTEGER NOT NULL DEFAULT 0
);

-- +goose Down
DROP TABLE slot_traffic;
