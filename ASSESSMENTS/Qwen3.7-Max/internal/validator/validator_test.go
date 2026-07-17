package validator

import (
	"context"
	"testing"

	"github.com/notifications-service/internal/domain"
)

func TestRecipientValidator_Email(t *testing.T) {
	v := NewRecipientValidator()
	ctx := context.Background()

	tests := []struct {
		name      string
		recipient string
		channel   domain.Channel
		wantErr   bool
	}{
		{"valid email", "user@example.com", domain.ChannelEmail, false},
		{"invalid email", "not-an-email", domain.ChannelEmail, true},
		{"empty email", "", domain.ChannelEmail, true},
		{"valid phone", "+1234567890", domain.ChannelSMS, false},
		{"invalid phone", "123", domain.ChannelSMS, true},
		{"valid device token", "abc123def456", domain.ChannelPush, false},
		{"invalid device token", "short", domain.ChannelPush, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &domain.NotificationRequest{Channel: tt.channel}
			err := v.Validate(ctx, tt.recipient, req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestContentValidator(t *testing.T) {
	v := NewContentValidator()
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *domain.NotificationRequest
		wantErr bool
	}{
		{
			"valid email with subject",
			&domain.NotificationRequest{
				Channel: domain.ChannelEmail,
				Subject: "Test",
				Message: "Hello",
			},
			false,
		},
		{
			"email without subject",
			&domain.NotificationRequest{
				Channel: domain.ChannelEmail,
				Message: "Hello",
			},
			true,
		},
		{
			"empty message",
			&domain.NotificationRequest{
				Channel: domain.ChannelSMS,
				Message: "",
			},
			true,
		},
		{
			"valid sms",
			&domain.NotificationRequest{
				Channel: domain.ChannelSMS,
				Message: "Hello",
			},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.Validate(ctx, "recipient", tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRateLimitValidator(t *testing.T) {
	v := NewRateLimitValidator(2, 1000000000)
	ctx := context.Background()
	req := &domain.NotificationRequest{Channel: domain.ChannelSMS}

	err1 := v.Validate(ctx, "user1", req)
	if err1 != nil {
		t.Errorf("first request should pass: %v", err1)
	}

	err2 := v.Validate(ctx, "user1", req)
	if err2 != nil {
		t.Errorf("second request should pass: %v", err2)
	}

	err3 := v.Validate(ctx, "user1", req)
	if err3 == nil {
		t.Error("third request should fail with rate limit")
	}

	err4 := v.Validate(ctx, "user2", req)
	if err4 != nil {
		t.Errorf("different user should not be rate limited: %v", err4)
	}
}

func TestPreferenceValidator(t *testing.T) {
	v := NewPreferenceValidator()
	ctx := context.Background()

	req := &domain.NotificationRequest{Channel: domain.ChannelEmail}

	err := v.Validate(ctx, "user1", req)
	if err != nil {
		t.Errorf("should pass when not opted out: %v", err)
	}

	v.SetOptOut("user1", domain.ChannelEmail, true)

	err = v.Validate(ctx, "user1", req)
	if err == nil {
		t.Error("should fail when opted out")
	}

	reqSMS := &domain.NotificationRequest{Channel: domain.ChannelSMS}
	err = v.Validate(ctx, "user1", reqSMS)
	if err != nil {
		t.Errorf("should pass for different channel: %v", err)
	}
}

func TestChain(t *testing.T) {
	chain := NewChain(
		NewRecipientValidator(),
		NewContentValidator(),
	)
	ctx := context.Background()

	req := &domain.NotificationRequest{
		Channel: domain.ChannelEmail,
		Subject: "Test",
		Message: "Hello",
	}

	err := chain.Validate(ctx, "user@example.com", req)
	if err != nil {
		t.Errorf("valid request should pass: %v", err)
	}

	invalidReq := &domain.NotificationRequest{
		Channel: domain.ChannelEmail,
		Message: "",
	}

	err = chain.Validate(ctx, "user@example.com", invalidReq)
	if err == nil {
		t.Error("invalid request should fail")
	}
}
