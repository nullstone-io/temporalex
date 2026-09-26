package temporalex

import "go.opentelemetry.io/otel"

const instrumentationName = "github.com/nullstone-io/temporalex"

var tracer = otel.Tracer(instrumentationName)
