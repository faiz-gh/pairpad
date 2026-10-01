-- Supports the expiry sweep, which scans rooms by last activity.
CREATE INDEX rooms_last_active_at_idx ON rooms (last_active_at);
