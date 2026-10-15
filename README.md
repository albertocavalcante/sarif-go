# sarif-go

A small SARIF 2.1.0 producer for linters and analyzers. It uses
`owenrumney/go-sarif/v3` for the format models and keeps analyzer execution and
filesystem IO outside the core.

```go
root, err := sarif.RootURI(checkoutRoot) // absolute native checkout path
if err != nil { return err }
report, err := sarif.Build(sarif.Tool{Name: "my-linter", RootURI: root},
    []sarif.Finding{{RuleID: "unused", Path: "src/main.go", Line: 2,
        Level: "warning", Message: "Unused declaration"}}, nil)
if err != nil { return err }
return sarif.Write(output, report)
```

Import `github.com/albertocavalcante/sarif-go` (package `sarif`). Core features:

- Repository-relative escaped URIs, deterministic results and rule indexes.
- Rule descriptions and help, related locations, suggested fixes and properties.
- Explicit execution failures as notifications and unsuccessful invocations.
- Source snapshots indexed by UTF-8 byte offset, exported as UTF-16 columns.
- A separate `goanalysis` adapter for native `analysis.Diagnostic` values.

Lines and columns are one-based; columns count UTF-16 code units. Region ends
are exclusive. Unknown columns and extents remain zero. Suggested edits require
complete regions; equal start and end means insertion. Overlapping edits in one
fix are rejected. Paths are relative to the configured checkout root; the core
does not inspect the filesystem or change the working directory. CLI adapters
must resolve user paths against their working directory before making them
relative to the checkout root.

`Build` does not run analyzers, upload reports, or decide whether findings fail
CI. Those policies belong to the caller. `Write` propagates IO errors. Call
`report.Validate()` when schema validation is needed, independently of writing.
Report identity uses each analyzer's stable rule IDs; no fingerprint is invented.

Go 1.26.2 or later. Development checks:

```sh
go test -race ./...
go vet ./...
go mod tidy
git diff --check
```
