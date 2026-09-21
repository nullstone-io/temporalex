# temporalex
This library creates ergonomic functions around Temporal

## Schedule

`Schedule` pairs a `Workflow` with a Temporal Schedule that starts it on a recurring spec.
It implements `Registrar`, so it goes in the worker's registration set *in place of* the workflow:
registering it registers the workflow and reconciles the schedule in Temporal. The schedule is
created when missing and updated otherwise, so its spec, action and policies always match the
deployed code. Operator state set through Temporal (paused, remaining actions, note) is preserved.

```go
var NightlySchedule = temporalex.Schedule[Config, NightlyInput, *NightlyResult]{
	ID:       "nightly",
	Workflow: Nightly,
	Input:    NightlyInput{},
	Spec:     client.ScheduleSpec{CronExpressions: []string{"0 9 * * *"}, Jitter: 10 * time.Minute},
	Policy:   client.SchedulePolicies{Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP},
	Client:   func(cfg Config) client.Client { return cfg.TemporalClient },
}
```

Leave `Client` nil in tests: `Register` then registers the workflow without calling Temporal.
`Register` panics if the schedule cannot be reconciled; a worker that cannot reach Temporal
cannot run anyway, and drift from the deployed definition should fail the deploy loudly.
