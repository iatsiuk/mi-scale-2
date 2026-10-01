# miscale Project Instructions

macOS CLI that reproduces the Xiaomi Mi Body Composition Scale 2 (XMTZC05HM) features of Zepp Life 6.16.1 (`com.xiaomi.hm.health`). Behavior must match the app; the decompiled app is the specification.

## Project Files

- `docs/analysis/ble-protocol.md` - BLE protocol: advertisement layout, GATT uuids, control commands, history transfer
- `docs/analysis/body-composition.md`, `docs/analysis/body-composition-codex.md` - body composition algorithm, two independent analyses of `libBodyfat.so` and the java layer
- `docs/analysis/app-logic.md` - app logic: live state machine, user matching, history import, merge, guest, infant, balance
- `docs/body-composition-validity.md` - scientific assessment of the computed values with references; update its tables when the formulas or the example vectors change
- `tools/Dockerfile.jadx` - jadx image; decompile with `docker run --rm -e JAVA_OPTS=-Xmx6g -v "$PWD":/work jadx:local -r -j 3 -d /work/decompiled2 /work/apk/zepplife-6.16.1.apk` (8 threads run out of memory)
- `tools/bodyfat-oracle/` - Unicorn harness that executes the real arm64 `libBodyfat.so` in Docker and writes the reference vectors
- Not in git: `docs/analysis/` and `tools/` (the files above, missing in a fresh clone), `apk/` (base APK, XAPK, `native/lib/arm64-v8a`), `decompiled2/sources` (jadx output, obfuscated; `compiled from:` comments give original file names), `res-out/` (decoded resources and single-class decompilations)

## Package Structure

- `internal/ble` - synchronous CoreBluetooth wrapper over `github.com/tinygo-org/cbgo` (darwin only): `Open(ctx)` waits for power on; `Adapter.Scan(ctx, cb)` reports duplicates, no callback runs after it returns; `Adapter.Connect(ctx, id)` discovers everything; `Device.Read/Write/Subscribe/Unsubscribe` serialize one GATT request at a time; a timed out request closes the connection because a late callback could complete the next request
- `internal/scale` - wire protocol and procedures: `ParseMeasurement` (13-byte 0x181B service data, 0x2A9C, history record), `EncodeTime/DecodeTime` (0x2A2B, UTC), control commands `Cmd*` and `ParseResponse` on 0x1542, `ParsePnP` (product id 25 = this scale), `Session` over the `GATT` interface: `Init` (mode check, clock), `DeviceInfo`, `History` (valid records and the raw count that decides the ack) then `AckHistory`, `EraseHistory`, `SetUnit`, `SetPartMeasure`, `OneFoot`, `Live`
- `internal/bodycomp` - port of source 101/102 analysis: `Compute(Input) (Result, error)`; `native.go` mirrors `libBodyfat.so` in float64, the rest mirrors java in float32; levels (`FatLevel`, `MuscleLevel`, ...), `BMRStandard`, `AgeAt`, `Trunc`; non-chinese locale only
- `internal/app` - app logic: `LiveTracker` (HMWeightingActivity state machine, emits `Event`), `Match`/`Candidates` (< 3 kg from the last weight), `SaveMeasurement` (30 s merge, empty user id keeps the record unassigned), `ImportHistory` (matching against data before the batch, idempotent merge, unassigned records instead of drops); a record dropped by a merge gets its timestamp ignored so a replayed sync cannot restore it; `Assign`, `BabyWeight`, `BalanceLevel`, `FormatWeight`
- `internal/store` - JSON data file (`~/Library/Application Support/miscale/data.json`): `User`, `Record`, `Data` (`Add`, `Remove`, `RemoveUser`, permanent `Ignore`/`IsIgnored` list), atomic `Save`
- `cmd/miscale` - cobra CLI; global flags `-f/--format` (markdown default, json) and `--data` (env `MISCALE_DATA`); results go to stdout through `env.render(view)`, progress and prompts to stderr through `env.printf`; prompts only when stdin is a TTY (`stdinIsTTY`); without a TTY nothing is discarded or ignored, only an explicit answer does that

## Porting Rules

- Port from the decompiled code, cite `file:line` in docs and comments where behavior is surprising; keep app quirks (inverted score interpolation, bone table gaps, month-only age, BMI sentinel -1 clamped to 10)
- Native formulas: float64, every `fmadd`/`fmsub`/`fnmsub` of the disassembly is `math.FMA`, wrap other products in `float64()` so the compiler cannot fuse them
- Java formulas: float32, wrap every operation in `float32()`; `(int)` casts truncate
- Java decimal helpers work on `String.valueOf(float)`: use `dec`, `t2`, `truncDec` in `internal/bodycomp/decimal.go`, never round binary values for them; `new BigDecimal(float)` rounds the exact binary value (`scale.RoundHalfUp`)
- `internal/bodycomp/testdata/*.csv.gz` are outputs of the real library; never edit them to make a test pass; a mismatch is a porting bug
- Use Docker for reverse-engineering tools; do not install them on the host

## Hardware Facts (verified on XMTZC05HM)

- PnP 0x2A50 `01 5701 1900 0001`: product id 25, source 102; firmware V1.0.0.12
- The scale clock runs in UTC; a weighing keeps the timestamp of its first packet
- History is kept per uid: a new uid sees only weighings made after its first `01 uid`; `04 uid` deletes the acknowledged records
- The scale sleeps; every GATT command needs the user to step on it first

## Code Style

### Imports

Group imports in order, separated by blank lines: standard library, external packages, local packages (`miscale/...`).

### Naming

- Package names: short, lowercase, no underscores
- Receivers: short, 1-2 letters
- Errors: `Err` prefix for sentinel errors (`ErrNoResponse`)

### Functions

- Max 80 lines, 50 statements (`funlen`), cyclomatic complexity 10 (`cyclop`), nesting 5 (`nestif`)
- Early returns for error handling

### Error Handling

- Wrap errors with context: `fmt.Errorf("operation: %w", err)`
- Check all errors (`errcheck`); use `errors.Is`/`errors.As`

### Comments

- Only for non-obvious logic; English, lowercase, brief

### Structs

- JSON tags on exported fields of persisted and printed structs: `json:"field_name"`, `omitempty` for optional fields

### Concurrency

- `context.Context` as first parameter
- cbgo delegate callbacks run on the CoreBluetooth queue: never block in them, hand data over through buffered channels

## Testing

- Prefer TDD: write the failing test first
- Use stdlib `testing` only (no testify), table-driven tests, run with `-race`
- Protocol procedures are tested through `scale.Session` with the fake scale in `internal/scale/session_test.go`
- CLI tests run the root command with a temp `--data` file (`runCLI`/`mustRun` in `cmd/miscale/root_test.go`) and check both output formats
- `internal/ble` and BLE commands have no unit tests; verify them on the scale with `miscale scan`, `miscale scale info` and `miscale sync --no-ack`

## Language

All code, comments and documentation in English, except `docs/analysis/*.md`, which are in Russian.

## Building

- Always build with `make build` (runs the linter); it embeds `cmd/miscale/Info.plist` with the Bluetooth usage description for TCC (a plain `go build` binary works only where the launching terminal already has Bluetooth access)
- `make test` runs tests with race detector and coverage
- Builds are darwin only: CoreBluetooth needs cgo

## Linting and Formatting

- `golangci-lint run` (part of `make build`), config in `.golangci.yml`
- Format with `gofmt -w` or `goimports -w`
