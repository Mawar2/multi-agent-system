# Test Project Conventions

## Branch Naming

Branch pattern: `feature/KAI-{ticket}-{summary}`

## Commit Format

Commit format: `{ticket}_{description}`

## Testing

TDD required: true

Test command: `make test`

## Linting

Lint command: `make lint`

## Formatting

Format command: `gofmt -w .`

## Forbidden Files

FORBIDDEN filenames: utils.go, helpers.go, common.go, misc.go

These files are anti-patterns and should never be created.
