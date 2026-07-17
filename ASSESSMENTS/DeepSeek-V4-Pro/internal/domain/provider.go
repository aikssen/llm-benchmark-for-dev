package domain

import "context"

type Provider interface {
	Name() Channel
	Send(ctx context.Context, recipient Recipient, message Message) error
}
