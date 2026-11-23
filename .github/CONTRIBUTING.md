# Contributing to Adamant

Thanks for your interest in contributing to **Adamant**! Whether you are fixing a vanilla parity gap, optimizing a hot path, or extending the external `api/` SDK, contributions are very welcome.

## Development setup

Requires **Go 1.27+** (`toolchain go1.27.3`).

```shell
git clone https://github.com/dm-vev/adamant
cd adamant
go build ./...
go test ./internal/archtest   # fast architecture, boundary & API ratchets
```

Before opening a pull request, verify:

1. `go vet ./...` and `(cd net && go vet ./...)` pass cleanly.
2. `go test ./internal/archtest` passes (enforces R1–R5 core/adapter boundaries, function length <= 100 lines, and intentional API diffs).
3. `make lint` introduces no new `golangci-lint` findings on changed files.
4. Package tests for the subsystem you touched pass (`-race` where applicable).

## Vanilla parity guidelines

- **Reference first.** Gameplay, world generation, mob AI, and command changes should cite the corresponding **Java Edition 26.3** method/behaviour or **Bedrock Dedicated Server 1.26.52.3** capture (see [`vanilla-reference.json`](../vanilla-reference.json)).
- **Regression tests.** Every bug or parity fix should include a focused, deterministic test that fails without the fix. Never generate expected golden values from the implementation under test.
- **Honest verification notes.** If you add a new parity verification harness or close a documented gap, update [`docs/verification.md`](../docs/verification.md).

## Architecture boundaries

- Keep `server/` free of protocol specifics (`R1`); wire encoding and version translation belong in `protocol/bedrock` and `net/`.
- Keep new hand-written functions concise (`<= 100` lines).
- If a feature can live cleanly in the external plugin layer, expose composable hooks and build helpers in [`api/`](../api) or [`api/bedrock/`](../api/bedrock) rather than coupling it into core types.
