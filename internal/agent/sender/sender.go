package sender

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"flowlens/internal/model"
)

var ErrPermanent = errors.New("permanent delivery failure")

type SleepFunc func(context.Context, time.Duration) error

type Option func(*Sender)

type Sender struct {
	endpoint string
	token    string
	client   *http.Client
	backoff  []time.Duration
	sleep    SleepFunc
}

func WithHTTPClient(client *http.Client) Option {
	return func(sender *Sender) { sender.client = client }
}

func WithBackoff(backoff []time.Duration) Option {
	return func(sender *Sender) { sender.backoff = append([]time.Duration(nil), backoff...) }
}

func WithSleep(sleep SleepFunc) Option {
	return func(sender *Sender) { sender.sleep = sleep }
}

func New(endpoint, token string, options ...Option) (*Sender, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, errors.New("sender endpoint must be an absolute HTTP or HTTPS URL")
	}
	if token == "" {
		return nil, errors.New("sender token is required")
	}
	sender := &Sender{
		endpoint: endpoint,
		token:    token,
		client:   &http.Client{Timeout: 15 * time.Second},
		backoff:  []time.Duration{time.Second, 5 * time.Second, 30 * time.Second},
		sleep:    sleepContext,
	}
	for _, option := range options {
		option(sender)
	}
	if sender.client == nil || sender.sleep == nil {
		return nil, errors.New("sender requires an HTTP client and sleep function")
	}
	return sender, nil
}

func (s *Sender) Send(ctx context.Context, batch model.Batch) error {
	if err := batch.Validate(); err != nil {
		return fmt.Errorf("validate batch for delivery: %w", err)
	}
	body, err := encodeBatch(batch)
	if err != nil {
		return err
	}

	var lastErr error
	for attempt := 0; attempt <= len(s.backoff); attempt++ {
		lastErr = s.attempt(ctx, body)
		if lastErr == nil || errors.Is(lastErr, ErrPermanent) {
			return lastErr
		}
		if attempt == len(s.backoff) {
			break
		}
		if err := s.sleep(ctx, s.backoff[attempt]); err != nil {
			return err
		}
	}
	return fmt.Errorf("send batch after retries: %w", lastErr)
}

func (s *Sender) attempt(ctx context.Context, body []byte) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create batch request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+s.token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Content-Encoding", "gzip")
	request.Header.Set("User-Agent", "flowlens-agent/0.1")

	response, err := s.client.Do(request)
	if err != nil {
		return fmt.Errorf("deliver batch: %w", err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	_ = response.Body.Close()

	switch {
	case response.StatusCode == http.StatusOK || response.StatusCode == http.StatusAccepted:
		return nil
	case response.StatusCode >= 400 && response.StatusCode < 500:
		return fmt.Errorf("%w: server returned %d", ErrPermanent, response.StatusCode)
	default:
		return fmt.Errorf("server returned retryable status %d", response.StatusCode)
	}
}

func encodeBatch(batch model.Batch) ([]byte, error) {
	var body bytes.Buffer
	compressed := gzip.NewWriter(&body)
	if err := json.NewEncoder(compressed).Encode(batch); err != nil {
		_ = compressed.Close()
		return nil, fmt.Errorf("encode batch: %w", err)
	}
	if err := compressed.Close(); err != nil {
		return nil, fmt.Errorf("close batch gzip stream: %w", err)
	}
	return body.Bytes(), nil
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
