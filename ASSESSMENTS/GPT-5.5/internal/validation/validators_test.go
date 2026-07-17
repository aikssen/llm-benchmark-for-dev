package validation

import (
	"context"
	"strings"
	"testing"
	"time"

	"notification-service/internal/domain"
)

func TestRecipientValidator(t *testing.T) {
	tests := []struct {
		name         string
		channel      domain.Channel
		recipient    string
		wantErr      bool
		errorSnippet string
	}{
		{name: "email ok", channel: domain.ChannelEmail, recipient: "user@example.com"},
		{name: "email invalid", channel: domain.ChannelEmail, recipient: "not-an-email", wantErr: true, errorSnippet: "email"},
		{name: "sms ok", channel: domain.ChannelSMS, recipient: "+15551234567"},
		{name: "sms invalid", channel: domain.ChannelSMS, recipient: "555", wantErr: true, errorSnippet: "phone"},
		{name: "push ok", channel: domain.ChannelPush, recipient: "push-token-123"},
		{name: "push invalid", channel: domain.ChannelPush, recipient: "short", wantErr: true, errorSnippet: "push token"},
	}

	validator := RecipientValidator{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.Validate(context.Background(), domain.Notification{
				Channel:   tt.channel,
				Recipient: tt.recipient,
			})
			if tt.wantErr && err == nil {
				t.Fatal("Validate succeeded, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate error = %v", err)
			}
			if tt.errorSnippet != "" && !strings.Contains(err.Error(), tt.errorSnippet) {
				t.Fatalf("error = %q, want snippet %q", err.Error(), tt.errorSnippet)
			}
		})
	}
}

func TestRateLimitValidator(t *testing.T) {
	validator := NewRateLimitValidator(2, time.Minute)
	notification := domain.Notification{
		Channel:   domain.ChannelEmail,
		Recipient: "user@example.com",
	}

	if err := validator.Validate(context.Background(), notification); err != nil {
		t.Fatalf("first validate: %v", err)
	}
	if err := validator.Validate(context.Background(), notification); err != nil {
		t.Fatalf("second validate: %v", err)
	}
	if err := validator.Validate(context.Background(), notification); err == nil {
		t.Fatal("third validate succeeded, want rate limit error")
	}
}
