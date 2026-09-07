package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Cyb3r-Jak3/email-backup-tool/stages"
)

// Multi is stage 3 fanning every artifact out to several backends, which is
// what a config listing more than one save destination produces. It is itself a
// stages.Sink, so the pipeline still sees exactly one stage 3.
type Multi struct {
	sinks []stages.Sink
}

// compile-time check that Multi satisfies stage 3.
var _ stages.Sink = (*Multi)(nil)

// NewMulti combines sinks. With a single sink that sink is returned unwrapped,
// so the common case keeps its own name in logs and errors.
func NewMulti(sinks ...stages.Sink) (stages.Sink, error) {
	switch len(sinks) {
	case 0:
		return nil, fmt.Errorf("save requires at least one of local, s3 or tar")
	case 1:
		return sinks[0], nil
	default:
		return &Multi{sinks: sinks}, nil
	}
}

// Name implements stages.Sink.
func (m *Multi) Name() string {
	names := make([]string, len(m.sinks))
	for i, sink := range m.sinks {
		names[i] = sink.Name()
	}
	return strings.Join(names, "+")
}

// Store implements stages.Sink, writing to every backend. The first failure
// stops the fan-out: a backup that only reached some of its destinations is a
// failed backup, and the run should not go on to record a new watermark.
func (m *Multi) Store(ctx context.Context, artifact *stages.Artifact) error {
	for _, sink := range m.sinks {
		if err := sink.Store(ctx, artifact); err != nil {
			return err
		}
	}
	return nil
}

// Close implements stages.Sink, closing every backend even if one fails so that
// no destination is left unfinalised.
func (m *Multi) Close() error {
	var errs []error
	for _, sink := range m.sinks {
		if err := sink.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
