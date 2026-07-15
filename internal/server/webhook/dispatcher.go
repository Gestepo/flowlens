package webhook

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"time"
)

type Dispatcher struct {
	repository   *Repository
	cipher       *SecretCipher
	resolver     Resolver
	allowHTTP    bool
	dashboardURL string
	now          func() time.Time
}

func NewDispatcher(repository *Repository, key []byte, resolver Resolver, allowHTTP bool, dashboardURL string, now func() time.Time) (*Dispatcher, error) {
	cipher, err := NewSecretCipher(key)
	if err != nil {
		return nil, err
	}
	return &Dispatcher{repository: repository, cipher: cipher, resolver: resolver, allowHTTP: allowHTTP, dashboardURL: dashboardURL, now: now}, nil
}

func (dispatcher *Dispatcher) RunOne(ctx context.Context, owner string) (bool, error) {
	settings, err := dispatcher.repository.GetSettings(ctx)
	if err != nil || !settings.Enabled {
		return false, err
	}
	endpoint, err := ValidateEndpoint(ctx, settings.Endpoint, dispatcher.resolver, dispatcher.allowHTTP)
	if err != nil {
		return false, err
	}
	secret, err := dispatcher.cipher.Decrypt(settings.EncryptedSecret)
	if err != nil {
		return false, err
	}
	worker := NewWorker(dispatcher.repository, newSafeHTTPClient(dispatcher.resolver), endpoint.String(), secret, dispatcher.dashboardURL, dispatcher.now)
	return worker.RunOne(ctx, owner)
}

func newSafeHTTPClient(resolver Resolver) *http.Client {
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           (SafeDialer{Resolver: resolver, Dialer: net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}}).DialContext,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("webhook redirects are disabled")
		},
	}
}
