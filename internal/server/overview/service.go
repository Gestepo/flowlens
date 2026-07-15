package overview

import (
	"context"
	"errors"
	"fmt"
	"time"

	"flowlens/internal/model"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNodeNotFound = errors.New("node not found")
	ErrInvalidRange = errors.New("invalid overview range")
)

type Point struct {
	At            time.Time `json:"at"`
	InboundBytes  int64     `json:"inbound_bytes"`
	OutboundBytes int64     `json:"outbound_bytes"`
	InboundBPS    float64   `json:"inbound_bps"`
	OutboundBPS   float64   `json:"outbound_bps"`
}

type Overview struct {
	NodeID            string    `json:"node_id"`
	Range             string    `json:"range"`
	InboundBytes      int64     `json:"inbound_bytes"`
	OutboundBytes     int64     `json:"outbound_bytes"`
	ActiveConnections int64     `json:"active_connections"`
	DomainCoverage    *float64  `json:"domain_coverage"`
	Series            []Point   `json:"series"`
	DataFreshAt       time.Time `json:"data_fresh_at"`
}

type Service struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

type rangeSpec struct {
	duration time.Duration
	step     time.Duration
}

var ranges = map[string]rangeSpec{
	"1h":  {duration: time.Hour, step: time.Minute},
	"24h": {duration: 24 * time.Hour, step: 30 * time.Minute},
	"7d":  {duration: 7 * 24 * time.Hour, step: 6 * time.Hour},
	"30d": {duration: 30 * 24 * time.Hour, step: 24 * time.Hour},
}

func NewService(pool *pgxpool.Pool, now func() time.Time) *Service {
	return &Service{pool: pool, now: now}
}

func (service *Service) Summary(ctx context.Context, nodeID, requestedRange string) (Overview, error) {
	spec, ok := ranges[requestedRange]
	if !ok {
		return Overview{}, ErrInvalidRange
	}
	var dataFreshAt time.Time
	if err := service.pool.QueryRow(ctx, "SELECT COALESCE(last_seen_at, 'epoch'::timestamptz) FROM nodes WHERE id = $1", nodeID).Scan(&dataFreshAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Overview{}, ErrNodeNotFound
		}
		return Overview{}, fmt.Errorf("query node freshness: %w", err)
	}
	dataFreshAt = dataFreshAt.UTC()

	end := service.now().UTC().Truncate(spec.step).Add(spec.step)
	start := end.Add(-spec.duration)
	pointCount := int(spec.duration / spec.step)
	result := Overview{
		NodeID:      nodeID,
		Range:       requestedRange,
		Series:      make([]Point, pointCount),
		DataFreshAt: dataFreshAt,
	}
	indices := make(map[int64]int, pointCount)
	for index := range result.Series {
		at := start.Add(time.Duration(index) * spec.step)
		result.Series[index].At = at
		indices[at.UnixNano()] = index
	}

	rows, err := service.pool.Query(ctx, `
		SELECT bucket, direction, bytes
		FROM traffic_minute
		WHERE node_id = $1 AND bucket >= $2 AND bucket < $3
		ORDER BY bucket
	`, nodeID, start, end)
	if err != nil {
		return Overview{}, fmt.Errorf("query overview series: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var bucket time.Time
		var direction model.Direction
		var bytes int64
		if err := rows.Scan(&bucket, &direction, &bytes); err != nil {
			return Overview{}, fmt.Errorf("scan overview series: %w", err)
		}
		aligned := bucket.UTC().Truncate(spec.step)
		index, ok := indices[aligned.UnixNano()]
		if !ok {
			continue
		}
		switch direction {
		case model.DirectionInbound:
			result.InboundBytes += bytes
			result.Series[index].InboundBytes += bytes
		case model.DirectionOutbound:
			result.OutboundBytes += bytes
			result.Series[index].OutboundBytes += bytes
		}
	}
	if err := rows.Err(); err != nil {
		return Overview{}, fmt.Errorf("iterate overview series: %w", err)
	}
	seconds := spec.step.Seconds()
	for index := range result.Series {
		result.Series[index].InboundBPS = float64(result.Series[index].InboundBytes) / seconds
		result.Series[index].OutboundBPS = float64(result.Series[index].OutboundBytes) / seconds
	}
	var detailedConnections, namedConnections int64
	if err := service.pool.QueryRow(ctx, `
		SELECT
		  count(*) FILTER (WHERE observed_at >= $4 AND state <> 'closed'),
		  count(*),
		  count(*) FILTER (WHERE confidence <> 'ip_only')
		FROM connection_details
		WHERE node_id=$1 AND observed_at >= $2 AND observed_at < $3
	`, nodeID, start, end, service.now().UTC().Add(-15*time.Second)).Scan(&result.ActiveConnections, &detailedConnections, &namedConnections); err != nil {
		return Overview{}, fmt.Errorf("query overview detail metrics: %w", err)
	}
	if detailedConnections > 0 {
		coverage := float64(namedConnections) * 100 / float64(detailedConnections)
		result.DomainCoverage = &coverage
	}
	return result, nil
}
