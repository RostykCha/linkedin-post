# Build Skill

Build the LinkedIn agent CLI and scheduler binaries.

## Usage

```
/build [cli|scheduler|all] [--clean]
```

## Commands

### Build Everything

```bash
cd c:\Users\rosti\linkedin-post
make build
```

Or manually:
```bash
go build -o bin/linkedin-agent.exe ./cmd/cli
go build -o bin/scheduler.exe ./cmd/scheduler
```

### Build CLI Only

```bash
make build-cli
```

Or:
```bash
go build -o bin/linkedin-agent.exe ./cmd/cli
```

### Build Scheduler Only

```bash
make build-scheduler
```

Or:
```bash
go build -o bin/scheduler.exe ./cmd/scheduler
```

### Clean Build

```bash
make clean && make build
```

## Docker Build

```bash
docker-compose build
```

## Verify Build

```bash
./bin/linkedin-agent --version
./bin/linkedin-agent --help
```

## Output Locations

- CLI: `bin/linkedin-agent.exe`
- Scheduler: `bin/scheduler.exe`

## Dependencies

Ensure Go 1.24+ is installed:
```bash
go version
```

Install dependencies:
```bash
go mod download
```

## Troubleshooting

- **Module errors**: Run `go mod tidy`
- **Build fails**: Check Go version matches `go.mod` (1.24.0)
- **Missing dependencies**: Run `go mod download`
