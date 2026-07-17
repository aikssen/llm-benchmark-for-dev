package validator_test

import (
	"context"
	"testing"
	"time"

	"github.com/notification-service/internal/domain"
	"github.com/notification-service/internal/validator"
)

func TestChannelValidator_ValidChannel(t *testing.T) {
	v := validator.NewChannelValidator()
	ctx := context.Background()

	req := &domain.CreateNotificationRequest{
		Channel: domain.ChannelEmail,
	}

	err := v.Validate(ctx, req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestChannelValidator_InvalidChannel(t *testing.T) {
	v := validator.NewChannelValidator()
	ctx := context.Background()

	req := &domain.CreateNotificationRequest{
		Channel: domain.Channel("invalid"),
	}

	err := v.Validate(ctx, req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRecipientValidator_EmptyRecipients(t *testing.T) {
	v := validator.NewRecipientValidator()
	ctx := context.Background()

	req := &domain.CreateNotificationRequest{
		Channel:    domain.ChannelEmail,
		Recipients: []domain.Recipient{},
	}

	err := v.Validate(ctx, req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRecipientValidator_ValidEmail(t *testing.T) {
	v := validator.NewRecipientValidator()
	ctx := context.Background()

	req := &domain.CreateNotificationRequest{
		Channel: domain.ChannelEmail,
		Recipients: []domain.Recipient{
			{Value: "test@example.com"},
		},
	}

	err := v.Validate(ctx, req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestRecipientValidator_InvalidEmail(t *testing.T) {
	v := validator.NewRecipientValidator()
	ctx := context.Background()

	req := &domain.CreateNotificationRequest{
		Channel: domain.ChannelEmail,
		Recipients: []domain.Recipient{
			{Value: "invalid-email"},
		},
	}

	err := v.Validate(ctx, req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRecipientValidator_ValidPhone(t *testing.T) {
	v := validator.NewRecipientValidator()
	ctx := context.Background()

	req := &domain.CreateNotificationRequest{
		Channel: domain.ChannelSMS,
		Recipients: []domain.Recipient{
			{Value: "+1234567890"},
		},
	}

	err := v.Validate(ctx, req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestRecipientValidator_InvalidPhone(t *testing.T) {
	v := validator.NewRecipientValidator()
	ctx := context.Background()

	req := &domain.CreateNotificationRequest{
		Channel: domain.ChannelSMS,
		Recipients: []domain.Recipient{
			{Value: "invalid-phone"},
		},
	}

	err := v.Validate(ctx, req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestContentValidator_EmptyBody(t *testing.T) {
	v := validator.NewContentValidator(10000)
	ctx := context.Background()

	req := &domain.CreateNotificationRequest{
		Channel: domain.ChannelEmail,
		Content: domain.Content{Body: ""},
	}

	err := v.Validate(ctx, req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestContentValidator_BodyTooLong(t *testing.T) {
	v := validator.NewContentValidator(10)
	ctx := context.Background()

	req := &domain.CreateNotificationRequest{
		Channel: domain.ChannelEmail,
		Content: domain.Content{Body: "this is a very long message that exceeds the limit"},
	}

	err := v.Validate(ctx, req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestContentValidator_EmailRequiresSubject(t *testing.T) {
	v := validator.NewContentValidator(10000)
	ctx := context.Background()

	req := &domain.CreateNotificationRequest{
		Channel: domain.ChannelEmail,
		Content: domain.Content{Body: "test body"},
	}

	err := v.Validate(ctx, req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestContentValidator_SMSDoesNotRequireSubject(t *testing.T) {
	v := validator.NewContentValidator(10000)
	ctx := context.Background()

	req := &domain.CreateNotificationRequest{
		Channel: domain.ChannelSMS,
		Content: domain.Content{Body: "test body"},
	}

	err := v.Validate(ctx, req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestRateLimiterValidator_AllowsWithinLimit(t *testing.T) {
	v := validator.NewRateLimiterValidator(5, time.Minute)
	ctx := context.Background()

	req := &domain.CreateNotificationRequest{
		Channel: domain.ChannelEmail,
		Recipients: []domain.Recipient{
			{Value: "test@example.com"},
		},
	}

	for i := 0; i < 5; i++ {
		err := v.Validate(ctx, req)
		if err != nil {
			t.Fatalf("expected no error on attempt %d, got %v", i+1, err)
		}
	}
}

func TestRateLimiterValidator_RejectsOverLimit(t *testing.T) {
	v := validator.NewRateLimiterValidator(2, time.Minute)
	ctx := context.Background()

	req := &domain.CreateNotificationRequest{
		Channel: domain.ChannelEmail,
		Recipients: []domain.Recipient{
			{Value: "test@example.com"},
		},
	}

	v.Validate(ctx, req)
	v.Validate(ctx, req)

	err := v.Validate(ctx, req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
