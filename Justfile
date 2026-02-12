# Default: show available commands
default:
    @just --list

# Build the binary
build:
    go build -o probe .

# Run all tests
test:
    go test -v ./...

# Run tests with race detector
test-race:
    go test -race -v ./...

# Run a specific test by name
test-one NAME:
    go test -v -run {{NAME}} ./...

# Build and run with arguments
run *ARGS:
    go run . {{ARGS}}

# Run against a target directory
run-on DIR *ARGS:
    go run . --dir {{DIR}} {{ARGS}}

# Run with verbose output
run-v *ARGS:
    go run . --verbose {{ARGS}}

# Run integration tests (requires Ollama)
test-integration:
    go test -tags integration -v -timeout 120s

# Clean build artifacts
clean:
    rm -f probe

# Check prerequisites (rg, ollama)
check:
    @which rg > /dev/null 2>&1 || (echo "Missing: ripgrep (rg)" && exit 1)
    @which ollama > /dev/null 2>&1 || (echo "Missing: ollama" && exit 1)
    @echo "All prerequisites found."

# Format code
fmt:
    go fmt ./...

# Vet code
vet:
    go vet ./...

# Lint + vet + test (CI-style)
ci: fmt vet test
