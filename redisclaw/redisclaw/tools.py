"""Tool definitions and execution for RedisClaw."""

from typing import Any
import httpx
import redis


# Tool definitions for Claude
TOOLS = [
    {
        "name": "run_command",
        "description": "Run a shell command in the sandbox. Use this to execute code, install packages, run tests, etc.",
        "input_schema": {
            "type": "object",
            "properties": {
                "command": {
                    "type": "string",
                    "description": "The shell command to execute"
                },
                "cwd": {
                    "type": "string",
                    "description": "Working directory (relative to /workspace)"
                },
                "timeout": {
                    "type": "integer",
                    "description": "Timeout in seconds (default: 300)"
                }
            },
            "required": ["command"]
        }
    },
    {
        "name": "read_file",
        "description": "Read the contents of a file from the workspace.",
        "input_schema": {
            "type": "object",
            "properties": {
                "path": {
                    "type": "string",
                    "description": "File path (relative to /workspace or absolute)"
                }
            },
            "required": ["path"]
        }
    },
    {
        "name": "write_file",
        "description": "Write content to a file in the workspace. Creates parent directories if needed.",
        "input_schema": {
            "type": "object",
            "properties": {
                "path": {
                    "type": "string",
                    "description": "File path (relative to /workspace or absolute)"
                },
                "content": {
                    "type": "string",
                    "description": "Content to write"
                }
            },
            "required": ["path", "content"]
        }
    },
    {
        "name": "list_files",
        "description": "List files and directories in a path.",
        "input_schema": {
            "type": "object",
            "properties": {
                "path": {
                    "type": "string",
                    "description": "Directory path (default: /)"
                }
            },
            "required": []
        }
    },
    {
        "name": "delete_file",
        "description": "Delete a file or directory from the workspace.",
        "input_schema": {
            "type": "object",
            "properties": {
                "path": {
                    "type": "string",
                    "description": "File or directory path to delete"
                }
            },
            "required": ["path"]
        }
    },
]


class ToolExecutor:
    """Executes tools against sandbox and Redis-FS."""
    
    def __init__(self, sandbox_url: str, redis_url: str, fs_key: str):
        self.sandbox_url = sandbox_url.rstrip("/")
        self.redis = redis.from_url(redis_url)
        self.fs_key = fs_key
        self.http = httpx.Client(timeout=300)
    
    def execute(self, name: str, args: dict[str, Any]) -> str:
        """Execute a tool and return the result."""
        try:
            if name == "run_command":
                return self._run_command(args)
            elif name == "read_file":
                return self._read_file(args)
            elif name == "write_file":
                return self._write_file(args)
            elif name == "list_files":
                return self._list_files(args)
            elif name == "delete_file":
                return self._delete_file(args)
            else:
                return f"Unknown tool: {name}"
        except Exception as e:
            return f"Error executing {name}: {e}"
    
    def _run_command(self, args: dict) -> str:
        """Run a command in the sandbox."""
        resp = self.http.post(
            f"{self.sandbox_url}/processes",
            json={
                "command": args["command"],
                "cwd": args.get("cwd", ""),
                "timeout_secs": args.get("timeout", 300),
                "wait": True,
            }
        )
        result = resp.json()
        
        output = result.get("stdout", "")
        if result.get("stderr"):
            output += f"\n[STDERR]\n{result['stderr']}"
        if result.get("exit_code", 0) != 0:
            output += f"\n[Exit code: {result['exit_code']}]"
        
        return output or "(no output)"
    
    def _read_file(self, args: dict) -> str:
        """Read a file via Redis-FS."""
        path = args["path"]
        if not path.startswith("/"):
            path = "/" + path
        
        result = self.redis.execute_command("FS.CAT", self.fs_key, path)
        if result is None:
            return f"File not found: {path}"
        return result.decode() if isinstance(result, bytes) else str(result)
    
    def _write_file(self, args: dict) -> str:
        """Write a file via Redis-FS."""
        path = args["path"]
        if not path.startswith("/"):
            path = "/" + path
        
        self.redis.execute_command("FS.ECHO", self.fs_key, path, args["content"])
        return f"Written {len(args['content'])} bytes to {path}"
    
    def _list_files(self, args: dict) -> str:
        """List files via Redis-FS."""
        path = args.get("path", "/")
        if not path.startswith("/"):
            path = "/" + path
        
        result = self.redis.execute_command("FS.LS", self.fs_key, path)
        if not result:
            return "(empty directory)"
        
        files = [f.decode() if isinstance(f, bytes) else str(f) for f in result]
        return "\n".join(files)
    
    def _delete_file(self, args: dict) -> str:
        """Delete a file via Redis-FS."""
        path = args["path"]
        if not path.startswith("/"):
            path = "/" + path
        
        self.redis.execute_command("FS.RM", self.fs_key, path)
        return f"Deleted {path}"
    
    def close(self):
        """Clean up resources."""
        self.http.close()
        self.redis.close()

