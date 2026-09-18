package visualtelemetry

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pacenote-sim/protocol/wire"
)

// This plugin's one table, in its own schema: the laps it was given.

// Store is this plugin's database.
type Store struct{ pool *pgxpool.Pool }

// Open connects to the database the host handed over.
func Open(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("visual-telemetry: the connection string is not one: %w", err)
	}
	cfg.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("visual-telemetry: the database would not open: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("visual-telemetry: the database would not answer: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Close returns every connection.
func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

var errNoStore = errors.New("visual-telemetry: there is no database")

func (s *Store) ready() error {
	if s == nil || s.pool == nil {
		return errNoStore
	}
	return nil
}

// ErrNoRows is a lookup that found nothing.
var ErrNoRows = pgx.ErrNoRows

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// Put keeps a lap. The same stint and lap again replaces it: a client that
// retried has the same lap, and the newer copy is the one it meant.
func (s *Store) Put(ctx context.Context, l Lap) error {
	if err := s.ready(); err != nil {
		return err
	}
	blob, err := packTrace(l.Trace)
	if err != nil {
		return err
	}
	sectors, err := json.Marshal(orEmpty(l.Sectors))
	if err != nil {
		return fmt.Errorf("visual-telemetry: the sectors could not be encoded: %w", err)
	}
	corners, err := json.Marshal(orEmpty(l.Corners))
	if err != nil {
		return fmt.Errorf("visual-telemetry: the corners could not be encoded: %w", err)
	}
	if l.CreatedAt.IsZero() {
		l.CreatedAt = time.Now()
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO laps (stint_id, lap, driver_slug, driver_name, session, track, car, track_length_m, lap_ms,
		                  compound, sectors, corners, trace, points, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (stint_id, lap) DO UPDATE
		   SET driver_slug = EXCLUDED.driver_slug, driver_name = EXCLUDED.driver_name, session = EXCLUDED.session,
		       track = EXCLUDED.track, car = EXCLUDED.car, track_length_m = EXCLUDED.track_length_m,
		       lap_ms = EXCLUDED.lap_ms, compound = EXCLUDED.compound, sectors = EXCLUDED.sectors,
		       corners = EXCLUDED.corners, trace = EXCLUDED.trace, points = EXCLUDED.points,
		       created_at = EXCLUDED.created_at`,
		l.StintID, l.Lap, l.DriverSlug, l.DriverName, l.Session, l.Track, l.Car, l.TrackLengthM, l.LapMs,
		l.Compound, sectors, corners, blob, len(l.Trace), l.CreatedAt)
	if err != nil {
		return fmt.Errorf("visual-telemetry: the lap could not be kept: %w", err)
	}
	return nil
}

func orEmpty[T any](in []T) []T {
	if in == nil {
		return []T{}
	}
	return in
}

const lapColumns = `stint_id, lap, driver_slug, driver_name, session, track, car, track_length_m, lap_ms,
	       compound, sectors, corners, trace, created_at`

// Get is one lap, trace and all, or [ErrNoRows].
func (s *Store) Get(ctx context.Context, stintID string, lap int) (Lap, error) {
	if err := s.ready(); err != nil {
		return Lap{}, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+lapColumns+` FROM laps WHERE stint_id = $1 AND lap = $2`, stintID, lap)
	if err != nil {
		return Lap{}, fmt.Errorf("visual-telemetry: the lap could not be read: %w", err)
	}
	defer rows.Close()
	list, err := scanLaps(rows, true)
	if err != nil {
		return Lap{}, err
	}
	if len(list) == 0 {
		return Lap{}, ErrNoRows
	}
	return list[0], nil
}

// Recent is a driver's last laps, newest first, without their traces: what a
// page lists. Everyone's when driverSlug is empty is deliberately not offered;
// RecentAll is.
func (s *Store) Recent(ctx context.Context, driverSlug string, limit int) ([]Lap, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+lapColumns+` FROM laps WHERE driver_slug = $1 ORDER BY created_at DESC, lap DESC LIMIT $2`,
		driverSlug, bounded(limit))
	if err != nil {
		return nil, fmt.Errorf("visual-telemetry: the laps could not be read: %w", err)
	}
	defer rows.Close()
	return scanLaps(rows, false)
}

// RecentAll is every driver's last laps, newest first, without their traces.
func (s *Store) RecentAll(ctx context.Context, limit int) ([]Lap, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+lapColumns+` FROM laps ORDER BY created_at DESC, lap DESC LIMIT $1`, bounded(limit))
	if err != nil {
		return nil, fmt.Errorf("visual-telemetry: the laps could not be read: %w", err)
	}
	defer rows.Close()
	return scanLaps(rows, false)
}

func bounded(limit int) int {
	if limit <= 0 || limit > 500 {
		return 100
	}
	return limit
}

// scanLaps reads rows; withTrace unpacks the trace, which a list does not need.
func scanLaps(rows pgx.Rows, withTrace bool) ([]Lap, error) {
	var out []Lap
	for rows.Next() {
		var l Lap
		var sectors, corners, blob []byte
		if err := rows.Scan(&l.StintID, &l.Lap, &l.DriverSlug, &l.DriverName, &l.Session, &l.Track, &l.Car,
			&l.TrackLengthM, &l.LapMs, &l.Compound, &sectors, &corners, &blob, &l.CreatedAt); err != nil {
			return nil, fmt.Errorf("visual-telemetry: a lap could not be read: %w", err)
		}
		_ = json.Unmarshal(sectors, &l.Sectors)
		_ = json.Unmarshal(corners, &l.Corners)
		if withTrace {
			pts, err := unpackTrace(blob)
			if err != nil {
				return nil, err
			}
			l.Trace = pts
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("visual-telemetry: the laps could not be read: %w", err)
	}
	return out, nil
}

// Prune removes laps older than the operator asked to keep. Zero keeps all.
func (s *Store) Prune(ctx context.Context, keepDays int) (int64, error) {
	if s == nil || s.pool == nil || keepDays <= 0 {
		return 0, nil
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM laps WHERE created_at < now() - make_interval(days => $1)`, keepDays)
	if err != nil {
		return 0, fmt.Errorf("visual-telemetry: old laps could not be removed: %w", err)
	}
	return tag.RowsAffected(), nil
}

// packTrace is the points as gzipped JSON: the protocol's own shape, so the
// bytes kept are the bytes that arrived, a tenth the size.
func packTrace(pts []wire.TracePoint) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if err := json.NewEncoder(zw).Encode(pts); err != nil {
		return nil, fmt.Errorf("visual-telemetry: the trace could not be encoded: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("visual-telemetry: the trace could not be compressed: %w", err)
	}
	return buf.Bytes(), nil
}

func unpackTrace(blob []byte) ([]wire.TracePoint, error) {
	zr, err := gzip.NewReader(bytes.NewReader(blob))
	if err != nil {
		return nil, fmt.Errorf("visual-telemetry: a kept trace could not be read: %w", err)
	}
	defer func() { _ = zr.Close() }()
	var pts []wire.TracePoint
	if err := json.NewDecoder(io.LimitReader(zr, 64<<20)).Decode(&pts); err != nil {
		return nil, fmt.Errorf("visual-telemetry: a kept trace could not be decoded: %w", err)
	}
	return pts, nil
}
