# Interactive Maestro

Start the normal terminal client in your project directory. Worker jobs and MCP file reads are scoped to that directory and its children. Reconnect from the same project directory:

```bash
codex --profile maestro
claude --agent maestro
```

Describe the coding task normally. Maestro reads the project, plans acceptance criteria, and calls its private `maestro_worker` MCP tools. Local OpenCode performs all implementation and test execution using the model and context window selected in `tb local-llm`.

`start_implementation` returns a persistent job ID immediately. `wait_for_job` waits up to 20 seconds and returns the recorded state. A running job requires another wait, even when output is quiet. Closing the client does not stop its detached worker. Keep the job ID to resume waiting in another Maestro session.

After `awaiting_review`, Maestro reads the actual artifacts and worker output. `review_job` records approval or starts a focused correction, with a maximum of three successful worker rounds. Failed, cancelled, or timed-out runs stop the request. Maestro reports the exact error and asks the user to resolve it, without implementing a fallback.

Maestro exposes file readers and the worker tools. Direct edits, shell commands, tests, and other agents are restricted by the native profile. Other profiles do not receive the worker server. This is a native client tool policy, not an operating-system isolation boundary against deliberate user overrides.

Antigravity remains read-only without a native worker MCP integration. Use Codex or Claude for this workflow.

On the first Codex Maestro session, approve the managed Maestro tool-policy hook in `/hooks` if Codex requests trust. The installer does not disable hook trust. The profile preapproves calls only to its private worker MCP server.

The MCP transport is `tb local-llm maestro-mcp`. It preserves JSON-RPC stdout without Toolbox banners. Job records and raw logs remain under `$XDG_STATE_HOME/opencode-local-llm/jobs` or `~/.local/state/opencode-local-llm/jobs`. Native jobs do not require Bubblewrap. Acceptance commands execute through the worker runner with a two-minute check limit.

The earlier `tb local-llm maestro --provider ...` controller remains available for existing jobs but is not the interactive workflow.
