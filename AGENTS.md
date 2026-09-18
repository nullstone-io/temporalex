# temporalex — agent guide

## Releasing

This module is a library. It has no version tags, GitHub releases, or deploy pipeline.
Consumers pin it by Go pseudo-version (commit sha) in their `go.mod`, so a change is
"released" when it lands on `master` through a reviewed PR and each consumer bumps to that
commit:

```
GOFLAGS=-mod=mod go get github.com/nullstone-io/temporalex@<full sha of origin/master>
go mod tidy
go mod vendor
```

Consumers: `enigma`, `nullfire`.

Resolve the sha from git (`git rev-parse origin/master`), never with `@latest`: the Go module
proxy caches versions forever, so `@latest` can return a commit that is no longer on the
branch. Never commit directly to `master`.
