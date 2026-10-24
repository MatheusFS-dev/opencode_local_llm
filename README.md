# Local OpenCode and Ollama

Shell setup for OpenCode V2 and a local Ollama model on Ubuntu 24.04 under WSL2. You choose the model storage directory when starting the downloader.

## Requirements

Ubuntu 24.04 on WSL2, `curl`, `python3`, `dpkg`, `sudo`, and network access are required. Setup installs `zstd` before the Ollama installer. OpenCode V2 is installed by its official user-level installer, so Node.js and npm are not required. Persistent mode requires systemd in WSL. The downloader requires a terminal at least 80 columns wide.

## Install and choose a service mode

From the repository root:

```bash
./src/setup.sh
./src/service.sh ondemand
```

`setup.sh` installs or updates Ollama and OpenCode V2. It stops on installation errors and reports the failing command. After downloading a model with `./src/download.sh`, you can use the systemd service instead:

```bash
./src/service.sh persistent
```

Switch back with `./src/service.sh ondemand`. Check the selected mode and port with `./src/service.sh status`. Persistent mode needs systemd as PID 1. In WSL, enable it in `/etc/wsl.conf` and restart the distribution:

```ini
[boot]
systemd=true
```

No model directory is built into the scripts. Both modes bind to `127.0.0.1:11434`, set a 16,384 token starting context, and disable Ollama cloud features. The downloader saves the selected base directory and the active model's storage directory. Persistent mode writes `/etc/systemd/system/ollama.service.d/local-models.conf` and restarts Ollama when the selected model changes.

## Download a model

```bash
./src/download.sh
```

The script first asks for an absolute **base directory**. On later runs, press Enter to keep the saved base. Each selected model gets its own Ollama storage root at `<base directory>/<model name>`. A colon in the model tag becomes `-` in the folder name. For example, choosing `/data/models` and `huihui_ai/qwen3.5-abliterated:4B` uses `/data/models/huihui_ai/qwen3.5-abliterated-4B`. Ollama stores its blobs and manifests inside that directory. Switching models changes the active storage root, so `run.sh` uses the last selected model.

The script installs the latest stable Go release from official metadata when needed, verifies its SHA-256 checksum, and builds the downloader. Select a model with arrow keys and press Enter. The left pane shows `src/models.json` entries. The right pane updates the current pull status and byte progress. Press `q` to exit or cancel a pull. An API error exits the interface and prints the full request and error in the terminal. Only one pull runs at a time. The catalog currently offers `huihui_ai/qwen3.5-abliterated:4B`, listed at about 3.3 GB.

## Run OpenCode

```bash
./src/run.sh /path/to/project
```

Omit the path to use the current directory. On-demand mode starts Ollama for the command and stops it when OpenCode exits. Persistent mode uses the configured service. Inside OpenCode, use `/models` and select `ollama/huihui_ai/qwen3.5-abliterated:4B`.

You can also run OpenCode directly when the persistent Ollama service is running. From your project directory:

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
