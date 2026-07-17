package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"notification-service/internal/domain"
)

func TestParseChannel(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    domain.Channel
		wantErr bool
	}{
		{"email lower", "email", domain.ChannelEmail, false},
		{"email mixed", "EMAIL", domain.ChannelEmail, false},
		{"sms", "sms", domain.ChannelSMS, false},
		{"push", "push", domain.ChannelPush, false},
		{"empty", "", "", true},
		{"unknown", "fax", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := domain.ParseChannel(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, domain.ErrUnsupportedChannel)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
