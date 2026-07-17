package providers_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"notification-service/internal/domain"
	"notification-service/internal/infrastructure/providers"
)

func TestRegistry(t *testing.T) {
	reg := make(providers.Registry)
	reg.Register(providers.NewEmailProvider())

	p, ok := reg.Get(domain.ChannelEmail)
	require.True(t, ok)
	assert.Equal(t, domain.ChannelEmail, p.Channel())

	_, ok = reg.Get(domain.ChannelPush)
	assert.False(t, ok)
}

func TestSimulatedProvidersEventuallySucceed(t *testing.T) {
	ctx := context.Background()
	rec := domain.Recipient{Address: "test@example.com"}
	content := domain.Content{Subject: "s", Body: "b"}

	email := providers.NewEmailProvider()
	err := email.Send(ctx, rec, content)
	assert.NoError(t, err)
}
