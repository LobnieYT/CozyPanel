-- +goose Up
-- A speed cap in Mbit/s each way: on the plan the user's own copies, on the user a
-- limit of its own over the plan's. NULL — no cap.
ALTER TABLE tariffs ADD COLUMN speed_limit INTEGER CHECK (speed_limit IS NULL OR speed_limit > 0);
ALTER TABLE users ADD COLUMN speed_limit INTEGER CHECK (speed_limit IS NULL OR speed_limit > 0);

-- Fair share on a node: the channel's width in Mbit/s and whether it is divided between
-- those who are downloading at the moment.
ALTER TABLE nodes ADD COLUMN fair_share INTEGER NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN channel_mbps INTEGER CHECK (channel_mbps IS NULL OR channel_mbps > 0);

-- +goose Down
ALTER TABLE nodes DROP COLUMN channel_mbps;
ALTER TABLE nodes DROP COLUMN fair_share;
ALTER TABLE users DROP COLUMN speed_limit;
ALTER TABLE tariffs DROP COLUMN speed_limit;