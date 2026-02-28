"""RedisClaw CLI - Interactive console interface."""

import argparse
import os
import sys

from rich.console import Console
from rich.markdown import Markdown
from rich.panel import Panel
from prompt_toolkit import PromptSession
from prompt_toolkit.history import FileHistory

from .agent import Agent, AgentConfig
from .tools import ToolExecutor


console = Console()


def print_welcome():
    """Print welcome message."""
    console.print(Panel.fit(
        "[bold cyan]🦀 RedisClaw[/bold cyan]\n"
        "A coding agent with Redis-FS backed sandbox\n\n"
        "[dim]Commands:[/dim]\n"
        "  /run <cmd>  - Run a shell command directly\n"
        "  /read <path> - Read a file\n"
        "  /ls [path]  - List files\n"
        "  /clear      - Clear conversation\n"
        "  /exit       - Exit",
        title="Welcome",
        border_style="cyan",
    ))


def run_interactive(config: AgentConfig):
    """Run interactive CLI mode."""
    print_welcome()
    
    # Check for API key
    if not os.environ.get("ANTHROPIC_API_KEY"):
        console.print("[yellow]⚠️  ANTHROPIC_API_KEY not set - agent mode disabled[/yellow]")
        console.print("[dim]You can still use /run, /read, /ls commands directly[/dim]\n")
        agent = None
        tools = ToolExecutor(config.sandbox_url, config.redis_url, config.fs_key)
    else:
        agent = Agent(config)
        tools = agent.tools
    
    # Setup prompt
    history_file = os.path.expanduser("~/.redisclaw_history")
    session = PromptSession(history=FileHistory(history_file))
    
    try:
        while True:
            try:
                user_input = session.prompt("redisclaw> ").strip()
            except KeyboardInterrupt:
                continue
            except EOFError:
                break
            
            if not user_input:
                continue
            
            # Handle commands
            if user_input.startswith("/"):
                if handle_command(user_input, tools, agent):
                    break
                continue
            
            # Agent chat
            if agent is None:
                console.print("[yellow]Agent not available (no API key)[/yellow]")
                console.print("[dim]Use /run, /read, /ls or set ANTHROPIC_API_KEY[/dim]")
                continue
            
            console.print()
            for chunk in agent.chat(user_input):
                if chunk.startswith("\n🔧") or chunk.startswith("📤"):
                    console.print(chunk, end="")
                else:
                    console.print(Markdown(chunk))
            console.print()
    
    finally:
        if agent:
            agent.close()
        else:
            tools.close()


def handle_command(cmd: str, tools: ToolExecutor, agent: Agent | None) -> bool:
    """Handle a slash command. Returns True if should exit."""
    parts = cmd.split(maxsplit=1)
    command = parts[0].lower()
    arg = parts[1] if len(parts) > 1 else ""
    
    if command in ("/exit", "/quit", "/q"):
        console.print("[cyan]Goodbye![/cyan]")
        return True
    
    elif command == "/clear":
        if agent:
            agent.reset()
        console.print("[green]Conversation cleared[/green]")
    
    elif command == "/run":
        if not arg:
            console.print("[red]Usage: /run <command>[/red]")
        else:
            result = tools.execute("run_command", {"command": arg})
            console.print(result)
    
    elif command == "/read":
        if not arg:
            console.print("[red]Usage: /read <path>[/red]")
        else:
            result = tools.execute("read_file", {"path": arg})
            console.print(result)
    
    elif command == "/ls":
        result = tools.execute("list_files", {"path": arg or "/"})
        console.print(result)
    
    elif command == "/write":
        if not arg:
            console.print("[red]Usage: /write <path>[/red]")
        else:
            console.print("Enter content (Ctrl+D when done):")
            content = sys.stdin.read()
            result = tools.execute("write_file", {"path": arg, "content": content})
            console.print(result)
    
    elif command == "/help":
        print_welcome()
    
    else:
        console.print(f"[red]Unknown command: {command}[/red]")
    
    return False


def main():
    """Main entry point."""
    parser = argparse.ArgumentParser(description="RedisClaw - Redis-FS Coding Agent")
    parser.add_argument("--sandbox", default="http://localhost:8090", help="Sandbox URL")
    parser.add_argument("--redis", default="redis://localhost:6380", help="Redis URL")
    parser.add_argument("--key", default="sandbox", help="Redis FS key")
    parser.add_argument("--model", default="claude-sonnet-4-20250514", help="Model to use")
    parser.add_argument("--run", metavar="CMD", help="Run a command and exit")
    parser.add_argument("--read", metavar="PATH", help="Read a file and exit")
    parser.add_argument("--ls", metavar="PATH", nargs="?", const="/", help="List files and exit")
    
    args = parser.parse_args()
    
    config = AgentConfig(
        sandbox_url=args.sandbox,
        redis_url=args.redis,
        fs_key=args.key,
        model=args.model,
    )
    
    # Single command modes
    if args.run or args.read or args.ls:
        tools = ToolExecutor(config.sandbox_url, config.redis_url, config.fs_key)
        try:
            if args.run:
                print(tools.execute("run_command", {"command": args.run}))
            elif args.read:
                print(tools.execute("read_file", {"path": args.read}))
            elif args.ls:
                print(tools.execute("list_files", {"path": args.ls}))
        finally:
            tools.close()
        return
    
    # Interactive mode
    run_interactive(config)


if __name__ == "__main__":
    main()

