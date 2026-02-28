# RedisClaw 🦀

A coding agent with Redis-FS backed sandbox, inspired by OpenClaw/Pi.

## Features

- **Agent Loop**: Full tool-use agent loop with Claude
- **Redis-FS Storage**: Persistent filesystem backed by Redis
- **Sandboxed Execution**: Code runs in isolated Docker container
- **Interactive CLI**: Chat with the agent or use direct commands

## Quick Start

```bash
# Start the sandbox
cd ../sandbox && docker-compose up -d

# Install RedisClaw
pip install -e .

# Run interactive mode (requires ANTHROPIC_API_KEY)
export ANTHROPIC_API_KEY=your_key
redisclaw

# Or use direct commands (no API key needed)
redisclaw --ls /
redisclaw --run "echo hello"
redisclaw --read /some/file.txt
```

## Architecture

```
┌─────────────────┐     ┌─────────────────┐
│   RedisClaw     │     │   Redis + FS    │
│   Agent Loop    │────▶│   Module        │
└────────┬────────┘     └────────┬────────┘
         │                       │
         │ REST API              │ FUSE Mount
         ▼                       ▼
┌─────────────────────────────────────────┐
│              Sandbox Container          │
│  /workspace ◀── FUSE ──▶ Redis FS Key   │
│  Python, Node.js, Git, etc.             │
└─────────────────────────────────────────┘
```

## CLI Commands

In interactive mode:

- `/run <cmd>` - Run a shell command in sandbox
- `/read <path>` - Read a file
- `/ls [path]` - List files
- `/write <path>` - Write a file (prompts for content)
- `/clear` - Clear conversation history
- `/exit` - Exit

Or just type naturally to chat with the agent.

## Tools

The agent has access to:

- `run_command` - Execute shell commands in sandbox
- `read_file` - Read files from workspace
- `write_file` - Write files to workspace
- `list_files` - List directory contents
- `delete_file` - Delete files/directories

## Testing

```bash
# Install dev dependencies
pip install -e ".[dev]"

# Run E2E tests (sandbox must be running)
pytest tests/test_e2e.py -v
```

## Configuration

| Flag | Default | Description |
|------|---------|-------------|
| `--sandbox` | `http://localhost:8090` | Sandbox server URL |
| `--redis` | `redis://localhost:6380` | Redis URL |
| `--key` | `sandbox` | Redis FS key |
| `--model` | `claude-sonnet-4-20250514` | Claude model |

