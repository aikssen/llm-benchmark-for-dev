package validation

import (
	"context"
	"errors"
	"sync"

	"github.com/anomalyco/notification-service/internal/domain"
)

type Validator interface {
	Name() string
	Validate(ctx context.Context, n *domain.Notification) error
}

type Pipeline struct {
	validators []Validator
}

func NewPipeline(vals ...Validator) *Pipeline {
	return &Pipeline{validators: vals}
}

func (p *Pipeline) Add(v Validator) {
	p.validators = append(p.validators, v)
}

func (p *Pipeline) Run(ctx context.Context, n *domain.Notification) error {
	for _, v := range p.validators {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := v.Validate(ctx, n); err != nil {
			return errors.Join(errors.New(v.Name()+": "), err)
		}
	}
	return nil
}

func (p *Pipeline) RunParallel(ctx context.Context, n *domain.Notification) error {
	if len(p.validators) == 0 {
		return nil
	}
	if len(p.validators) == 1 {
		if err := p.validators[0].Validate(ctx, n); err != nil {
			return errors.Join(errors.New(p.validators[0].Name()+": "), err)
		}
		return nil
	}
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		first error
	)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for _, v := range p.validators {
		v := v
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := v.Validate(ctx, n)
			if err == nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if first == nil {
				first = errors.Join(errors.New(v.Name()+": "), err)
				cancel()
			}
		}()
	}
	wg.Wait()
	return first
}
