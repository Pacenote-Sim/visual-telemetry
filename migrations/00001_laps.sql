-- The laps this plugin was given, trace and all, so that any of them can be
-- drawn again and any two overlaid.
--
-- The host creates the schema and runs this as a role that owns it and nothing
-- else, so everything here is unqualified. There is no down migration: removing
-- the plugin drops the schema whole.
CREATE TABLE laps (
    -- The server's own identifiers for the stint and the lap in it. A lap
    -- posted twice replaces itself.
    stint_id       text        NOT NULL,
    lap            integer     NOT NULL CHECK (lap > 0),
    driver_slug    text        NOT NULL,
    driver_name    text        NOT NULL DEFAULT '',
    session        text        NOT NULL DEFAULT '',
    track          text        NOT NULL DEFAULT '',
    car            text        NOT NULL DEFAULT '',
    track_length_m integer     NOT NULL DEFAULT 0,
    lap_ms         integer     NOT NULL DEFAULT 0,
    compound       text        NOT NULL DEFAULT '',
    -- Sector boundaries as fractions of the lap, and the corners' apexes with
    -- their numbers, as the client sent them.
    sectors        jsonb       NOT NULL DEFAULT '[]'::jsonb,
    corners        jsonb       NOT NULL DEFAULT '[]'::jsonb,
    -- The trace: the points as JSON, gzipped. A few thousand samples are a
    -- few hundred kilobytes uncompressed and a tenth of that here.
    trace          bytea       NOT NULL,
    points         integer     NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (stint_id, lap)
);

CREATE INDEX laps_driver ON laps (driver_slug, created_at DESC);
CREATE INDEX laps_created ON laps (created_at DESC);
