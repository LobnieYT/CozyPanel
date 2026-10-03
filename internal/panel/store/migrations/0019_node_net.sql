-- +goose Up
-- Per-node network overrides (DNS, routes, outbounds) as JSON documents; NULL or ''
-- means the node inherits the global settings (node_dns, node_routes, node_outbounds).
ALTER TABLE nodes ADD COLUMN dns_override TEXT;
ALTER TABLE nodes ADD COLUMN routes_override TEXT;
ALTER TABLE nodes ADD COLUMN outbounds_override TEXT;

-- +goose Down
ALTER TABLE nodes DROP COLUMN outbounds_override;
ALTER TABLE nodes DROP COLUMN routes_override;
ALTER TABLE nodes DROP COLUMN dns_override;
