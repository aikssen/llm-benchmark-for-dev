package provider

import "errors"

// Simulated errors used by the Simulator provider.
var (
	ErrSimulatedTransient = errors.New("simulated transient provider error")
	ErrSimulatedTimeout   = errors.New("simulated provider timeout")
)
