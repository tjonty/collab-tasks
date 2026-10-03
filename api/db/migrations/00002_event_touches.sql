-- Events list every task whose row they wrote, so clients can merge page
-- loads with live events by comparing seq against each task's updated_seq.

-- +goose Up
ALTER TABLE events ADD COLUMN touches uuid[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE events DROP COLUMN touches;
