package domain

import "context"

type ValidationResult struct {
	Passed  bool     `json:"passed"`
	Errors  []string `json:"errors,omitempty"`
}

type Validator interface {
	Name() string
	Validate(ctx context.Context, n *Notification) (*ValidationResult, error)
}
