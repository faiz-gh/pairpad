CREATE TABLE rooms (
    id             TEXT PRIMARY KEY,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_active_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE room_updates (
    id          BIGSERIAL PRIMARY KEY,
    room_id     TEXT NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    data        BYTEA NOT NULL,
    is_snapshot BOOLEAN NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX room_updates_room_id_id_idx ON room_updates (room_id, id);
