# Contributing to wc-harness

## Testing

Run every suite before committing. `pre-commit` runs the same scripts.

```sh
scripts/tests/gofmt
scripts/tests/lint
scripts/tests/go_test
scripts/tests/shell_parse
```

## Repo-specific Conventions

- Module path is `github.com/WywySenarios/wc-harness`.
- Keep it on the standard library.
- Never commit with `--no-verify`.
