package model

import (
	"fmt"
	"net/netip"
	"time"
)

type OwnerKind string

type DomainConfidence string

const (
	OwnerHost      OwnerKind = "host"
	OwnerProcess   OwnerKind = "process"
	OwnerContainer OwnerKind = "container"

	ConfidenceConfirmed DomainConfidence = "confirmed"
	ConfidenceInferred  DomainConfidence = "inferred"
	ConfidenceIPOnly    DomainConfidence = "ip_only"
)

type OwnerRef struct {
	Kind          OwnerKind `json:"kind"`
	PID           int       `json:"pid,omitempty"`
	Process       string    `json:"process,omitempty"`
	ContainerID   string    `json:"container_id,omitempty"`
	ContainerName string    `json:"container_name,omitempty"`
}

type OwnerInventory struct {
	Owner     OwnerRef `json:"owner"`
	CgroupID  uint64   `json:"cgroup_id,omitempty"`
	Addresses []string `json:"addresses,omitempty"`
	Ports     []uint16 `json:"ports,omitempty"`
	Running   bool     `json:"running"`
}

type Endpoint struct {
	IP         string           `json:"ip"`
	Port       uint16           `json:"port"`
	Domain     string           `json:"domain,omitempty"`
	Confidence DomainConfidence `json:"confidence"`
}

type ConnectionDelta struct {
	Protocol      string   `json:"protocol"`
	Local         Endpoint `json:"local"`
	Remote        Endpoint `json:"remote"`
	Owner         OwnerRef `json:"owner"`
	BytesSent     int64    `json:"bytes_sent"`
	BytesReceived int64    `json:"bytes_received"`
	State         string   `json:"state"`
}

type ProxyRequest struct {
	Host       string `json:"host"`
	SourceIP   string `json:"source_ip"`
	Method     string `json:"method"`
	Status     int    `json:"status"`
	BytesSent  int64  `json:"bytes_sent"`
	Upstream   string `json:"upstream"`
	DurationMS int64  `json:"duration_ms"`
}

type NameEvidence struct {
	IP         string    `json:"ip"`
	Name       string    `json:"name"`
	Source     string    `json:"source"`
	ValidFrom  time.Time `json:"valid_from"`
	ValidUntil time.Time `json:"valid_until"`
}

func (owner OwnerRef) validate() error {
	switch owner.Kind {
	case OwnerHost:
		return nil
	case OwnerProcess:
		if owner.PID <= 0 {
			return fmt.Errorf("process owner pid is required")
		}
		return nil
	case OwnerContainer:
		if owner.ContainerID == "" {
			return fmt.Errorf("container owner container_id is required")
		}
		return nil
	default:
		return fmt.Errorf("owner kind is invalid")
	}
}

func (endpoint Endpoint) validate(label string) error {
	if _, err := netip.ParseAddr(endpoint.IP); err != nil {
		return fmt.Errorf("%s ip is invalid", label)
	}
	if endpoint.Port == 0 {
		return fmt.Errorf("%s port must be within 1..65535", label)
	}
	if !endpoint.Confidence.valid() {
		return fmt.Errorf("%s confidence is invalid", label)
	}
	return nil
}

func (confidence DomainConfidence) valid() bool {
	switch confidence {
	case ConfidenceConfirmed, ConfidenceInferred, ConfidenceIPOnly:
		return true
	default:
		return false
	}
}

func (connection ConnectionDelta) validate() error {
	if connection.Protocol == "" {
		return fmt.Errorf("protocol is required")
	}
	if err := connection.Local.validate("local"); err != nil {
		return err
	}
	if err := connection.Remote.validate("remote"); err != nil {
		return err
	}
	if err := connection.Owner.validate(); err != nil {
		return err
	}
	if connection.BytesSent < 0 {
		return fmt.Errorf("bytes_sent must be non-negative")
	}
	if connection.BytesReceived < 0 {
		return fmt.Errorf("bytes_received must be non-negative")
	}
	return nil
}

func (request ProxyRequest) validate() error {
	if request.Host == "" || request.Method == "" || request.Upstream == "" {
		return fmt.Errorf("proxy request host, method, and upstream are required")
	}
	if _, err := netip.ParseAddr(request.SourceIP); err != nil {
		return fmt.Errorf("proxy request source_ip is invalid")
	}
	if request.Status < 100 || request.Status > 599 || request.BytesSent < 0 || request.DurationMS < 0 {
		return fmt.Errorf("proxy request counters are invalid")
	}
	return nil
}

func (evidence NameEvidence) validate() error {
	if _, err := netip.ParseAddr(evidence.IP); err != nil {
		return fmt.Errorf("name evidence ip is invalid")
	}
	if evidence.Name == "" || evidence.Source == "" || evidence.ValidFrom.IsZero() || !evidence.ValidUntil.After(evidence.ValidFrom) {
		return fmt.Errorf("name evidence is invalid")
	}
	return nil
}

func (inventory OwnerInventory) validate() error {
	if err := inventory.Owner.validate(); err != nil {
		return err
	}
	for _, address := range inventory.Addresses {
		if _, err := netip.ParseAddr(address); err != nil {
			return fmt.Errorf("owner inventory address is invalid")
		}
	}
	for _, port := range inventory.Ports {
		if port == 0 {
			return fmt.Errorf("owner inventory port must be within 1..65535")
		}
	}
	return nil
}
