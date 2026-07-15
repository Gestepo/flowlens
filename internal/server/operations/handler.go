package operations

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

var ErrNotFound = errors.New("operation record not found")

type Node struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Status           string    `json:"status"`
	LastSeenAt       time.Time `json:"last_seen_at"`
	FailedCollectors []string  `json:"failed_collectors"`
}

type Delivery struct {
	ID             int64      `json:"id"`
	Status         string     `json:"status"`
	Attempt        int        `json:"attempt"`
	ResponseStatus *int       `json:"response_status"`
	LastError      string     `json:"last_error"`
	CreatedAt      time.Time  `json:"created_at"`
	DeliveredAt    *time.Time `json:"delivered_at"`
}

type Alert struct {
	ID              int64             `json:"id"`
	RuleID          string            `json:"rule_id"`
	Status          string            `json:"status"`
	Severity        string            `json:"severity"`
	NodeID          string            `json:"node_id"`
	OwnerID         *string           `json:"owner_id"`
	Title           string            `json:"title"`
	Evidence        map[string]string `json:"evidence"`
	TrafficFilter   map[string]string `json:"traffic_filter"`
	ObservedValue   float64           `json:"observed_value"`
	ComparisonValue *float64          `json:"comparison_value"`
	WindowSeconds   int               `json:"window_seconds"`
	FirstSeenAt     time.Time         `json:"first_seen_at"`
	LastSeenAt      time.Time         `json:"last_seen_at"`
	ResolvedAt      *time.Time        `json:"resolved_at"`
	OccurrenceCount int64             `json:"occurrence_count"`
	Deliveries      []Delivery        `json:"deliveries,omitempty"`
}

type AlertRule struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Enabled    bool    `json:"enabled"`
	Severity   string  `json:"severity"`
	Threshold  float64 `json:"threshold"`
	Multiplier float64 `json:"multiplier"`
}

type Settings struct {
	DetailRetentionDays      int         `json:"detail_retention_days"`
	AggregateRetentionMonths int         `json:"aggregate_retention_months"`
	AlertRules               []AlertRule `json:"alert_rules"`
}

type HealthResponse struct {
	Status                  string            `json:"status"`
	Nodes                   []Node            `json:"nodes"`
	DatabaseUsagePercent    float64           `json:"database_usage_percent"`
	WebhookTerminalFailures int               `json:"webhook_terminal_failures"`
	Jobs                    map[string]string `json:"jobs"`
}

type Service interface {
	Health(context.Context, time.Time) (HealthResponse, error)
	ListAlerts(context.Context, string, int, int) ([]Alert, error)
	GetAlert(context.Context, int64) (Alert, error)
	GetSettings(context.Context) (Settings, error)
	UpdateRetention(context.Context, int, int) error
	UpdateAlertRules(context.Context, []AlertRule) error
	ListNodes(context.Context, time.Time) ([]Node, error)
	RenameNode(context.Context, string, string) error
}

type Handler struct{ service Service }

func NewHandler(service Service) *Handler { return &Handler{service: service} }

func (handler *Handler) Health(w http.ResponseWriter, request *http.Request) {
	value, err := handler.service.Health(request.Context(), time.Now().UTC())
	writeResult(w, value, err)
}

func (handler *Handler) Alerts(w http.ResponseWriter, request *http.Request) {
	status := request.URL.Query().Get("status")
	if status == "" {
		status = "open"
	}
	if status != "open" && status != "resolved" && status != "all" {
		writeOperationError(w, http.StatusBadRequest, "invalid_status", "告警状态无效")
		return
	}
	limit, offset := queryPage(request)
	value, err := handler.service.ListAlerts(request.Context(), status, limit, offset)
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	writeOperationJSON(w, http.StatusOK, map[string]any{"items": value, "limit": limit, "offset": offset})
}

func (handler *Handler) Alert(w http.ResponseWriter, request *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(request, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeOperationError(w, http.StatusBadRequest, "invalid_alert", "告警编号无效")
		return
	}
	value, err := handler.service.GetAlert(request.Context(), id)
	writeResult(w, value, err)
}

func (handler *Handler) Settings(w http.ResponseWriter, request *http.Request) {
	value, err := handler.service.GetSettings(request.Context())
	writeResult(w, value, err)
}

func (handler *Handler) Retention(w http.ResponseWriter, request *http.Request) {
	var input struct {
		DetailDays      int `json:"detail_days"`
		AggregateMonths int `json:"aggregate_months"`
	}
	if !decodeOperationJSON(w, request, &input) {
		return
	}
	if input.DetailDays < 1 || input.DetailDays > 30 || input.AggregateMonths < 1 || input.AggregateMonths > 12 {
		writeOperationError(w, http.StatusBadRequest, "invalid_retention", "明细保留必须为 1–30 天，日汇总必须为 1–12 个月")
		return
	}
	if err := handler.service.UpdateRetention(request.Context(), input.DetailDays, input.AggregateMonths); err != nil {
		writeResult(w, nil, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *Handler) AlertSettings(w http.ResponseWriter, request *http.Request) {
	var input struct {
		Rules []AlertRule `json:"rules"`
	}
	if !decodeOperationJSON(w, request, &input) {
		return
	}
	for _, rule := range input.Rules {
		if rule.ID == "" || rule.Threshold < 0 || rule.Multiplier < 0 {
			writeOperationError(w, http.StatusBadRequest, "invalid_alert_rule", "告警阈值无效")
			return
		}
	}
	if err := handler.service.UpdateAlertRules(request.Context(), input.Rules); err != nil {
		writeResult(w, nil, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *Handler) Nodes(w http.ResponseWriter, request *http.Request) {
	value, err := handler.service.ListNodes(request.Context(), time.Now().UTC())
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	writeOperationJSON(w, http.StatusOK, map[string]any{"items": value})
}

func (handler *Handler) Node(w http.ResponseWriter, request *http.Request) {
	var input struct {
		Name string `json:"name"`
	}
	if !decodeOperationJSON(w, request, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 128 {
		writeOperationError(w, http.StatusBadRequest, "invalid_node_name", "节点名称不能为空且最多 128 个字符")
		return
	}
	if err := handler.service.RenameNode(request.Context(), chi.URLParam(request, "id"), input.Name); err != nil {
		writeResult(w, nil, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func TrafficFilter(evidence map[string]string) map[string]string {
	filter := make(map[string]string)
	if value := evidence["node_id"]; value != "" {
		filter["node"] = value
	}
	if value := evidence["owner_id"]; value != "" {
		filter["owner"] = value
	}
	if value := evidence["destination"]; value != "" {
		if net.ParseIP(value) != nil {
			filter["ip"] = value
		} else {
			filter["domain"] = value
		}
	}
	return filter
}

func queryPage(request *http.Request) (int, int) {
	limit, _ := strconv.Atoi(request.URL.Query().Get("limit"))
	if limit < 1 || limit > 200 {
		limit = 50
	}
	offset, _ := strconv.Atoi(request.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func decodeOperationJSON(w http.ResponseWriter, request *http.Request, target any) bool {
	request.Body = http.MaxBytesReader(w, request.Body, 256<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeOperationError(w, http.StatusBadRequest, "invalid_request", "请求格式无效")
		return false
	}
	return true
}

func writeResult(w http.ResponseWriter, value any, err error) {
	if errors.Is(err, ErrNotFound) {
		writeOperationError(w, http.StatusNotFound, "not_found", "记录不存在")
		return
	}
	if err != nil {
		writeOperationError(w, http.StatusInternalServerError, "internal_error", "操作失败")
		return
	}
	writeOperationJSON(w, http.StatusOK, value)
}

func writeOperationError(w http.ResponseWriter, status int, code, message string) {
	writeOperationJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func writeOperationJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
