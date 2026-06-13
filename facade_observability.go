package main

import (
	"context"

	apitypes "ben/desktop/api/types"
	"ben/desktop/internal/observability"
)

func startFacadeSpan(ctx context.Context, service, operation string, fields map[string]any) (context.Context, observability.Span) {
	ctx, span := observability.Start(ctx, "wails."+service+"."+operation,
		observability.String("service", service),
		observability.String("component", "wails"),
	)
	if len(fields) > 0 {
		span.SetInput(apitypes.TraceSummary{
			Summary:  "wails request",
			Fields:   fields,
			Redacted: true,
		})
	}
	return ctx, span
}

func finishFacadeSpan[T any](span observability.Span, value T, err error, fields map[string]any) (T, error) {
	if err != nil {
		span.RecordError(err)
		return value, err
	}
	if len(fields) > 0 {
		span.SetOutput(apitypes.TraceSummary{
			Summary: "wails response",
			Fields:  fields,
		})
	}
	return value, nil
}
