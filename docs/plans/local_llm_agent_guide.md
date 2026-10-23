# Local Coding Agents with OpenCode, Codex, and Claude Code

Updated: 2026-10-23. Commands follow current documentation. GPU/context estimates have not been benchmarked on your hardware.

## 1. Model ranking by GPU

Priority: coding-agent reliability, reasoning, fewer refusals, and *usable* context. `A` = community abliterated (reduced refusals), `S` = standard. Rankings are practical starting points, **not** benchmark-verified equivalence to GPT High. No listed local model should be assumed to match a frontier GPT at High reasoning on difficult, long-horizon tasks.

The **Download / installed** column approximates each model's persistent disk footprint after `ollama pull`, **not** VRAM usage. **Free disk to reserve** is a conservative pre-download allowance (about 30–50% above model size) for pulling and storage overhead. Values assume the listed default quantization, and shared Ollama blobs may reduce total usage when installing several models. Context ranges are conservative **starting targets to test**, not guaranteed measurements. Native advertised limits are much larger, usually 128K–256K. The KV cache, runtime, parallel requests, and GPU display usage consume additional VRAM.

### RTX 4090: 24 GB (best overall)

| Rank | Ollama model (paste into `ollama pull`) | Type | Download / installed | Free disk to reserve | Context to test | Best use |
|---|---|---|---|---|---|---|
| 1 | `huihui_ai/Qwen3.8-abliterated:27b` | A | 18 GB | 25 GB | 16–32K | Best first choice, coding + reasoning |
| 2 | `qwen3.8:27b` | S | 18 GB | 25 GB | 16–32K | Compare for stronger instruction following |
| 3 | `huihui_ai/Qwen3.6-abliterated:27b` | A | 17 GB | 24 GB | 16–32K | Alternative uncensored coding agent |
| 4 | `huihui_ai/glm-4.7-flash-abliterated` | A | 19 GB | 27 GB | 16–32K | Fast MoE coding alternative |
| 5 | `qwen3-coder:30b` | S | 19 GB | 27 GB | 16–32K | Code-focused tool execution |
| 6 | `huihui_ai/gpt-oss-abliterated:20b` | A | 14 GB | 20 GB | 32–64K | More headroom for context and reasoning |
| 7 | `gpt-oss:20b` | S | 14 GB | 20 GB | 32–64K | Standard reasoning baseline |

### RTX 4070 Ti: 12 GB

| Rank | Model | Type | Download / installed | Free disk to reserve | Context to test |
|---|---|---|---|---|---|
| 1 | `huihui_ai/gemma-4-abliterated:12b` | A | 7.6 GB | 11 GB | 16–32K |
| 2 | `gemma4:12b` | S | 7.7–8.0 GB | 12 GB | 16–32K |
| 3 | `huihui_ai/qwen3.5-abliterated:9b` | A | 6.6 GB | 10 GB | 16–32K |
| 4 | `qwen3.5:9b` | S | 6.6–7.6 GB | 11 GB | 16–32K |

### RTX 5070: 8 GB (as specified)

| Rank | Model | Type | Download / installed | Free disk to reserve | Context to test |
|---|---|---|---|---|---|
| 1 | `huihui_ai/qwen3.5-abliterated:4B` | A | 3.3 GB | 5 GB | 16–32K |
| 2 | `qwen3.5:4b` | S | 3.3–4.0 GB | 6 GB | 16–32K |
| 3 | `huihui_ai/qwen3.5-abliterated:9b` | A | 6.6 GB | 10 GB | 4–8K, tight fit |
| 4 | `qwen3.5:9b` | S | 6.6–7.6 GB | 11 GB | 4–8K, tight fit |

For meaningful autonomous coding on 8 GB, prefer a 4B model with enough context over a heavily memory-constrained 9B model. The 8 GB RTX 5070 configuration may refer to a laptop SKU. Verify with `nvidia-smi`.

*Abliteration reduces refusal behavior but does not eliminate all refusals or guarantee code quality. Compare with the standard counterpart before relying on it.*

Disk and VRAM are independent constraints. For several installed models, add their **Download / installed** sizes to estimate persistent storage. Budget separately for OpenCode, Ollama, your repositories, build dependencies, and test artifacts. On Linux, models are normally stored under `/usr/share/ollama/.ollama/models`; set `OLLAMA_MODELS` to move them to a larger SSD. Verify actual usage with `ollama list` and `du -sh /usr/share/ollama/.ollama/models` (default Linux installation).

## 2. Run a local agent in OpenCode (no cloud LLM)

These commands target **OpenCode V2**, Linux/macOS or Windows WSL, and an installed NVIDIA driver. Install [Ollama](https://ollama.com/download), then:

```bash
npm install -g @opencode/cli
ollama pull huihui_ai/Qwen3.8-abliterated:27b
opencode
```

Inside OpenCode, run `/models` and choose `ollama/huihui_ai/Qwen3.8-abliterated:27b`. OpenCode V2 automatically discovers the local Ollama server on `127.0.0.1:11434`. No paid API or custom JSON is required. To change GPU class, replace the model with one from the tables above.

For a single noninteractive implementation task:

```bash
opencode run --standalone --agent build \
  --model 'ollama/huihui_ai/Qwen3.8-abliterated:27b' \
  'Implement the requested feature, run relevant tests, and summarize changes.'
```

OpenCode provides file editing, shell commands, and tool integrations. Internet search may require enabling a separate web-search provider. **Local inference does not imply offline tools.**

### Actual context and GPU usage

1. Set Ollama's context-length slider to the suggested starting size (e.g., **32K** on 24 GB or **16K** on 12 GB). Alternatively, when starting Ollama manually: `OLLAMA_CONTEXT_LENGTH=32768 ollama serve` (stop the existing Ollama service first).
2. While the model is running, check `ollama ps`. Confirm `PROCESSOR` is **100% GPU** and `CONTEXT` matches the target.
3. Increase context incrementally only while it remains entirely on GPU. Ollama recommends **64K** for complex coding agents, but 64K may not fit alongside a large model. A nominal 256K context window does not mean 256K fits in VRAM.

## 3. Codex (commercial planner/reviewer, local OpenCode executor)

Install/authenticate [Codex CLI](https://developers.openai.com/codex) (`npm install -g @openai/codex@latest`). Place the controller rules in Codex's **global** instructions, *not* project `AGENTS.md`, which OpenCode also reads:

```bash
mkdir -p ~/.codex
cat >> ~/.codex/AGENTS.md <<'EOF'

## Local execution delegation
For coding tasks, act as the CLOUD planner and reviewer. Plan and specify acceptance tests.
Delegate implementation through your shell tool with:
  opencode run --standalone --agent build --model 'ollama/huihui_ai/Qwen3.8-abliterated:27b' 'DETAILED IMPLEMENTATION TASK AND ACCEPTANCE TESTS'
Replace the quoted task with the real task. Wait for the command to finish.
Review the actual `git diff` and test results, not merely the worker summary.
If needed, send focused corrections through a new OpenCode run, maximum 3 rounds.
Do not implement directly unless explicitly requested or the local worker fails.
EOF
codex
```

Choose your subscribed/API-backed Codex model with High reasoning. Give it a normal coding request. Codex plans, invokes the local process, waits, reviews, and can retry. Shell execution must be permitted in its sandbox.

## 4. Claude Code (commercial planner/reviewer, local OpenCode executor)

Install/authenticate [Claude Code](https://code.claude.com/docs/en/quickstart). In the repository root, create or append to **`CLAUDE.md`**, which OpenCode V2 does not automatically read:

```bash
cat >> CLAUDE.md <<'EOF'

## Local implementation worker
You are the CLOUD planner and reviewer. Plan the solution and test criteria.
Use your Bash tool to delegate code changes to:
  opencode run --standalone --agent build --model 'ollama/huihui_ai/Qwen3.8-abliterated:27b' 'DETAILED IMPLEMENTATION TASK AND ACCEPTANCE TESTS'
Replace the quoted task with the real task. Wait for completion.
Inspect `git diff`, independently verify tests and request local fixes if needed.
Limit worker retries to 3. Do not edit implementation files directly unless asked.
EOF
claude
```

Select your paid Claude model in Claude Code, then submit the request. **Do not use `ollama launch claude` for this hybrid arrangement**, because that makes Claude Code itself use the *local* model instead of Anthropic as planner.

## 5. Constraints and troubleshooting

- **Separate roles:** Codex/Claude calls remain billable. OpenCode worker inference uses your GPU. These are *shell-invoked workers*, not built-in cross-provider subagents or shared hidden reasoning.
- **Isolation:** Use a Git repository, preferably a disposable branch/worktree, and restrict shell/file permissions before unattended execution. Local abliterated models can make unsafe tool calls.
- **Avoid recursion:** Do not place cloud-only delegation rules in a project-level `AGENTS.md`, because OpenCode reads it. Claude-specific `CLAUDE.md` is safe with OpenCode V2.
- **Models not found:** Run `ollama list`, `opencode models`, and ensure the Ollama service is running. Confirm the exact `ollama/<model>` ID.
- **Out of memory/slow:** Lower context, choose a smaller model or quantization, close competing GPU workloads, then inspect `ollama ps` for CPU offloading.
- **Multiple GPUs:** Run separate Ollama instances/hosts for independent workers or select a card with `CUDA_VISIBLE_DEVICES` when starting the server. VRAM does not automatically pool into one fast 44 GB device.

## References

1. [OpenCode V2: install](https://opencode.ai/download), [models and automatic Ollama discovery](https://opencode.ai/v2/docs/models), [CLI `run`](https://opencode.ai/v2/docs/cli/commands), [project instructions](https://opencode.ai/v2/docs/instructions).
2. [Ollama: FAQ (model storage paths and `OLLAMA_MODELS`)](https://docs.ollama.com/faq), [context length and `ollama ps`](https://docs.ollama.com/context-length), [NVIDIA GPU selection](https://docs.ollama.com/gpu), [integration with coding tools](https://ollama.com/blog/launch).
3. [Ollama official models](https://ollama.com/library): [Qwen3.8](https://ollama.com/library/qwen3.8), [Qwen3.6](https://ollama.com/library/qwen3.6), [Qwen3.5](https://ollama.com/library/qwen3.5), [Gemma 4](https://ollama.com/library/gemma4), [GLM-4.7-Flash](https://ollama.com/library/glm-4.7-flash), [Qwen3-Coder](https://ollama.com/library/qwen3-coder), [gpt-oss](https://ollama.com/library/gpt-oss).
4. [Ollama Huihui abliterated models](https://ollama.com/huihui_ai): [Qwen3.8](https://ollama.com/huihui_ai/Qwen3.8-abliterated), [Qwen3.6](https://ollama.com/huihui_ai/Qwen3.6-abliterated), [Qwen3.5](https://ollama.com/huihui_ai/qwen3.5-abliterated), [Gemma 4](https://ollama.com/huihui_ai/gemma-4-abliterated), [GLM](https://ollama.com/huihui_ai/glm-4.7-flash-abliterated), [gpt-oss](https://ollama.com/huihui_ai/gpt-oss-abliterated).
5. [Codex AGENTS.md guidance](https://developers.openai.com/cookbook/examples/codex/iterating-development-workflows-with-codex), [Claude Code memory/CLAUDE.md](https://code.claude.com/docs/en/memory).
