package validation_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aikssen/notification-service/internal/domain"
	"github.com/aikssen/notification-service/internal/validation"
)

func TestContentValidator(t *testing.T) {
	v := validation.ContentValidator{}
	ctx := context.Background()

	t.Run("rejects empty body", func(t *testing.T) {
		err := v.Validate(ctx, domain.BatchRequest{Channel: domain.ChannelSMS, Message: domain.Message{Body: ""}})
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrValidation)
	})

	t.Run("requires subject for email", func(t *testing.T) {
		err := v.Validate(ctx, domain.BatchRequest{Channel: domain.ChannelEmail, Message: domain.Message{Body: "hi"}})
		require.Error(t, err)
	})

	t.Run("accepts valid sms", func(t *testing.T) {
		err := v.Validate(ctx, domain.BatchRequest{Channel: domain.ChannelSMS, Message: domain.Message{Body: "hi"}})
		assert.NoError(t, err)
	})
}

func TestFormatValidator(t *testing.T) {
	v := validation.FormatValidator{}
	ctx := context.Background()

	cases := []struct {
		name      string
		channel   domain.Channel
		recipient string
		wantErr   bool
	}{
		{"valid email", domain.ChannelEmail, "user@example.com", false},
		{"invalid email", domain.ChannelEmail, "not-an-email", true},
		{"valid e164 phone", domain.ChannelSMS, "+14155552671", false},
		{"invalid phone", domain.ChannelSMS, "123", true},
		{"valid push token", domain.ChannelPush, "abcdefgh12345678", false},
		{"short push token", domain.ChannelPush, "abc", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := v.Validate(ctx, tc.channel, tc.recipient)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestPreferenceValidator(t *testing.T) {
	store := validation.NewInMemoryPreferenceStore()
	store.OptOut("user@example.com", domain.ChannelEmail)
	v := validation.PreferenceValidator{Store: store}
	ctx := context.Background()

	err := v.Validate(ctx, domain.ChannelEmail, "user@example.com")
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrValidation)

	err = v.Validate(ctx, domain.ChannelSMS, "user@example.com")
	assert.NoError(t, err)
}

func TestRateLimitValidator(t *testing.T) {
	v := validation.NewRateLimitValidator(2)
	ctx := context.Background()

	assert.NoError(t, v.Validate(ctx, domain.ChannelSMS, "+14155552671"))
	assert.NoError(t, v.Validate(ctx, domain.ChannelSMS, "+14155552671"))
	err := v.Validate(ctx, domain.ChannelSMS, "+14155552671")
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrValidation)

	// A different recipient has its own independent budget.
	assert.NoError(t, v.Validate(ctx, domain.ChannelSMS, "+14155552672"))
}

// stubRequestValidator lets pipeline ordering tests control pass/fail per call.
type stubRequestValidator struct {
	name   string
	err    error
	called *bool
}

func (s stubRequestValidator) Name() string { return s.name }
func (s stubRequestValidator) Validate(context.Context, domain.BatchRequest) error {
	*s.called = true
	return s.err
}

type stubRecipientValidator struct {
	name   string
	err    error
	called *bool
}

func (s stubRecipientValidator) Name() string { return s.name }
func (s stubRecipientValidator) Validate(context.Context, domain.Channel, string) error {
	*s.called = true
	return s.err
}

func TestPipelineShortCircuitsRequestValidators(t *testing.T) {
	var firstCalled, secondCalled bool
	boom := errors.New("boom")
	pipeline := validation.NewPipeline(
		[]validation.RequestValidator{
			stubRequestValidator{name: "first", err: boom, called: &firstCalled},
			stubRequestValidator{name: "second", err: nil, called: &secondCalled},
		},
		nil,
	)

	err := pipeline.ValidateRequest(context.Background(), domain.BatchRequest{})
	require.ErrorIs(t, err, boom)
	assert.True(t, firstCalled)
	assert.False(t, secondCalled, "second validator must not run once the first fails")
}

func TestPipelineShortCircuitsRecipientValidators(t *testing.T) {
	var firstCalled, secondCalled bool
	boom := errors.New("boom")
	pipeline := validation.NewPipeline(
		nil,
		[]validation.RecipientValidator{
			stubRecipientValidator{name: "first", err: boom, called: &firstCalled},
			stubRecipientValidator{name: "second", err: nil, called: &secondCalled},
		},
	)

	err := pipeline.ValidateRecipient(context.Background(), domain.ChannelSMS, "+14155552671")
	require.ErrorIs(t, err, boom)
	assert.True(t, firstCalled)
	assert.False(t, secondCalled, "second validator must not run once the first fails")
}
