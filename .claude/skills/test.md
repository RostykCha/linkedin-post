# Test Skill

Run tests for the LinkedIn agent project.

## Usage

```
/test [--package=<pkg>] [--verbose] [--coverage]
```

## Commands

### Run All Tests

```bash
cd c:\Users\rosti\linkedin-post
make test
```

Or:
```bash
go test ./...
```

### Run Tests with Verbose Output

```bash
go test -v ./...
```

### Run Tests for Specific Package

```bash
go test -v ./internal/ai/...
go test -v ./internal/agent/...
go test -v ./internal/storage/...
```

### Run Tests with Coverage

```bash
go test -cover ./...
```

### Generate Coverage Report

```bash
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

### Run Tests with Race Detection

```bash
go test -race ./...
```

## Test Patterns

This project uses:
- Table-driven tests
- Interface mocking for unit tests
- `testify/assert` for assertions

Example test structure:
```go
func TestFunction(t *testing.T) {
    tests := []struct {
        name     string
        input    InputType
        expected OutputType
        wantErr  bool
    }{
        {"case 1", input1, expected1, false},
        {"case 2", input2, expected2, true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test implementation
        })
    }
}
```

## Troubleshooting

- **Tests timeout**: Add `-timeout 60s` flag
- **Race conditions**: Run with `-race` flag
- **Flaky tests**: Check for shared state or timing issues
