package domain

import (
	"testing"
)

func TestChannelIsValid(t *testing.T) {
	tests := []struct {
		name    string
		channel Channel
		want    bool
	}{
		{"valid email", ChannelEmail, true},
		{"valid sms", ChannelSMS, true},
		{"valid push", ChannelPush, true},
		{"invalid channel", Channel("fax"), false},
		{"empty channel", Channel(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.channel.IsValid(); got != tt.want {
				t.Errorf("Channel.IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewNotificationValidate(t *testing.T) {
	tests := []struct {
		name    string
		input   NewNotification
		wantErr error
	}{
		{
			name: "valid notification",
			input: NewNotification{
				Channel:    ChannelEmail,
				Recipients: []Recipient{{ID: "user@test.com", Type: "email"}},
				Message:    Message{Body: "Hello"},
			},
			wantErr: nil,
		},
		{
			name: "empty recipients",
			input: NewNotification{
				Channel:    ChannelEmail,
				Recipients: []Recipient{},
				Message:    Message{Body: "Hello"},
			},
			wantErr: ErrEmptyRecipients,
		},
		{
			name: "empty message body",
			input: NewNotification{
				Channel:    ChannelEmail,
				Recipients: []Recipient{{ID: "user@test.com", Type: "email"}},
				Message:    Message{Body: ""},
			},
			wantErr: ErrEmptyMessage,
		},
		{
			name: "invalid channel",
			input: NewNotification{
				Channel:    Channel("invalid"),
				Recipients: []Recipient{{ID: "user@test.com", Type: "email"}},
				Message:    Message{Body: "Hello"},
			},
			wantErr: ErrInvalidChannel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if err != tt.wantErr {
				t.Errorf("NewNotification.Validate() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
