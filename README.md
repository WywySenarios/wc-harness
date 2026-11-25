# wc-harness

Runs one task from `<workspace>/PROMPT.md` through an OpenAI-compatible model and the tools staged in a `wc-tools` tree.

```sh
wc-harness -tools /path/to/tools /workspace
```

## Configuration (Required)

The workspace must contain `PROMPT.md`.

Env vars `WC_LLM_BASE_URL`, `WC_LLM_MODEL`, and `WC_LLM_API_KEY` must be populated.
