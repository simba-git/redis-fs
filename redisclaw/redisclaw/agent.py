"""RedisClaw Agent - The core agent loop."""

import json
from dataclasses import dataclass, field
from typing import Generator

import anthropic

from .tools import TOOLS, ToolExecutor


SYSTEM_PROMPT = """You are RedisClaw, a coding agent running in a sandboxed environment with a Redis-backed persistent filesystem.

## Environment
- Your workspace is at /workspace in the sandbox
- Files written via Redis-FS are immediately available in the sandbox
- The sandbox has Python 3, Node.js, Git, and common dev tools
- Files persist across sessions

## Guidelines
1. When asked to write code, write it to a file and run it to verify
2. Always check command output for errors
3. Use relative paths from /workspace when possible
4. Be concise but show your work
5. If a task requires multiple steps, do them in sequence

## Workflow
1. Understand the request
2. Plan your approach (briefly)
3. Execute using tools
4. Verify the results
5. Report back

You have access to: run_command, read_file, write_file, list_files, delete_file."""


@dataclass
class Message:
    """A conversation message."""
    role: str
    content: str


@dataclass
class AgentConfig:
    """Agent configuration."""
    sandbox_url: str = "http://localhost:8090"
    redis_url: str = "redis://localhost:6380"
    fs_key: str = "sandbox"
    model: str = "claude-sonnet-4-20250514"
    max_tokens: int = 4096


@dataclass 
class AgentState:
    """Mutable agent state."""
    messages: list[Message] = field(default_factory=list)
    tool_results: list[dict] = field(default_factory=list)


class Agent:
    """The RedisClaw agent with tool-use loop."""
    
    def __init__(self, config: AgentConfig | None = None):
        self.config = config or AgentConfig()
        self.client = anthropic.Anthropic()
        self.tools = ToolExecutor(
            self.config.sandbox_url,
            self.config.redis_url,
            self.config.fs_key,
        )
        self.state = AgentState()
    
    def chat(self, user_message: str) -> Generator[str, None, None]:
        """
        Send a message and yield responses.
        
        This is the main agent loop. It:
        1. Sends the user message to Claude
        2. If Claude wants to use tools, executes them
        3. Continues until Claude gives a final response
        """
        self.state.messages.append(Message(role="user", content=user_message))
        
        while True:
            # Build messages for API
            api_messages = [
                {"role": m.role, "content": m.content}
                for m in self.state.messages
            ]
            
            # Call Claude
            response = self.client.messages.create(
                model=self.config.model,
                max_tokens=self.config.max_tokens,
                system=SYSTEM_PROMPT,
                tools=TOOLS,
                messages=api_messages,
            )
            
            # Process response
            assistant_content = []
            tool_uses = []
            text_response = ""
            
            for block in response.content:
                if block.type == "text":
                    text_response += block.text
                    assistant_content.append({"type": "text", "text": block.text})
                elif block.type == "tool_use":
                    tool_uses.append(block)
                    assistant_content.append({
                        "type": "tool_use",
                        "id": block.id,
                        "name": block.name,
                        "input": block.input,
                    })
            
            # Add assistant message
            self.state.messages.append(Message(
                role="assistant",
                content=json.dumps(assistant_content) if tool_uses else text_response
            ))
            
            # If no tool use, we're done
            if not tool_uses:
                yield text_response
                return
            
            # Execute tools
            tool_results = []
            for tool_use in tool_uses:
                yield f"\n🔧 {tool_use.name}: {json.dumps(tool_use.input)}\n"
                
                result = self.tools.execute(tool_use.name, tool_use.input)
                
                # Truncate very long outputs
                if len(result) > 10000:
                    result = result[:10000] + "\n... (truncated)"
                
                yield f"📤 {result[:500]}{'...' if len(result) > 500 else ''}\n"
                
                tool_results.append({
                    "type": "tool_result",
                    "tool_use_id": tool_use.id,
                    "content": result,
                })
            
            # Add tool results as user message (Claude's convention)
            self.state.messages.append(Message(
                role="user",
                content=json.dumps(tool_results)
            ))
            
            # Check stop reason
            if response.stop_reason == "end_turn":
                if text_response:
                    yield text_response
                return
    
    def reset(self):
        """Clear conversation history."""
        self.state = AgentState()
    
    def close(self):
        """Clean up resources."""
        self.tools.close()

