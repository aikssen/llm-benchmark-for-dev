package domain

import "testing"

func TestParseChannel(t *testing.T) {
	cases := []struct {
		in      string
		want    Channel
		wantErr bool
	}{
		{"email", ChannelEmail, false},
		{"EMAIL", ChannelEmail, false},
		{"  sms  ", ChannelSMS, false},
		{"push", ChannelPush, false},
		{"", "", true},
		{"fax", "", true},
	}
	for _, c := range cases {
		got, err := ParseChannel(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseChannel(%q): expected error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseChannel(%q): unexpected error: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("ParseChannel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestValidateRecipient(t *testing.T) {
	cases := []struct {
		channel   Channel
		recipient string
		wantErr   bool
	}{
		{ChannelEmail, "user@example.com", false},
		{ChannelEmail, "no-at-sign", true},
		{ChannelEmail, "with space@example.com", true},
		{ChannelEmail, "", true},
		{ChannelSMS, "+14155551234", false},
		{ChannelSMS, "4155551234", true},
		{ChannelSMS, "+abc", true},
		{ChannelPush, "device-token-abc-123", false},
		{ChannelPush, "x", true},
	}
	for _, c := range cases {
		err := ValidateRecipient(c.channel, c.recipient)
		if (err != nil) != c.wantErr {
			t.Errorf("ValidateRecipient(%s, %q) err=%v, wantErr=%v", c.channel, c.recipient, err, c.wantErr)
		}
	}
}

func TestValidateMessage(t *testing.T) {
	if err := ValidateMessage(ChannelEmail, Message{Subject: "", Body: "hi"}); err == nil {
		t.Error("expected error for missing subject")
	}
	if err := ValidateMessage(ChannelEmail, Message{Subject: "Hi", Body: "hello"}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if err := ValidateMessage(ChannelSMS, Message{Body: ""}); err == nil {
		t.Error("expected error for empty body")
	}
	long := make([]byte, 1601)
	for i := range long {
		long[i] = 'a'
	}
	if err := ValidateMessage(ChannelSMS, Message{Body: string(long)}); err == nil {
		t.Error("expected error for oversize sms")
	}
}

func TestStatusTerminal(t *testing.T) {
	if !StatusDelivered.Terminal() {
		t.Error("delivered should be terminal")
	}
	if !StatusFailed.Terminal() {
		t.Error("failed should be terminal")
	}
	if !StatusRejected.Terminal() {
		t.Error("rejected should be terminal")
	}
	if StatusPending.Terminal() {
		t.Error("pending should not be terminal")
	}
}
