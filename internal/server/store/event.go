package store

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"flowlens/internal/model"
	"flowlens/internal/server/attribution"
	"flowlens/internal/server/geoip"

	"github.com/jackc/pgx/v5"
)

func insertEvent(ctx context.Context, tx pgx.Tx, nodeID string, event model.Event, resolver GeoIPResolver) error {
	switch event.Kind {
	case model.EventInterfaceDelta:
		return insertInterfaceDelta(ctx, tx, nodeID, event)
	case model.EventHealth:
		return insertHealth(ctx, tx, nodeID, event)
	case model.EventOwnerInventory:
		return insertOwnerInventory(ctx, tx, nodeID, event)
	case model.EventNameEvidence:
		return insertNameEvidence(ctx, tx, nodeID, event)
	case model.EventConnection:
		return insertConnection(ctx, tx, nodeID, event, resolver)
	case model.EventProxyRequest:
		return insertProxyRequest(ctx, tx, nodeID, event, resolver)
	default:
		return fmt.Errorf("insert event %q: unsupported kind %q", event.ID, event.Kind)
	}
}

func insertOwnerInventory(ctx context.Context, tx pgx.Tx, nodeID string, event model.Event) error {
	inventory := event.OwnerInventory
	ownerID, ownerName := ownerIdentity(inventory.Owner)
	addresses, err := json.Marshal(inventory.Addresses)
	if err != nil {
		return err
	}
	ports, err := json.Marshal(inventory.Ports)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO owners (node_id, owner_id, kind, display_name, pid, container_id, cgroup_id, addresses, ports, running, first_seen_at, last_seen_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$11)
		ON CONFLICT (node_id, owner_id) DO UPDATE SET
		  display_name=EXCLUDED.display_name, pid=EXCLUDED.pid, container_id=EXCLUDED.container_id,
		  cgroup_id=EXCLUDED.cgroup_id, addresses=EXCLUDED.addresses, ports=EXCLUDED.ports,
		  running=EXCLUDED.running, last_seen_at=GREATEST(owners.last_seen_at, EXCLUDED.last_seen_at)
	`, nodeID, ownerID, inventory.Owner.Kind, ownerName, nullablePID(inventory.Owner), nullableString(inventory.Owner.ContainerID), nullableUint64(inventory.CgroupID), addresses, ports, inventory.Running, event.ObservedAt)
	if err != nil {
		return fmt.Errorf("upsert owner inventory event %q: %w", event.ID, err)
	}
	return nil
}

func insertNameEvidence(ctx context.Context, tx pgx.Tx, nodeID string, event model.Event) error {
	evidence := event.NameEvidence
	_, err := tx.Exec(ctx, `
		INSERT INTO domain_evidence (event_id,node_id,observed_at,ip,name,source,valid_from,valid_until)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING
	`, event.ID, nodeID, event.ObservedAt, evidence.IP, evidence.Name, evidence.Source, evidence.ValidFrom, evidence.ValidUntil)
	if err != nil {
		return fmt.Errorf("insert name evidence event %q: %w", event.ID, err)
	}
	return nil
}

func insertConnection(ctx context.Context, tx pgx.Tx, nodeID string, event model.Event, resolver GeoIPResolver) error {
	connection := event.Connection
	ownerID, ownerName := ownerIdentity(connection.Owner)
	if err := ensureObservedOwner(ctx, tx, nodeID, ownerID, ownerName, connection.Owner, event.ObservedAt); err != nil {
		return err
	}
	evidence, err := validEvidence(ctx, tx, nodeID, connection.Remote.IP, event.ObservedAt)
	if err != nil {
		return err
	}
	decision := attribution.DecideAt(*connection, evidence, event.ObservedAt)
	direction := attribution.ClassifyDirection(*connection, defaultLocalPrefixes)
	network := networkInfo(resolver, connection.Remote.IP)
	result, err := tx.Exec(ctx, `
		INSERT INTO connection_details
		(event_id,node_id,observed_at,direction,protocol,local_ip,local_port,remote_ip,remote_port,owner_id,owner_kind,owner_name,display_name,confidence,evidence_source,evidence_observed_at,bytes_sent,bytes_received,state,country_code,country_name,asn,organization,network_classification)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24)
		ON CONFLICT DO NOTHING
	`, event.ID, nodeID, event.ObservedAt, direction, connection.Protocol, connection.Local.IP, connection.Local.Port, connection.Remote.IP, connection.Remote.Port, ownerID, connection.Owner.Kind, ownerName, decision.DisplayName, decision.Confidence, decision.EvidenceSource, decision.EvidenceObservedAt, connection.BytesSent, connection.BytesReceived, connection.State, network.CountryCode, network.CountryName, network.ASN, network.Organization, network.Classification)
	if err != nil {
		return fmt.Errorf("insert connection event %q: %w", event.ID, err)
	}
	if result.RowsAffected() == 0 {
		return nil
	}
	bytes := saturatingBytes(connection.BytesSent, connection.BytesReceived)
	if _, err := tx.Exec(ctx, `
		INSERT INTO owner_minute (node_id,bucket,owner_id,owner_kind,owner_name,direction,bytes,connections)
		VALUES ($1,date_trunc('minute',$2::timestamptz),$3,$4,$5,$6,$7,1)
		ON CONFLICT (node_id,bucket,owner_id,direction) DO UPDATE SET bytes=owner_minute.bytes+EXCLUDED.bytes, connections=owner_minute.connections+1
	`, nodeID, event.ObservedAt, ownerID, connection.Owner.Kind, ownerName, direction, bytes); err != nil {
		return fmt.Errorf("aggregate connection owner event %q: %w", event.ID, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO domain_minute (node_id,bucket,direction,domain,confidence,bytes,connections,requests)
		VALUES ($1,date_trunc('minute',$2::timestamptz),$3,$4,$5,$6,1,0)
		ON CONFLICT (node_id,bucket,direction,domain,confidence) DO UPDATE SET bytes=domain_minute.bytes+EXCLUDED.bytes, connections=domain_minute.connections+1
	`, nodeID, event.ObservedAt, direction, decision.DisplayName, decision.Confidence, bytes); err != nil {
		return fmt.Errorf("aggregate connection domain event %q: %w", event.ID, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO flow_minute (node_id,bucket,direction,owner_id,owner_name,source,destination,domain,confidence,protocol,remote_port,country_code,country_name,asn,organization,network_classification,bytes,connections)
		VALUES ($1,date_trunc('minute',$2::timestamptz),$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,1)
		ON CONFLICT (node_id,bucket,direction,owner_id,source,destination,domain,confidence,protocol,remote_port)
		DO UPDATE SET bytes=flow_minute.bytes+EXCLUDED.bytes, connections=flow_minute.connections+1
	`, nodeID, event.ObservedAt, direction, ownerID, ownerName, connection.Local.IP, connection.Remote.IP, decision.DisplayName, decision.Confidence, connection.Protocol, connection.Remote.Port, network.CountryCode, network.CountryName, network.ASN, network.Organization, network.Classification, bytes); err != nil {
		return fmt.Errorf("aggregate connection flow event %q: %w", event.ID, err)
	}
	return nil
}

func insertProxyRequest(ctx context.Context, tx pgx.Tx, nodeID string, event model.Event, resolver GeoIPResolver) error {
	request := event.ProxyRequest
	upstreamOwner, observedPort, err := resolveUpstreamOwner(ctx, tx, nodeID, request.Upstream, event.ObservedAt)
	if err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `
		INSERT INTO proxy_request_details (event_id,node_id,observed_at,host,source_ip,method,status,bytes_sent,upstream,upstream_owner_id,duration_ms)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT DO NOTHING
	`, event.ID, nodeID, event.ObservedAt, request.Host, request.SourceIP, request.Method, request.Status, request.BytesSent, request.Upstream, upstreamOwner, request.DurationMS)
	if err != nil {
		return fmt.Errorf("insert proxy request event %q: %w", event.ID, err)
	}
	if result.RowsAffected() == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO domain_minute (node_id,bucket,direction,domain,confidence,bytes,connections,requests)
		VALUES ($1,date_trunc('minute',$2::timestamptz),'inbound',$3,'confirmed',$4,0,1)
		ON CONFLICT (node_id,bucket,direction,domain,confidence) DO UPDATE SET bytes=domain_minute.bytes+EXCLUDED.bytes, requests=domain_minute.requests+1
	`, nodeID, event.ObservedAt, request.Host, request.BytesSent); err != nil {
		return fmt.Errorf("aggregate proxy domain event %q: %w", event.ID, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO proxy_status_minute (node_id,bucket,host,status,bytes,requests)
		VALUES ($1,date_trunc('minute',$2::timestamptz),$3,$4,$5,1)
		ON CONFLICT (node_id,bucket,host,status) DO UPDATE SET bytes=proxy_status_minute.bytes+EXCLUDED.bytes, requests=proxy_status_minute.requests+1
	`, nodeID, event.ObservedAt, request.Host, request.Status, request.BytesSent); err != nil {
		return fmt.Errorf("aggregate proxy status event %q: %w", event.ID, err)
	}
	ownerID, ownerName := upstreamOwner, request.Upstream
	destination, remotePort := proxyDestination(request.Upstream)
	if remotePort == 0 {
		remotePort = observedPort
	}
	if ownerID != "" {
		var encodedPorts []byte
		if err := tx.QueryRow(ctx, `SELECT display_name,ports FROM owners WHERE node_id=$1 AND owner_id=$2`, nodeID, ownerID).Scan(&ownerName, &encodedPorts); err != nil {
			return fmt.Errorf("load proxy upstream owner %q: %w", ownerID, err)
		}
		if remotePort == 0 {
			var ports []uint16
			if err := json.Unmarshal(encodedPorts, &ports); err != nil {
				return fmt.Errorf("decode proxy upstream owner ports: %w", err)
			}
			if len(ports) == 1 {
				remotePort = int(ports[0])
			}
		}
	} else if request.Upstream != "" {
		ownerID = "upstream:" + request.Upstream
	} else {
		ownerID, ownerName = "unattributed", "未归属"
	}
	if destination == "" {
		destination = "unattributed"
	}
	network := networkInfo(resolver, request.SourceIP)
	if _, err := tx.Exec(ctx, `
		INSERT INTO flow_minute (node_id,bucket,direction,owner_id,owner_name,source,destination,domain,confidence,protocol,remote_port,country_code,country_name,asn,organization,network_classification,bytes,connections,requests)
		VALUES ($1,date_trunc('minute',$2::timestamptz),'inbound',$3,$4,$5,$6,$7,'confirmed','tcp',$8,$9,$10,$11,$12,$13,$14,0,1)
		ON CONFLICT (node_id,bucket,direction,owner_id,source,destination,domain,confidence,protocol,remote_port)
		DO UPDATE SET bytes=flow_minute.bytes+EXCLUDED.bytes, requests=flow_minute.requests+1
	`, nodeID, event.ObservedAt, ownerID, ownerName, request.SourceIP, destination, request.Host, remotePort, network.CountryCode, network.CountryName, network.ASN, network.Organization, network.Classification, request.BytesSent); err != nil {
		return fmt.Errorf("aggregate proxy flow event %q: %w", event.ID, err)
	}
	return nil
}

func proxyDestination(upstream string) (string, int) {
	host, portText, err := net.SplitHostPort(strings.TrimSpace(upstream))
	if err != nil {
		return strings.TrimSpace(upstream), 0
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return host, 0
	}
	return host, port
}

func insertHealth(ctx context.Context, tx pgx.Tx, nodeID string, event model.Event) error {
	health := event.Health
	if _, err := tx.Exec(ctx, `
		INSERT INTO collector_health
			(event_id, node_id, observed_at, collector, status, code, dropped_events, usage_percent, message)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT DO NOTHING
	`, event.ID, nodeID, event.ObservedAt, health.Collector, health.Status, health.Code, health.DroppedEvents, health.UsagePercent, health.Message); err != nil {
		return fmt.Errorf("insert health event %q: %w", event.ID, err)
	}
	if health.Collector == "docker" && health.Code == "collector_disabled" {
		if _, err := tx.Exec(ctx, `UPDATE owners SET running=false WHERE node_id=$1 AND kind='container'`, nodeID); err != nil {
			return fmt.Errorf("mark disabled Docker owners stopped: %w", err)
		}
	}
	return nil
}

func insertInterfaceDelta(ctx context.Context, tx pgx.Tx, nodeID string, event model.Event) error {
	result, err := tx.Exec(ctx, `
		INSERT INTO interface_deltas
			(event_id, node_id, observed_at, interface, direction, bytes, packets)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT DO NOTHING
	`, event.ID, nodeID, event.ObservedAt, event.Interface, event.Direction, event.Bytes, event.Packets)
	if err != nil {
		return fmt.Errorf("insert interface event %q: %w", event.ID, err)
	}
	if result.RowsAffected() == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO traffic_minute (node_id, bucket, direction, bytes, packets)
		VALUES ($1, date_trunc('minute', $2::timestamptz), $3, $4, $5)
		ON CONFLICT (node_id, bucket, direction) DO UPDATE
		SET bytes = traffic_minute.bytes + EXCLUDED.bytes,
		    packets = traffic_minute.packets + EXCLUDED.packets
	`, nodeID, event.ObservedAt, event.Direction, event.Bytes, event.Packets); err != nil {
		return fmt.Errorf("aggregate interface event %q: %w", event.ID, err)
	}
	return nil
}

var defaultLocalPrefixes = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("fc00::/7"),
}

func ownerIdentity(owner model.OwnerRef) (string, string) {
	switch owner.Kind {
	case model.OwnerContainer:
		name := owner.ContainerName
		if name == "" {
			name = owner.ContainerID
		}
		return "container:" + owner.ContainerID, name
	case model.OwnerProcess:
		name := owner.Process
		if name == "" {
			name = "process"
		}
		return fmt.Sprintf("process:%d:%s", owner.PID, name), name
	default:
		return "host", "Host"
	}
}

func ensureObservedOwner(ctx context.Context, tx pgx.Tx, nodeID, ownerID, ownerName string, owner model.OwnerRef, observedAt time.Time) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO owners (node_id,owner_id,kind,display_name,pid,container_id,running,first_seen_at,last_seen_at)
		VALUES ($1,$2,$3,$4,$5,$6,true,$7,$7)
		ON CONFLICT (node_id,owner_id) DO UPDATE SET display_name=EXCLUDED.display_name,last_seen_at=GREATEST(owners.last_seen_at,EXCLUDED.last_seen_at)
	`, nodeID, ownerID, owner.Kind, ownerName, nullablePID(owner), nullableString(owner.ContainerID), observedAt)
	if err != nil {
		return fmt.Errorf("upsert observed owner %q: %w", ownerID, err)
	}
	return nil
}

func validEvidence(ctx context.Context, tx pgx.Tx, nodeID, ip string, observedAt time.Time) ([]model.NameEvidence, error) {
	rows, err := tx.Query(ctx, `
		SELECT host(ip),name,source,valid_from,valid_until FROM domain_evidence
		WHERE node_id=$1 AND ip=$2::inet AND valid_from <= $3 AND valid_until > $3
	`, nodeID, ip, observedAt)
	if err != nil {
		return nil, fmt.Errorf("query domain evidence: %w", err)
	}
	defer rows.Close()
	var evidence []model.NameEvidence
	for rows.Next() {
		var item model.NameEvidence
		if err := rows.Scan(&item.IP, &item.Name, &item.Source, &item.ValidFrom, &item.ValidUntil); err != nil {
			return nil, fmt.Errorf("scan domain evidence: %w", err)
		}
		evidence = append(evidence, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate domain evidence: %w", err)
	}
	return evidence, nil
}

func resolveUpstreamOwner(ctx context.Context, tx pgx.Tx, nodeID, upstream string, observedAt time.Time) (string, int, error) {
	upstream = strings.TrimSpace(upstream)
	if upstream == "" {
		return "", 0, nil
	}
	var ownerID string
	err := tx.QueryRow(ctx, `
		SELECT owner_id FROM owners
		WHERE node_id=$1 AND kind='container' AND running=true AND (display_name=$2 OR container_id=$2)
		ORDER BY last_seen_at DESC LIMIT 1
	`, nodeID, upstream).Scan(&ownerID)
	if err == nil {
		return ownerID, 0, nil
	}
	if err != nil && err != pgx.ErrNoRows {
		return "", 0, fmt.Errorf("resolve proxy upstream owner by name: %w", err)
	}
	err = tx.QueryRow(ctx, `
		SELECT min(owner_id) FROM owners
		WHERE node_id=$1 AND kind='container' AND running=true
		  AND regexp_replace(display_name, '-[0-9]+$', '')=$2
		HAVING count(*)=1
	`, nodeID, upstream).Scan(&ownerID)
	if err == nil {
		return ownerID, 0, nil
	}
	if err != nil && err != pgx.ErrNoRows {
		return "", 0, fmt.Errorf("resolve proxy upstream owner by compose name: %w", err)
	}
	host, portText, err := net.SplitHostPort(upstream)
	if err != nil {
		if _, parseErr := netip.ParseAddr(upstream); parseErr != nil {
			return "", 0, nil
		}
		var observedPort int
		err = tx.QueryRow(ctx, `
			SELECT min(owner_id),min(local_port) FROM (
				SELECT DISTINCT owner_id,local_port FROM connection_details
				WHERE node_id=$1 AND owner_kind='process' AND local_ip=$2::inet AND local_port > 0
				  AND observed_at >= $3::timestamptz - interval '2 minutes'
				  AND observed_at <= $3::timestamptz + interval '2 minutes'
			) candidates
			HAVING count(DISTINCT owner_id)=1 AND count(DISTINCT local_port)=1
		`, nodeID, upstream, observedAt).Scan(&ownerID, &observedPort)
		if err == pgx.ErrNoRows {
			return "", 0, nil
		}
		if err != nil {
			return "", 0, fmt.Errorf("resolve proxy upstream process: %w", err)
		}
		return ownerID, observedPort, nil
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, nil
	}
	err = tx.QueryRow(ctx, `
		SELECT owner_id FROM owners
		WHERE node_id=$1 AND kind='container' AND running=true
		  AND addresses @> to_jsonb(ARRAY[$2::text])
		  AND ports @> to_jsonb(ARRAY[$3::int])
		ORDER BY last_seen_at DESC LIMIT 1
	`, nodeID, host, port).Scan(&ownerID)
	if err == pgx.ErrNoRows {
		err = tx.QueryRow(ctx, `
			SELECT owner_id FROM connection_details
			WHERE node_id=$1 AND owner_kind='process' AND local_ip=$2::inet AND local_port=$3
			  AND observed_at >= $4::timestamptz - interval '30 seconds'
			  AND observed_at <= $4::timestamptz + interval '30 seconds'
			ORDER BY abs(extract(epoch FROM observed_at-$4::timestamptz)),observed_at DESC
			LIMIT 1
		`, nodeID, host, port, observedAt).Scan(&ownerID)
		if err == pgx.ErrNoRows {
			return "", 0, nil
		}
		if err != nil {
			return "", 0, fmt.Errorf("resolve proxy upstream process by address: %w", err)
		}
		return ownerID, port, nil
	}
	if err != nil {
		return "", 0, fmt.Errorf("resolve proxy upstream owner: %w", err)
	}
	return ownerID, port, nil
}

func nullablePID(owner model.OwnerRef) any {
	if owner.PID <= 0 {
		return nil
	}
	return owner.PID
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableUint64(value uint64) any {
	if value == 0 || value > math.MaxInt64 {
		return nil
	}
	return int64(value)
}

func saturatingBytes(left, right int64) int64 {
	if right > math.MaxInt64-left {
		return math.MaxInt64
	}
	return left + right
}

func networkInfo(resolver GeoIPResolver, rawAddress string) geoip.NetworkInfo {
	if resolver == nil {
		return geoip.NetworkInfo{Classification: "unknown"}
	}
	address, err := netip.ParseAddr(rawAddress)
	if err != nil {
		return geoip.NetworkInfo{Classification: "unknown"}
	}
	return resolver.Lookup(address)
}
