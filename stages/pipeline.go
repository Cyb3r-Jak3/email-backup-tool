package stages

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// Pipeline wires the three stages together. Each field holds one stage, and
// swapping an implementation in is the only change needed to back up from a
// different source, encrypt differently, or store somewhere else.
type Pipeline struct {
	// Fetch is stage 1.
	Fetch Fetcher
	// Transform is stage 2. It is never nil: a run without encryption uses the
	// pass-through transformer so the pipeline has no special case.
	Transform Transformer
	// Store is stage 3.
	Store Sink
	// Logger receives per-stage progress. Required.
	Logger *zap.Logger
}

// Result reports what a run did. Latest is the timestamp watermark to persist,
// so the next run can pick up where this one stopped.
type Result struct {
	// Messages is the number of messages stage 1 produced.
	Messages int
	// Artifacts is the number of blobs stage 3 wrote.
	Artifacts int
	// Bytes is the total size of the stored artifacts, after transformation.
	Bytes int64
	// Latest is the newest message date seen, zero if no message was stored.
	Latest time.Time
}

// Run drives one backup: stage 1 streams messages, and each is flattened into
// artifacts that pass through stage 2 and into stage 3 before the next message
// is fetched, so a large mailbox is never held in memory at once. Stage 3 is
// closed before Run returns, including on error.
func (p *Pipeline) Run(ctx context.Context, req FetchRequest) (Result, error) {
	var result Result
	if p.Fetch == nil || p.Transform == nil || p.Store == nil {
		return result, fmt.Errorf("pipeline is missing a stage")
	}
	fields := []zap.Field{
		zap.String("stage_1_fetch", p.Fetch.Name()),
		zap.String("stage_2_transform", p.Transform.Name()),
		zap.String("stage_3_store", p.Store.Name()),
		zap.Bool("all_messages", req.All()),
	}
	// A zero time logs as a meaningless year-1 date, so it is left out.
	if !req.All() {
		fields = append(fields, zap.Time("since", req.Since))
	}
	p.Logger.Info("Starting backup", fields...)

	fetchErr := p.Fetch.Fetch(ctx, req, func(msg *Message) error {
		for _, artifact := range msg.Artifacts() {
			transformed, err := p.Transform.Transform(ctx, artifact)
			if err != nil {
				return fmt.Errorf("stage 2 (%s) on %s: %w", p.Transform.Name(), artifact.Path, err)
			}
			if err := p.Store.Store(ctx, transformed); err != nil {
				return fmt.Errorf("stage 3 (%s) on %s: %w", p.Store.Name(), transformed.Path, err)
			}
			result.Artifacts++
			result.Bytes += int64(len(transformed.Data))
			p.Logger.Debug("Stored artifact",
				zap.String("path", transformed.Path),
				zap.Int("bytes", len(transformed.Data)),
			)
		}
		result.Messages++
		if msg.Date.After(result.Latest) {
			result.Latest = msg.Date
		}
		return nil
	})

	// Stage 3 is closed even on a fetch error so a partially written tar or
	// buffered upload is finalized rather than left dangling.
	closeErr := p.Store.Close()
	if fetchErr != nil {
		return result, fmt.Errorf("stage 1 (%s): %w", p.Fetch.Name(), fetchErr)
	}
	if closeErr != nil {
		return result, fmt.Errorf("stage 3 (%s): closing: %w", p.Store.Name(), closeErr)
	}

	done := []zap.Field{
		zap.Int("messages", result.Messages),
		zap.Int("artifacts", result.Artifacts),
		zap.Int64("bytes", result.Bytes),
	}
	if !result.Latest.IsZero() {
		done = append(done, zap.Time("latest_message", result.Latest))
	}
	p.Logger.Info("Backup complete", done...)
	return result, nil
}
