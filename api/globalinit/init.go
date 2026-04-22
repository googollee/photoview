package globalinit

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

type initer struct {
	init      func(context.Context) error
	terminate func(context.Context)
}

var initers = make(map[string]initer)

func Register(name string, init func(context.Context) error, terminate func(context.Context)) {
	initers[name] = initer{
		init:      init,
		terminate: terminate,
	}
}

func Initialize(ctx context.Context) (func(context.Context), error) {
	var errs []error
	var terminaters []func(context.Context)

	for name, initer := range initers {
		init := initer.init
		if init != nil {
			if err := init(ctx); err != nil {
				errs = append(errs, fmt.Errorf("%q init error: %w", name, err))
			}
		}

		if initer.terminate != nil {
			terminaters = append(terminaters, initer.terminate)
		}
	}

	slices.Reverse(terminaters)

	return func(ctx context.Context) {
		for _, term := range terminaters {
			term(ctx)
		}
	}, errors.Join(errs...)
}
