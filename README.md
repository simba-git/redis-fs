# redis-fs

A native Redis module that implements a POSIX-like virtual filesystem as a custom data type with an `FS.*` command family.

## Overview

**One Redis key = one filesystem.** The entire filesystem lives under a single key as a custom module type, backed by a flat hashmap of absolute paths to inodes. Each inode stores type (file/dir/symlink), POSIX metadata (mode, uid, gid, timestamps), and inline content.

Think of it as `RedisJSON` but for filesystems — `FS.WRITE`, `FS.READ`, `FS.LS`, `FS.MKDIR`, and more.

## Building

```bash
make
```

Produces `fs.so`.

## Loading

```bash
redis-server --loadmodule ./fs.so
```

Or at runtime:

```bash
redis-cli MODULE LOAD /path/to/fs.so
```

## Quick Start

```bash
# Initialize a filesystem
redis-cli FS.INIT myfs

# Write a file
redis-cli FS.WRITE myfs /hello.txt "Hello, World!"

# Read it back
redis-cli FS.READ myfs /hello.txt

# Create directories
redis-cli FS.MKDIR myfs /docs PARENTS

# List directory contents
redis-cli FS.LS myfs /

# Get file metadata
redis-cli FS.STAT myfs /hello.txt

# Tree view
redis-cli FS.TREE myfs /

# Search for files
redis-cli FS.FIND myfs / "*.txt"

# Search file contents
redis-cli FS.GREP myfs / "*Hello*"
```

## Commands

| Command | Description | Complexity |
|---------|-------------|------------|
| `FS.INIT key` | Initialize a filesystem | O(1) |
| `FS.INFO key` | Get filesystem stats | O(1) |
| `FS.WRITE key path content` | Write/overwrite a file | O(d) |
| `FS.READ key path` | Read file content | O(1) |
| `FS.APPEND key path content` | Append to a file | O(1) |
| `FS.DEL key path [RECURSIVE]` | Delete file/directory | O(n) |
| `FS.TOUCH key path` | Create empty file or update mtime | O(d) |
| `FS.MKDIR key path [PARENTS]` | Create directory | O(d) |
| `FS.LS key path [LONG]` | List directory | O(n) |
| `FS.STAT key path` | Get inode metadata | O(1) |
| `FS.EXISTS key path` | Check path existence | O(1) |
| `FS.CHMOD key path mode` | Change permissions | O(1) |
| `FS.CHOWN key path uid [gid]` | Change ownership | O(1) |
| `FS.SYMLINK key target linkpath` | Create symbolic link | O(d) |
| `FS.READLINK key path` | Read symlink target | O(1) |
| `FS.CP key src dst [RECURSIVE]` | Copy file/directory | O(n) |
| `FS.MV key src dst` | Move/rename | O(n) |
| `FS.TREE key path [DEPTH n]` | Recursive tree view | O(n) |
| `FS.FIND key path pattern [TYPE t]` | Find by glob pattern | O(n) |
| `FS.GREP key path pattern [NOCASE]` | Search file contents | O(n*m) |

Where `d` = path depth, `n` = number of inodes, `m` = total content size.

## License

BSD-2-Clause
