ALTER TABLE throttle ADD COLUMN last_at INTEGER NOT NULL DEFAULT 0;
UPDATE throttle SET last_at = first_at;
