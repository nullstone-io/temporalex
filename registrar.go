package temporalex

import "go.temporal.io/sdk/worker"

type Registrar[TConfig any] interface {
	Register(cfg TConfig, registry worker.Registry)
}
