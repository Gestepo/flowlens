package trafficquery

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"flowlens/internal/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

type CollectorGap struct {
	Collector string    `json:"collector"`
	Code      string    `json:"code"`
	At        time.Time `json:"at"`
	Recovered bool      `json:"recovered"`
}

type LiveItem struct {
	ID            string                 `json:"id"`
	ObservedAt    time.Time              `json:"observed_at"`
	Direction     model.Direction        `json:"direction"`
	OwnerID       string                 `json:"owner_id"`
	OwnerName     string                 `json:"owner_name"`
	Source        string                 `json:"source"`
	Destination   string                 `json:"destination"`
	DisplayName   string                 `json:"display_name"`
	Confidence    model.DomainConfidence `json:"confidence"`
	Protocol      string                 `json:"protocol"`
	State         string                 `json:"state"`
	BytesSent     int64                  `json:"bytes_sent"`
	BytesReceived int64                  `json:"bytes_received"`
}

type LiveMetrics struct {
	CurrentInboundBPS  float64 `json:"current_inbound_bps"`
	CurrentOutboundBPS float64 `json:"current_outbound_bps"`
	PeakInboundBPS     float64 `json:"peak_inbound_bps"`
	PeakOutboundBPS    float64 `json:"peak_outbound_bps"`
	ActiveConnections  int64   `json:"active_connections"`
}

type DomainItem struct {
	Domain      string                 `json:"domain"`
	Direction   model.Direction        `json:"direction"`
	Confidence  model.DomainConfidence `json:"confidence"`
	Bytes       int64                  `json:"bytes"`
	Connections int64                  `json:"connections"`
	Requests    int64                  `json:"requests"`
	OwnerCount  int64                  `json:"owner_count"`
}

type DomainStatus struct {
	Status   int   `json:"status"`
	Requests int64 `json:"requests"`
	Bytes    int64 `json:"bytes"`
}

type DomainSource struct {
	IP       string `json:"ip"`
	Requests int64  `json:"requests"`
	Bytes    int64  `json:"bytes"`
}

type DomainOwner struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Requests int64  `json:"requests"`
	Bytes    int64  `json:"bytes"`
}

type DomainNetwork struct {
	CountryCode    string `json:"country_code"`
	CountryName    string `json:"country_name"`
	ASN            uint32 `json:"asn"`
	Organization   string `json:"organization"`
	Classification string `json:"classification"`
	Connections    int64  `json:"connections"`
	Bytes          int64  `json:"bytes"`
}

type DomainDetail struct {
	DomainItem
	Statuses []DomainStatus  `json:"statuses"`
	Sources  []DomainSource  `json:"sources"`
	Owners   []DomainOwner   `json:"owners"`
	Networks []DomainNetwork `json:"networks"`
}

type OwnerItem struct {
	ID          string          `json:"id"`
	Kind        model.OwnerKind `json:"kind"`
	Name        string          `json:"name"`
	Bytes       int64           `json:"bytes"`
	Inbound     int64           `json:"inbound_bytes"`
	Outbound    int64           `json:"outbound_bytes"`
	Connections int64           `json:"connections"`
	Ports       []uint16        `json:"ports"`
}

type OwnerPoint struct {
	At            time.Time `json:"at"`
	InboundBytes  int64     `json:"inbound_bytes"`
	OutboundBytes int64     `json:"outbound_bytes"`
	InboundBPS    float64   `json:"inbound_bps"`
	OutboundBPS   float64   `json:"outbound_bps"`
}

type OwnerDetail struct {
	OwnerItem
	Series            []OwnerPoint   `json:"series"`
	ActiveConnections []LiveItem     `json:"active_connections"`
	DataFreshAt       time.Time      `json:"data_fresh_at"`
	PartialData       []CollectorGap `json:"partial_data"`
}

type FlowItem struct {
	Direction             model.Direction        `json:"direction"`
	OwnerID               string                 `json:"owner_id"`
	OwnerName             string                 `json:"owner_name"`
	Source                string                 `json:"source"`
	Destination           string                 `json:"destination"`
	Domain                string                 `json:"domain"`
	Confidence            model.DomainConfidence `json:"confidence"`
	Protocol              string                 `json:"protocol"`
	RemotePort            uint16                 `json:"remote_port"`
	CountryCode           string                 `json:"country_code"`
	CountryName           string                 `json:"country_name"`
	ASN                   uint32                 `json:"asn"`
	Organization          string                 `json:"organization"`
	NetworkClassification string                 `json:"network_classification"`
	Bytes                 int64                  `json:"bytes"`
	Connections           int64                  `json:"connections"`
	Requests              int64                  `json:"requests"`
}

type Response[T any] struct {
	Items       []T            `json:"items"`
	NextCursor  string         `json:"next_cursor,omitempty"`
	DataFreshAt time.Time      `json:"data_fresh_at"`
	PartialData []CollectorGap `json:"partial_data"`
	Metrics     *LiveMetrics   `json:"metrics,omitempty"`
}

type Service struct{ pool *pgxpool.Pool }

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

func (service *Service) Live(ctx context.Context, filter Filter) (Response[LiveItem], error) {
	offset := decodeCursor(filter.Cursor)
	order := map[string]string{"bytes": "bytes_sent+bytes_received", "connections": "observed_at", "requests": "observed_at", "time": "observed_at"}[filter.Sort]
	rows, err := service.pool.Query(ctx, fmt.Sprintf(`
		SELECT event_id,observed_at,direction,owner_id,owner_name,host(local_ip),local_port,host(remote_ip),remote_port,display_name,confidence,protocol,state,bytes_sent,bytes_received
		FROM connection_details WHERE node_id=$1 AND observed_at >= $2 AND observed_at < $3
		AND ($4='' OR direction=$4) AND ($5='' OR owner_id=$5) AND ($6='' OR display_name ILIKE '%%'||$6||'%%')
		AND ($7='' OR confidence=$7) AND ($8='' OR local_ip=$8::inet OR remote_ip=$8::inet)
		AND ($9=0 OR local_port=$9 OR remote_port=$9) AND ($10='' OR protocol=$10)
		ORDER BY %s DESC, observed_at DESC, event_id DESC LIMIT $11 OFFSET $12
	`, order), filter.NodeID, filter.Start, filter.End, filter.Direction, filter.OwnerID, filter.Domain, filter.Confidence, addrString(filter), int(filter.Port), filter.Protocol, filter.Limit, offset)
	if err != nil {
		return Response[LiveItem]{}, fmt.Errorf("query live traffic: %w", err)
	}
	defer rows.Close()
	result := Response[LiveItem]{Items: []LiveItem{}}
	for rows.Next() {
		var item LiveItem
		var localIP, remoteIP string
		var localPort, remotePort int
		if err := rows.Scan(&item.ID, &item.ObservedAt, &item.Direction, &item.OwnerID, &item.OwnerName, &localIP, &localPort, &remoteIP, &remotePort, &item.DisplayName, &item.Confidence, &item.Protocol, &item.State, &item.BytesSent, &item.BytesReceived); err != nil {
			return Response[LiveItem]{}, err
		}
		item.Source = net.JoinHostPort(localIP, strconv.Itoa(localPort))
		item.Destination = net.JoinHostPort(remoteIP, strconv.Itoa(remotePort))
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return Response[LiveItem]{}, err
	}
	metrics, err := service.liveMetrics(ctx, filter)
	if err != nil {
		return Response[LiveItem]{}, err
	}
	result.Metrics = &metrics
	service.decorate(ctx, filter, len(result.Items), offset, &result.NextCursor, &result.DataFreshAt, &result.PartialData)
	return result, nil
}

func (service *Service) liveMetrics(ctx context.Context, filter Filter) (LiveMetrics, error) {
	var metrics LiveMetrics
	err := service.pool.QueryRow(ctx, `
		SELECT
			coalesce(sum(bytes) FILTER (WHERE direction='inbound'),0)::float8 / 10,
			coalesce(sum(bytes) FILTER (WHERE direction='outbound'),0)::float8 / 10
		FROM interface_deltas
		WHERE node_id=$1 AND observed_at >= $2::timestamptz - interval '10 seconds' AND observed_at < $2::timestamptz
	`, filter.NodeID, filter.End).Scan(&metrics.CurrentInboundBPS, &metrics.CurrentOutboundBPS)
	if err != nil {
		return LiveMetrics{}, fmt.Errorf("query current live rates: %w", err)
	}
	err = service.pool.QueryRow(ctx, `
		WITH minute_rates AS (
			SELECT bucket,
				coalesce(sum(bytes) FILTER (WHERE direction='inbound'),0)::float8 / 60 inbound_bps,
				coalesce(sum(bytes) FILTER (WHERE direction='outbound'),0)::float8 / 60 outbound_bps
			FROM traffic_minute
			WHERE node_id=$1 AND bucket >= $2 AND bucket < $3
			GROUP BY bucket
		)
		SELECT coalesce(max(inbound_bps),0),coalesce(max(outbound_bps),0),
			(SELECT count(*) FROM connection_details
			 WHERE node_id=$1 AND observed_at >= $3::timestamptz - interval '15 seconds' AND observed_at < $3::timestamptz AND state<>'closed')
		FROM minute_rates
	`, filter.NodeID, filter.Start, filter.End).Scan(&metrics.PeakInboundBPS, &metrics.PeakOutboundBPS, &metrics.ActiveConnections)
	if err != nil {
		return LiveMetrics{}, fmt.Errorf("query peak live rates: %w", err)
	}
	return metrics, nil
}

func (service *Service) Domains(ctx context.Context, filter Filter) (Response[DomainItem], error) {
	offset := decodeCursor(filter.Cursor)
	order := map[string]string{"bytes": "total_bytes", "connections": "total_connections", "requests": "total_requests", "time": "last_bucket"}[filter.Sort]
	rows, err := service.pool.Query(ctx, fmt.Sprintf(`
		WITH ranked AS (
			SELECT domain,direction,confidence,sum(bytes) AS total_bytes,sum(connections) AS total_connections,sum(requests) AS total_requests,max(bucket) AS last_bucket
			FROM domain_minute WHERE node_id=$1 AND bucket >= $2 AND bucket < $3
			AND ($4='' OR direction=$4) AND ($5='' OR domain ILIKE '%%'||$5||'%%') AND ($6='' OR confidence=$6)
			GROUP BY domain,direction,confidence
		)
		SELECT domain,direction,confidence,total_bytes,total_connections,total_requests,
			CASE WHEN direction='inbound' THEN (
				SELECT count(DISTINCT coalesce(NULLIF(request.upstream_owner_id,''),NULLIF('upstream:'||request.upstream,'upstream:'))) FROM proxy_request_details request
				WHERE request.node_id=$1 AND request.observed_at >= $2 AND request.observed_at < $3 AND request.host=ranked.domain
			) ELSE (
				SELECT count(DISTINCT flow.owner_id) FROM flow_minute flow
				WHERE flow.node_id=$1 AND flow.bucket >= $2 AND flow.bucket < $3 AND flow.direction=ranked.direction
				AND flow.domain=ranked.domain AND flow.confidence=ranked.confidence
			) END AS owner_count,last_bucket
		FROM ranked ORDER BY %s DESC,domain LIMIT $7 OFFSET $8
	`, order), filter.NodeID, filter.Start, filter.End, filter.Direction, filter.Domain, filter.Confidence, filter.Limit, offset)
	if err != nil {
		return Response[DomainItem]{}, fmt.Errorf("query domains: %w", err)
	}
	defer rows.Close()
	result := Response[DomainItem]{Items: []DomainItem{}}
	for rows.Next() {
		var item DomainItem
		var lastBucket time.Time
		if err := rows.Scan(&item.Domain, &item.Direction, &item.Confidence, &item.Bytes, &item.Connections, &item.Requests, &item.OwnerCount, &lastBucket); err != nil {
			return Response[DomainItem]{}, err
		}
		result.Items = append(result.Items, item)
	}
	service.decorate(ctx, filter, len(result.Items), offset, &result.NextCursor, &result.DataFreshAt, &result.PartialData)
	return result, rows.Err()
}

func (service *Service) DomainDetail(ctx context.Context, filter Filter) (DomainDetail, error) {
	detail := DomainDetail{
		DomainItem: DomainItem{Domain: filter.Domain, Direction: filter.Direction, Confidence: filter.Confidence},
		Statuses:   []DomainStatus{}, Sources: []DomainSource{}, Owners: []DomainOwner{}, Networks: []DomainNetwork{},
	}
	err := service.pool.QueryRow(ctx, `
		SELECT coalesce(sum(bytes),0),coalesce(sum(connections),0),coalesce(sum(requests),0)
		FROM domain_minute WHERE node_id=$1 AND bucket >= $2 AND bucket < $3
		AND domain=$4 AND direction=$5 AND confidence=$6
	`, filter.NodeID, filter.Start, filter.End, filter.Domain, filter.Direction, filter.Confidence).Scan(&detail.Bytes, &detail.Connections, &detail.Requests)
	if err != nil {
		return DomainDetail{}, fmt.Errorf("query domain detail: %w", err)
	}
	if filter.Direction == model.DirectionInbound {
		if err := service.inboundDomainDetail(ctx, filter, &detail); err != nil {
			return DomainDetail{}, err
		}
	} else {
		if err := service.outboundDomainOwners(ctx, filter, &detail); err != nil {
			return DomainDetail{}, err
		}
		if err := service.outboundDomainNetworks(ctx, filter, &detail); err != nil {
			return DomainDetail{}, err
		}
	}
	detail.OwnerCount = int64(len(detail.Owners))
	return detail, nil
}

func (service *Service) inboundDomainDetail(ctx context.Context, filter Filter, detail *DomainDetail) error {
	statusRows, err := service.pool.Query(ctx, `
		SELECT status,sum(requests),sum(bytes) FROM proxy_status_minute
		WHERE node_id=$1 AND bucket >= $2 AND bucket < $3 AND host=$4
		GROUP BY status ORDER BY status
	`, filter.NodeID, filter.Start, filter.End, filter.Domain)
	if err != nil {
		return fmt.Errorf("query domain statuses: %w", err)
	}
	for statusRows.Next() {
		var item DomainStatus
		if err := statusRows.Scan(&item.Status, &item.Requests, &item.Bytes); err != nil {
			statusRows.Close()
			return err
		}
		detail.Statuses = append(detail.Statuses, item)
	}
	if err := statusRows.Err(); err != nil {
		statusRows.Close()
		return err
	}
	statusRows.Close()

	sourceRows, err := service.pool.Query(ctx, `
		SELECT host(source_ip),count(*),sum(bytes_sent) FROM proxy_request_details
		WHERE node_id=$1 AND observed_at >= $2 AND observed_at < $3 AND host=$4
		GROUP BY source_ip ORDER BY sum(bytes_sent) DESC LIMIT 10
	`, filter.NodeID, filter.Start, filter.End, filter.Domain)
	if err != nil {
		return fmt.Errorf("query domain sources: %w", err)
	}
	for sourceRows.Next() {
		var item DomainSource
		if err := sourceRows.Scan(&item.IP, &item.Requests, &item.Bytes); err != nil {
			sourceRows.Close()
			return err
		}
		detail.Sources = append(detail.Sources, item)
	}
	if err := sourceRows.Err(); err != nil {
		sourceRows.Close()
		return err
	}
	sourceRows.Close()

	ownerRows, err := service.pool.Query(ctx, `
		SELECT coalesce(NULLIF(request.upstream_owner_id,''),'upstream:'||request.upstream),coalesce(owner.display_name,NULLIF(request.upstream,''),'未归属'),count(*),sum(request.bytes_sent)
		FROM proxy_request_details request
		LEFT JOIN owners owner ON owner.node_id=request.node_id AND owner.owner_id=request.upstream_owner_id
		WHERE request.node_id=$1 AND request.observed_at >= $2 AND request.observed_at < $3 AND request.host=$4 AND (request.upstream_owner_id<>'' OR request.upstream<>'')
		GROUP BY request.upstream_owner_id,request.upstream,owner.display_name ORDER BY sum(request.bytes_sent) DESC LIMIT 10
	`, filter.NodeID, filter.Start, filter.End, filter.Domain)
	if err != nil {
		return fmt.Errorf("query domain owners: %w", err)
	}
	defer ownerRows.Close()
	for ownerRows.Next() {
		var item DomainOwner
		if err := ownerRows.Scan(&item.ID, &item.Name, &item.Requests, &item.Bytes); err != nil {
			return err
		}
		detail.Owners = append(detail.Owners, item)
	}
	return ownerRows.Err()
}

func (service *Service) outboundDomainOwners(ctx context.Context, filter Filter, detail *DomainDetail) error {
	rows, err := service.pool.Query(ctx, `
		SELECT owner_id,max(owner_name),sum(connections),sum(bytes) FROM flow_minute
		WHERE node_id=$1 AND bucket >= $2 AND bucket < $3 AND direction=$4 AND domain=$5 AND confidence=$6
		GROUP BY owner_id ORDER BY sum(bytes) DESC LIMIT 10
	`, filter.NodeID, filter.Start, filter.End, filter.Direction, filter.Domain, filter.Confidence)
	if err != nil {
		return fmt.Errorf("query domain owners: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item DomainOwner
		if err := rows.Scan(&item.ID, &item.Name, &item.Requests, &item.Bytes); err != nil {
			return err
		}
		detail.Owners = append(detail.Owners, item)
	}
	return rows.Err()
}

func (service *Service) outboundDomainNetworks(ctx context.Context, filter Filter, detail *DomainDetail) error {
	rows, err := service.pool.Query(ctx, `
		SELECT country_code,max(country_name),asn,max(organization),network_classification,sum(connections),sum(bytes)
		FROM flow_minute
		WHERE node_id=$1 AND bucket >= $2 AND bucket < $3 AND direction=$4 AND domain=$5 AND confidence=$6
		GROUP BY country_code,asn,network_classification ORDER BY sum(bytes) DESC LIMIT 10
	`, filter.NodeID, filter.Start, filter.End, filter.Direction, filter.Domain, filter.Confidence)
	if err != nil {
		return fmt.Errorf("query domain networks: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item DomainNetwork
		if err := rows.Scan(&item.CountryCode, &item.CountryName, &item.ASN, &item.Organization, &item.Classification, &item.Connections, &item.Bytes); err != nil {
			return err
		}
		detail.Networks = append(detail.Networks, item)
	}
	return rows.Err()
}

func (service *Service) Owners(ctx context.Context, filter Filter) (Response[OwnerItem], error) {
	offset := decodeCursor(filter.Cursor)
	order := map[string]string{"bytes": "total_bytes", "connections": "total_connections", "requests": "total_connections", "time": "last_bucket"}[filter.Sort]
	rows, err := service.pool.Query(ctx, fmt.Sprintf(`
		SELECT owner.owner_id,owner.kind,owner.display_name,coalesce(sum(minute.bytes),0) AS total_bytes,
			coalesce(sum(minute.bytes) FILTER (WHERE minute.direction='inbound'),0),coalesce(sum(minute.bytes) FILTER (WHERE minute.direction='outbound'),0),
			coalesce(sum(minute.connections),0) AS total_connections,greatest(owner.last_seen_at,coalesce(max(minute.bucket),'epoch')),coalesce(owner.ports,'[]'::jsonb)
		FROM owners owner LEFT JOIN owner_minute minute ON minute.node_id=owner.node_id AND minute.owner_id=owner.owner_id
			AND minute.bucket >= $2 AND minute.bucket < $3 AND ($4='' OR minute.direction=$4)
		WHERE owner.node_id=$1 AND (owner.running OR minute.owner_id IS NOT NULL)
		AND ($5='' OR owner.owner_id=$5 OR owner.display_name ILIKE '%%'||$5||'%%')
		GROUP BY owner.owner_id,owner.kind,owner.display_name,owner.last_seen_at,owner.ports ORDER BY %s DESC,owner.display_name LIMIT $6 OFFSET $7
	`, order), filter.NodeID, filter.Start, filter.End, filter.Direction, filter.OwnerID, filter.Limit, offset)
	if err != nil {
		return Response[OwnerItem]{}, fmt.Errorf("query owners: %w", err)
	}
	defer rows.Close()
	result := Response[OwnerItem]{Items: []OwnerItem{}}
	for rows.Next() {
		var item OwnerItem
		var lastBucket time.Time
		var ports []byte
		if err := rows.Scan(&item.ID, &item.Kind, &item.Name, &item.Bytes, &item.Inbound, &item.Outbound, &item.Connections, &lastBucket, &ports); err != nil {
			return Response[OwnerItem]{}, err
		}
		if err := json.Unmarshal(ports, &item.Ports); err != nil {
			return Response[OwnerItem]{}, err
		}
		result.Items = append(result.Items, item)
	}
	service.decorate(ctx, filter, len(result.Items), offset, &result.NextCursor, &result.DataFreshAt, &result.PartialData)
	return result, rows.Err()
}

func (service *Service) OwnerDetail(ctx context.Context, filter Filter) (OwnerDetail, error) {
	owners, err := service.Owners(ctx, filter)
	if err != nil {
		return OwnerDetail{}, err
	}
	var item *OwnerItem
	for index := range owners.Items {
		if owners.Items[index].ID == filter.OwnerID {
			item = &owners.Items[index]
			break
		}
	}
	if item == nil {
		return OwnerDetail{}, fmt.Errorf("owner %q was not found", filter.OwnerID)
	}
	detail := OwnerDetail{OwnerItem: *item, Series: []OwnerPoint{}, ActiveConnections: []LiveItem{}, DataFreshAt: owners.DataFreshAt, PartialData: owners.PartialData}
	rows, err := service.pool.Query(ctx, `
		SELECT bucket,coalesce(sum(bytes) FILTER (WHERE direction='inbound'),0),coalesce(sum(bytes) FILTER (WHERE direction='outbound'),0)
		FROM owner_minute WHERE node_id=$1 AND owner_id=$2 AND bucket >= $3 AND bucket < $4
		GROUP BY bucket ORDER BY bucket
	`, filter.NodeID, filter.OwnerID, filter.Start, filter.End)
	if err != nil {
		return OwnerDetail{}, fmt.Errorf("query owner trend: %w", err)
	}
	for rows.Next() {
		var point OwnerPoint
		if err := rows.Scan(&point.At, &point.InboundBytes, &point.OutboundBytes); err != nil {
			rows.Close()
			return OwnerDetail{}, err
		}
		point.InboundBPS = float64(point.InboundBytes) / 60
		point.OutboundBPS = float64(point.OutboundBytes) / 60
		detail.Series = append(detail.Series, point)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return OwnerDetail{}, err
	}
	rows.Close()

	activeSince := owners.DataFreshAt.Add(-15 * time.Second)
	activeRows, err := service.pool.Query(ctx, `
		SELECT event_id,observed_at,direction,owner_id,owner_name,host(local_ip),local_port,host(remote_ip),remote_port,display_name,confidence,protocol,state,bytes_sent,bytes_received
		FROM connection_details WHERE node_id=$1 AND owner_id=$2 AND observed_at >= $3 AND observed_at < $4 AND state<>'closed'
		ORDER BY observed_at DESC,event_id DESC LIMIT 20
	`, filter.NodeID, filter.OwnerID, activeSince, filter.End)
	if err != nil {
		return OwnerDetail{}, fmt.Errorf("query owner active connections: %w", err)
	}
	defer activeRows.Close()
	for activeRows.Next() {
		var connection LiveItem
		var localIP, remoteIP string
		var localPort, remotePort int
		if err := activeRows.Scan(&connection.ID, &connection.ObservedAt, &connection.Direction, &connection.OwnerID, &connection.OwnerName, &localIP, &localPort, &remoteIP, &remotePort, &connection.DisplayName, &connection.Confidence, &connection.Protocol, &connection.State, &connection.BytesSent, &connection.BytesReceived); err != nil {
			return OwnerDetail{}, err
		}
		connection.Source = net.JoinHostPort(localIP, strconv.Itoa(localPort))
		connection.Destination = net.JoinHostPort(remoteIP, strconv.Itoa(remotePort))
		detail.ActiveConnections = append(detail.ActiveConnections, connection)
	}
	return detail, activeRows.Err()
}

func (service *Service) Flows(ctx context.Context, filter Filter) (Response[FlowItem], error) {
	offset := decodeCursor(filter.Cursor)
	order := map[string]string{"bytes": "total_bytes", "connections": "total_connections", "requests": "total_requests", "time": "last_bucket"}[filter.Sort]
	rows, err := service.pool.Query(ctx, fmt.Sprintf(`
		SELECT direction,owner_id,owner_name,source,destination,domain,confidence,protocol,remote_port,country_code,country_name,asn,organization,network_classification,sum(bytes) AS total_bytes,sum(connections) AS total_connections,sum(requests) AS total_requests,max(bucket) AS last_bucket
		FROM flow_minute WHERE node_id=$1 AND bucket >= $2 AND bucket < $3
		AND ($4='' OR direction=$4) AND ($5='' OR owner_id=$5) AND ($6='' OR domain ILIKE '%%'||$6||'%%')
		AND ($7='' OR confidence=$7) AND ($8='' OR source=$8 OR destination=$8) AND ($9=0 OR remote_port=$9) AND ($10='' OR protocol=$10)
		GROUP BY direction,owner_id,owner_name,source,destination,domain,confidence,protocol,remote_port,country_code,country_name,asn,organization,network_classification
		ORDER BY %s DESC,domain LIMIT $11 OFFSET $12
	`, order), filter.NodeID, filter.Start, filter.End, filter.Direction, filter.OwnerID, filter.Domain, filter.Confidence, addrString(filter), int(filter.Port), filter.Protocol, filter.Limit, offset)
	if err != nil {
		return Response[FlowItem]{}, fmt.Errorf("query flows: %w", err)
	}
	defer rows.Close()
	result := Response[FlowItem]{Items: []FlowItem{}}
	for rows.Next() {
		var item FlowItem
		var lastBucket time.Time
		if err := rows.Scan(&item.Direction, &item.OwnerID, &item.OwnerName, &item.Source, &item.Destination, &item.Domain, &item.Confidence, &item.Protocol, &item.RemotePort, &item.CountryCode, &item.CountryName, &item.ASN, &item.Organization, &item.NetworkClassification, &item.Bytes, &item.Connections, &item.Requests, &lastBucket); err != nil {
			return Response[FlowItem]{}, err
		}
		result.Items = append(result.Items, item)
	}
	service.decorate(ctx, filter, len(result.Items), offset, &result.NextCursor, &result.DataFreshAt, &result.PartialData)
	return result, rows.Err()
}

func (service *Service) decorate(ctx context.Context, filter Filter, count, offset int, next *string, fresh *time.Time, gaps *[]CollectorGap) {
	if *gaps == nil {
		*gaps = []CollectorGap{}
	}
	_ = service.pool.QueryRow(ctx, `SELECT COALESCE(last_seen_at,'epoch') FROM nodes WHERE id=$1`, filter.NodeID).Scan(fresh)
	rows, err := service.pool.Query(ctx, `
		SELECT degraded.collector,degraded.code,degraded.observed_at,
			EXISTS (
				SELECT 1 FROM collector_health recovered
				WHERE recovered.node_id=$1 AND recovered.collector=degraded.collector
				AND recovered.status='healthy' AND recovered.observed_at > degraded.observed_at AND recovered.observed_at < $3
			)
		FROM (
			SELECT DISTINCT ON (collector) collector,code,observed_at
			FROM collector_health
			WHERE node_id=$1 AND observed_at >= $2 AND observed_at < $3 AND status <> 'healthy'
			ORDER BY collector,observed_at DESC
		) degraded
		ORDER BY degraded.collector
	`, filter.NodeID, filter.Start, filter.End)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var gap CollectorGap
			if rows.Scan(&gap.Collector, &gap.Code, &gap.At, &gap.Recovered) == nil {
				*gaps = append(*gaps, gap)
			}
		}
	}
	if count == filter.Limit {
		*next = encodeCursor(offset + count)
	}
}

func addrString(filter Filter) string {
	if filter.IP.IsValid() {
		return filter.IP.String()
	}
	return ""
}

func encodeCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func decodeCursor(cursor string) int {
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0
	}
	offset, err := strconv.Atoi(strings.TrimSpace(string(decoded)))
	if err != nil || offset < 0 {
		return 0
	}
	return offset
}
