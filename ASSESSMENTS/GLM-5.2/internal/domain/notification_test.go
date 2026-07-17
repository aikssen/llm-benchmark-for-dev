package domain

import "testing"

func TestAggregateStatus(t *testing.T) {
	cases := []struct {
		name    string
		results []RecipientResult
		want    Status
	}{
		{"empty", nil, StatusProcessing},
		{"all delivered", []RecipientResult{
			{RecipientID: "a", Status: StatusDelivered},
			{RecipientID: "b", Status: StatusDelivered},
		}, StatusDelivered},
		{"all failed", []RecipientResult{
			{RecipientID: "a", Status: StatusFailed},
			{RecipientID: "b", Status: StatusValidationFailed},
		}, StatusFailed},
		{"partial", []RecipientResult{
			{RecipientID: "a", Status: StatusDelivered},
			{RecipientID: "b", Status: StatusFailed},
		}, StatusPartial},
		{"in-flight", []RecipientResult{
			{RecipientID: "a", Status: StatusDelivered},
			{RecipientID: "b", Status: StatusSending},
		}, StatusProcessing},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := AggregateStatus(c.results); got != c.want {
				t.Fatalf("got %s, want %s", got, c.want)
			}
		})
	}
}

func TestStatusIsTerminal(t *testing.T) {
	terminal := []Status{StatusDelivered, StatusFailed, StatusValidationFailed, StatusPartial}
	for _, s := range terminal {
		if !s.IsTerminal() {
			t.Errorf("%s should be terminal", s)
		}
	}
	nonTerminal := []Status{StatusAccepted, StatusProcessing, StatusSending}
	for _, s := range nonTerminal {
		if s.IsTerminal() {
			t.Errorf("%s should not be terminal", s)
		}
	}
}

func TestValidationErrorText(t *testing.T) {
	ve := NewValidationError("bad", "email", "nope")
	if ve.Error() != "bad [email]: nope" {
		t.Fatalf("unexpected error text: %s", ve.Error())
	}
	ve2 := NewValidationError("bad", "", "nope")
	if ve2.Error() != "bad: nope" {
		t.Fatalf("unexpected error text: %s", ve2.Error())
	}
}
