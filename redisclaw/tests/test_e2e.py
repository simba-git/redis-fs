"""
E2E tests for RedisClaw.

Requires sandbox docker-compose to be running:
    cd sandbox && docker-compose up -d

Run with:
    pytest tests/test_e2e.py -v
"""

import time
import pytest
import redis
import httpx

from redisclaw.tools import ToolExecutor, TOOLS


SANDBOX_URL = "http://localhost:8090"
REDIS_URL = "redis://localhost:6380"
FS_KEY = "sandbox"  # Must match the key used by sandbox docker-compose


@pytest.fixture
def tools():
    """Create a ToolExecutor for testing."""
    executor = ToolExecutor(SANDBOX_URL, REDIS_URL, FS_KEY)
    yield executor
    executor.close()


@pytest.fixture
def redis_client():
    """Create a Redis client for direct access."""
    client = redis.from_url(REDIS_URL)
    yield client
    client.close()


class TestSandboxConnection:
    """Test sandbox connectivity."""
    
    def test_health_check(self):
        """Sandbox health endpoint responds."""
        resp = httpx.get(f"{SANDBOX_URL}/health")
        assert resp.status_code == 200
        assert resp.json()["status"] == "ok"
    
    def test_run_simple_command(self, tools):
        """Can run a simple command."""
        result = tools.execute("run_command", {"command": "echo hello"})
        assert "hello" in result


class TestRedisConnection:
    """Test Redis-FS connectivity."""
    
    def test_ping(self, redis_client):
        """Redis responds to ping."""
        assert redis_client.ping()
    
    def test_fs_echo_and_cat(self, redis_client):
        """Can write and read via FS commands."""
        test_content = f"test-{time.time()}"
        redis_client.execute_command("FS.ECHO", FS_KEY, "/ping-test.txt", test_content)
        result = redis_client.execute_command("FS.CAT", FS_KEY, "/ping-test.txt")
        assert result.decode() == test_content


class TestWriteViaRedisReadInSandbox:
    """Test writing via Redis and reading in sandbox."""
    
    def test_text_file(self, tools, redis_client):
        """Write text file via Redis, read in sandbox."""
        content = f"Hello from Redis at {time.time()}"
        redis_client.execute_command("FS.ECHO", FS_KEY, "/redis-write.txt", content)
        
        result = tools.execute("run_command", {"command": "cat /workspace/redis-write.txt"})
        assert content in result
    
    def test_python_script(self, tools, redis_client):
        """Write Python script via Redis, execute in sandbox."""
        script = '''
print("Script executed!")
print(2 + 2)
'''
        redis_client.execute_command("FS.ECHO", FS_KEY, "/test_script.py", script)
        
        result = tools.execute("run_command", {"command": "python3 /workspace/test_script.py"})
        assert "Script executed!" in result
        assert "4" in result
    
    def test_json_file(self, tools, redis_client):
        """Write JSON via Redis, parse in sandbox."""
        import json
        data = {"name": "RedisClaw", "version": "1.0", "features": ["sandbox", "redis-fs"]}
        redis_client.execute_command("FS.ECHO", FS_KEY, "/config.json", json.dumps(data))
        
        result = tools.execute("run_command", {
            "command": "python3 -c \"import json; d=json.load(open('/workspace/config.json')); print(d['name'])\""
        })
        assert "RedisClaw" in result


class TestWriteInSandboxReadViaRedis:
    """Test writing in sandbox and reading via Redis."""
    
    def test_echo_to_file(self, tools, redis_client):
        """Echo to file in sandbox, read via Redis."""
        content = f"Sandbox write at {time.time()}"
        tools.execute("run_command", {
            "command": f"echo '{content}' > /workspace/sandbox-write.txt"
        })
        
        result = redis_client.execute_command("FS.CAT", FS_KEY, "/sandbox-write.txt")
        assert content in result.decode()
    
    def test_python_write(self, tools, redis_client):
        """Python write in sandbox, read via Redis."""
        tools.execute("run_command", {
            "command": '''python3 -c "
with open('/workspace/py-write.txt', 'w') as f:
    f.write('Written by Python')
"'''
        })
        
        result = redis_client.execute_command("FS.CAT", FS_KEY, "/py-write.txt")
        assert result.decode() == "Written by Python"


class TestToolExecutor:
    """Test the ToolExecutor directly."""
    
    def test_write_file(self, tools, redis_client):
        """write_file tool works."""
        result = tools.execute("write_file", {
            "path": "/tool-write.txt",
            "content": "Written via tool"
        })
        assert "Written" in result
        
        # Verify via Redis
        content = redis_client.execute_command("FS.CAT", FS_KEY, "/tool-write.txt")
        assert content.decode() == "Written via tool"
    
    def test_read_file(self, tools, redis_client):
        """read_file tool works."""
        redis_client.execute_command("FS.ECHO", FS_KEY, "/tool-read.txt", "Content to read")
        
        result = tools.execute("read_file", {"path": "/tool-read.txt"})
        assert result == "Content to read"
    
    def test_list_files(self, tools, redis_client):
        """list_files tool works."""
        redis_client.execute_command("FS.ECHO", FS_KEY, "/list-test/a.txt", "a")
        redis_client.execute_command("FS.ECHO", FS_KEY, "/list-test/b.txt", "b")
        
        result = tools.execute("list_files", {"path": "/list-test"})
        assert "a.txt" in result
        assert "b.txt" in result
    
    def test_delete_file(self, tools, redis_client):
        """delete_file tool works."""
        redis_client.execute_command("FS.ECHO", FS_KEY, "/to-delete.txt", "delete me")

        result = tools.execute("delete_file", {"path": "/to-delete.txt"})
        assert "Deleted" in result

        # Verify deleted - FS.CAT returns None for non-existent files
        cat_result = redis_client.execute_command("FS.CAT", FS_KEY, "/to-delete.txt")
        assert cat_result is None


class TestDirectoryOperations:
    """Test directory operations."""
    
    def test_mkdir_in_sandbox(self, tools, redis_client):
        """Create directory in sandbox, verify via Redis."""
        tools.execute("run_command", {
            "command": "mkdir -p /workspace/new-project/src"
        })
        tools.execute("run_command", {
            "command": "echo 'fn main() {}' > /workspace/new-project/src/main.rs"
        })
        
        result = redis_client.execute_command("FS.LS", FS_KEY, "/new-project/src")
        files = [f.decode() for f in result]
        assert "main.rs" in files
    
    def test_nested_write_via_redis(self, tools, redis_client):
        """Write to nested path via Redis (auto-creates parents)."""
        redis_client.execute_command(
            "FS.ECHO", FS_KEY, "/deep/nested/path/file.txt", "deep content"
        )
        
        result = tools.execute("run_command", {
            "command": "cat /workspace/deep/nested/path/file.txt"
        })
        assert "deep content" in result


class TestComplexWorkflows:
    """Test complex multi-step workflows."""
    
    def test_python_project_workflow(self, tools, redis_client):
        """Create and run a Python project."""
        # Write main.py via Redis
        main_py = '''
def greet(name):
    return f"Hello, {name}!"

if __name__ == "__main__":
    print(greet("RedisClaw"))
'''
        redis_client.execute_command("FS.ECHO", FS_KEY, "/pyproject/main.py", main_py)
        
        # Run in sandbox
        result = tools.execute("run_command", {
            "command": "cd /workspace/pyproject && python3 main.py"
        })
        assert "Hello, RedisClaw!" in result
    
    def test_file_processing_workflow(self, tools, redis_client):
        """Process a file: read, transform, write."""
        # Write input via Redis
        input_data = "line1\nline2\nline3"
        redis_client.execute_command("FS.ECHO", FS_KEY, "/process/input.txt", input_data)
        
        # Process in sandbox
        tools.execute("run_command", {
            "command": '''python3 -c "
with open('/workspace/process/input.txt') as f:
    lines = f.readlines()
with open('/workspace/process/output.txt', 'w') as f:
    for i, line in enumerate(lines, 1):
        f.write(f'{i}: {line}')
"'''
        })
        
        # Read output via Redis
        result = redis_client.execute_command("FS.CAT", FS_KEY, "/process/output.txt")
        assert "1: line1" in result.decode()
        assert "3: line3" in result.decode()

