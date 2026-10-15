# Contributing

Keep analyzer-specific logic and filesystem IO outside the producer core.
Changes need tests for the behavior they introduce, including failure cases.
Run `gofmt`, `go test -race ./...`, `go vet ./...`, and `go mod tidy`.
Review the complete diff before opening a pull request. Use Conventional Commits.
