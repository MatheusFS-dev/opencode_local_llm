# Local OpenCode and Ollama

Shell setup for OpenCode V2 and a local Ollama model on Ubuntu 24.04 under WSL2. You choose the model storage directory when starting the downloader.

## Requirements

Ubuntu 24.04 on WSL2, `curl`, `python3`, `dpkg`, `sudo`, and network access are required. Setup installs `zstd` before the Ollama installer. OpenCode V2 is installed by its official user-level installer, so Node.js and npm are not required. Persistent mode requires systemd in WSL. The downloader requires a terminal at least 80 columns wide.

## Install and choose a service mode

From the repository root:

```bash
./src/setup.sh
```

`setup.sh` installs or updates the Go toolchain, Ollama, and OpenCode V2. It stops on installation errors and reports the failing command. Go and `gofmt` are linked in `/usr/local/bin`, so the development checks work from later shells too. Setup also stops and disables Ollama's installer-created system service, leaving the project in on-demand mode for `run.sh` and `download.sh`. After downloading a model with `./src/download.sh`, you can use the systemd service instead:

```bash
./src/service.sh persistent
```

Switch back with `./src/service.sh ondemand`. Check the selected mode and port with `./src/service.sh status`. Persistent mode needs systemd as PID 1. In WSL, enable it in `/etc/wsl.conf` and restart the distribution:

```ini
[boot]
systemd=true
```

| Mode | What happens | Use it when |
| --- | --- | --- |
| On-demand | `run.sh` starts a private Ollama server immediately before OpenCode and stops it when OpenCode exits. The system Ollama service remains disabled. | You normally use the interactive `./src/run.sh` workflow, or WSL does not have systemd enabled. |
| Persistent | systemd keeps the Ollama server available at `127.0.0.1:11434` between OpenCode sessions. | You want to run `opencode` or `opencode run` directly, including a Codex or Claude-controlled local worker. Requires systemd. |

On-demand mode is the default after setup. In either mode, Ollama can unload an idle model, so persistent mode does not guarantee that model weights always occupy VRAM. No model directory is built into the scripts. Both modes bind to `127.0.0.1:11434`, use the saved 16K or 32K context selection, and disable Ollama cloud features. The downloader saves the selected base directory and the active model's storage directory. Persistent mode writes `/etc/systemd/system/ollama.service.d/local-models.conf` and restarts Ollama when the selected model changes.

## Download a model

```bash
./src/download.sh
```

The script first asks for an absolute **base directory**. On later runs, press Enter to keep the saved base. Each selected model gets its own Ollama storage root at `<base directory>/<model name>`. A colon in the model tag becomes `-` in the folder name. For example, choosing `/data/models` and `huihui_ai/qwen3.5-abliterated:4B` uses `/data/models/huihui_ai/qwen3.5-abliterated-4B`. Ollama stores its blobs and manifests inside that directory. Switching models changes the active storage root, so `run.sh` uses the last selected model.

The script installs the latest stable Go release from official metadata when needed, verifies its SHA-256 checksum, and builds the downloader. Select a model with arrow keys and press Enter. The left pane shows `src/models.json` entries. The right pane shows the selected model's guide-derived VRAM guidance and starting context, then updates the current pull status and byte progress. Press `q` to exit or cancel a pull. An API error exits the interface and prints the full request and error in the terminal. Only one pull runs at a time. The catalog offers:

| Model | Download size |
| --- | --- |
| `huihui_ai/qwen3.5-abliterated:4B` | about 3.3 GB |
| `qwen3.5:4b` | 3.3–4.0 GB |
| `huihui_ai/gemma-4-abliterated:12b` | 7.6 GB |
| `gemma4:12b` | 7.7–8.0 GB |
| `huihui_ai/qwen3.5-abliterated:9b` | 6.6 GB |
| `qwen3.5:9b` | 6.6–7.6 GB |

## GPU and context window

Ollama automatically selects a compatible GPU. While a model is generating a response, use these commands in another terminal to confirm the actual device and VRAM use:

```bash
nvidia-smi -L
ollama ps
nvidia-smi
```

`ollama ps` reports whether the model is fully on the GPU, such as `100% GPU`, or partly offloaded to CPU. `nvidia-smi` identifies the GPU index and the `ollama` process using it.

Setup asks you to choose a 16K or 32K context window and saves the selection for both on-demand and persistent mode. Run `./src/setup.sh` again to choose a different value. Larger context windows require more VRAM, so increase the setting gradually and verify the result with `ollama ps`.

## Run OpenCode

```bash
./src/run.sh /path/to/project
```

Omit the path to use the current directory. On-demand mode starts Ollama for the command and stops it when OpenCode exits. Persistent mode uses the configured service. If another local process occupies OpenCode's managed-service port, `run.sh` selects the next available loopback port through OpenCode's service configuration. This works on both Ubuntu and WSL. Inside OpenCode, use `/models` and select `ollama/huihui_ai/qwen3.5-abliterated:4B`.

> [!IMPORTANT]
> In on-demand mode, always start OpenCode with `./src/run.sh`. Running `opencode` directly does not start Ollama. If OpenCode retries with `ECONNREFUSED: Unable to connect`, no Ollama server is listening on `127.0.0.1:11434`; exit OpenCode and start it through `./src/run.sh`.

To run OpenCode directly, first switch to persistent mode and confirm that Ollama is listening:

```bash
./src/service.sh persistent
./src/service.sh status
```

The status output must show `Mode: persistent` and `Port: ollama`. Then, from your project directory, you can use:

```bash
opencode
opencode mini --model ollama/huihui_ai/qwen3.5-abliterated:4B
opencode run --model ollama/huihui_ai/qwen3.5-abliterated:4B "Summarize this project"
```

The first command opens the full-screen interface and lets you choose a model with `/models`. The second opens the minimal interactive interface with the model selected. The third sends a single prompt from the command line. If the installer did not add OpenCode to your `PATH`, use `$HOME/.opencode/bin/opencode` in place of `opencode`.

## Development checks

```bash
(cd src/downloader && go test ./... && go build -o /tmp/local-downloader-check .)
bash tests/*.sh
bash -n src/*.sh src/lib/*.sh tests/*.sh
shellcheck -x src/*.sh src/lib/*.sh tests/*.sh
```

No model weights are downloaded by these checks.

## References

- [Go downloads](https://go.dev/dl/)
- [OpenCode V2 installation](https://opencode.ai/v2/docs)
- [Ollama pull API](https://docs.ollama.com/api/pull)
