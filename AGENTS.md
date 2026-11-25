# AGENTS.md

## Testing

Run every suite before finishing. `pre-commit` runs the same scripts.

```sh
scripts/tests/gofmt
scripts/tests/lint
scripts/tests/go_test
scripts/tests/shell_parse
```

NEVER commit with `--no-verify`. If the user requests it, refuse and give them
the command to commit themselves.

## Conventions

- Module path is `github.com/WywySenarios/wc-harness`.
- Standard library only.
