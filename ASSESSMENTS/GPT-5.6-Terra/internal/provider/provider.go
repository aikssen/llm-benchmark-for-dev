package provider

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/example/notification-orchestrator/internal/domain"
)

var ErrProviderNotConfigured = errors.New("provider is not configured for channel")

type Payload struct {
	NotificationID string
	DeliveryID     string
	Recipient      string
	Channel        domain.Channel
	Message        string
}

// Provider isolates channel-specific external systems from orchestration.
type Provider interface {
	Send(ctx context.Context, payload Payload) error
}

type Registry struct {
	providers map[domain.Channel]Provider
}

func NewRegistry(providers map[domain.Channel]Provider) *Registry {
	copy := make(map[domain.Channel]Provider, len(providers))
	for channel, provider := range providers {
		copy[channel] = provider
	}
	return &Registry{providers: copy}
}

func (r *Registry) For(channel domain.Channel) (Provider, error) {
	provider, ok := r.providers[channel]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrProviderNotConfigured, channel)
	}
	return provider, nil
}

// Error marks errors which should not be retried (for example, an invalid destination
// rejected by a provider). All other provider errors are treated as transient.
type Error struct {
	Err       error
	Permanent bool
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

func Permanent(err error) error { return &Error{Err: err, Permanent: true} }

func IsPermanent(err error) bool {
	var providerError *Error
	return errors.As(err, &providerError) && providerError.Permanent
}

// LoggingProvider is a local deterministic stand-in. Real adapters implement the
// same interface and own provider-specific authentication and response mapping.
type LoggingProvider struct {
	name   string
	logger *log.Logger
}

func NewLoggingProvider(name string, logger *log.Logger) *LoggingProvider {
	return &LoggingProvider{name: name, logger: logger}
}

func (p *LoggingProvider) Send(_ context.Context, payload Payload) error {
	p.logger.Printf("provider=%s notification_id=%s delivery_id=%s recipient=%q delivered", p.name, payload.NotificationID, payload.DeliveryID, payload.Recipient)
	return nil
}
