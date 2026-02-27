# Redis-FS Skill Guide — Migrating Agent File Storage to Redis

## Introduction

Redis-FS is a Redis module that provides a complete POSIX-like filesystem as a native data type. One Redis key holds one filesystem volume — directories, files, symlinks, permissions, and all the metadata you'd expect.

**Why migrate from local files to Redis-FS:**
- **Persistence** — data survives process restarts via RDB/AOF
- **Multi-client access** — any Redis client can read/write the same volume concurrently
- **Atomic operations** — every FS.* command is atomic, no partial writes
- **No local disk dependency** — agents can run statelessly; storage lives in Redis
- **Instant cleanup** — `DEL vol` removes an entire filesystem in one command

**Key concept:** one Redis key = one filesystem volume. The key name is the volume name. All paths within a volume are absolute (start with `/`).

## Prerequisites

1. Redis server running with the module loaded:
   ```
   redis-server --loadmodule /path/to/fs.so
   ```
2. All examples below use `redis-cli`. Replace `vol` with your chosen volume name.

## Command Mapping — Quick Reference

| Unix shell command                | redis-cli equivalent                        | Notes                                      |
|-----------------------------------|---------------------------------------------|--------------------------------------------|
| `echo "text" > file`             | `FS.ECHO vol /file "text"`                  | Creates parents automatically              |
| `echo "text" >> file`            | `FS.ECHO vol /file "text" APPEND`           | Creates file if missing; also `FS.APPEND`  |
| `cat file`                       | `FS.CAT vol /file`                          | Follows symlinks                           |
| `touch file`                     | `FS.TOUCH vol /file`                        | Creates empty file or updates mtime        |
| `mkdir dir`                      | `FS.MKDIR vol /dir`                         | Parent must exist                          |
| `mkdir -p a/b/c`                 | `FS.MKDIR vol /a/b/c PARENTS`               | Creates intermediates                      |
| `ls dir`                         | `FS.LS vol /dir`                            | Returns child names                        |
| `ls -l dir`                      | `FS.LS vol /dir LONG`                       | Includes type, mode, size, mtime           |
| `rm file`                        | `FS.RM vol /file`                           | Works on files, dirs, symlinks             |
| `rm -r dir`                      | `FS.RM vol /dir RECURSIVE`                  | Deletes entire subtree                     |
| `cp src dst`                     | `FS.CP vol /src /dst`                       | Files only without RECURSIVE               |
| `cp -r src dst`                  | `FS.CP vol /src /dst RECURSIVE`             | Deep copy with metadata                    |
| `mv src dst`                     | `FS.MV vol /src /dst`                       | Moves entire subtrees atomically           |
| `find dir -name "*.txt"`         | `FS.FIND vol /dir "*.txt"`                  | Full glob: `*`, `?`, `[a-z]`, `[!x]`, `\` |
| `find dir -name "*.txt" -type f` | `FS.FIND vol /dir "*.txt" TYPE file`        | Filter by type: file, dir, symlink         |
| `grep -r "pattern" dir`          | `FS.GREP vol /dir "*pattern*"`              | Glob match on each line, bloom-accelerated |
| `grep -ri "pattern" dir`         | `FS.GREP vol /dir "*pattern*" NOCASE`       | Case-insensitive                           |
| `stat file`                      | `FS.STAT vol /file`                         | Full metadata: type, mode, uid, gid, times |
| `test -e file`                   | `FS.TEST vol /file`                         | Returns 1 or 0                             |
| `chmod 0755 file`                | `FS.CHMOD vol /file 0755`                   | Octal mode string                          |
| `chown uid:gid file`             | `FS.CHOWN vol /file uid gid`                | Separate uid and gid args                  |
| `ln -s target link`              | `FS.LN vol /target /link`                   | Target can be relative or absolute         |
| `readlink link`                  | `FS.READLINK vol /link`                     | Returns raw target string                  |
| `tree dir`                       | `FS.TREE vol /dir`                          | Nested array structure                     |
| `tree -L 2 dir`                  | `FS.TREE vol /dir DEPTH 2`                  | Limits recursion depth                     |
| `du -sh` / `df`                  | `FS.INFO vol`                               | File/dir/symlink counts + total bytes      |

## Bulk Import — Migrating an Existing Directory Tree

### Import all files from a local directory

`FS.ECHO` auto-creates parent directories, so you only need to import the files — the directory structure builds itself.

**Shell one-liner — import a directory tree into Redis:**

```bash
cd /path/to/local/dir && find . -type f | while read -r f; do
  redis-cli FS.ECHO vol "/${f#./}" "$(cat "$f")"
done
```

This walks every file under the current directory and writes it into the volume `vol` with its relative path converted to absolute.

**For binary files**, use `redis-cli --pipe` or pipe raw bytes. File content in Redis-FS is binary-safe:

```bash
cd /path/to/local/dir && find . -type f | while read -r f; do
  path="/${f#./}"
  content="$(cat "$f")"
  redis-cli FS.ECHO vol "$path" "$content"
done
```

### Optional: preserve permissions after import

```bash
cd /path/to/local/dir && find . -type f | while read -r f; do
  path="/${f#./}"
  mode=$(stat -c '%a' "$f")
  uid=$(stat -c '%u' "$f")
  gid=$(stat -c '%g' "$f")
  redis-cli FS.CHMOD vol "$path" "0$mode"
  redis-cli FS.CHOWN vol "$path" "$uid" "$gid"
done
```

### Verify the import

```bash
# Check overall counts
redis-cli FS.INFO vol

# Spot-check the directory structure
redis-cli FS.TREE vol / DEPTH 2

# List all files
redis-cli FS.FIND vol / "*"

# Verify a specific file
redis-cli FS.CAT vol /README.md
```

## Going-Forward Usage Patterns

Once migrated, replace shell commands in your agent's workflow with their `FS.*` equivalents. Every example below shows the old shell pattern and the new Redis-FS command.

### Write a file

```bash
# Before:
echo "Hello, World!" > /data/hello.txt

# After:
redis-cli FS.ECHO vol /data/hello.txt "Hello, World!"
```

Parent directories are created automatically — no need to `mkdir -p` first.

### Append to a file

```bash
# Before:
echo "new line" >> /data/log.txt

# After:
redis-cli FS.ECHO vol /data/log.txt "new line" APPEND
```

If the file doesn't exist yet, it is created. `FS.APPEND vol /data/log.txt "new line"` also works (returns the new byte count).

### Read a file

```bash
# Before:
cat /data/hello.txt

# After:
redis-cli FS.CAT vol /data/hello.txt
```

Returns `nil` if the file doesn't exist. Follows symlinks automatically.

### Create directories

```bash
# Before:
mkdir -p /data/a/b/c

# After:
redis-cli FS.MKDIR vol /data/a/b/c PARENTS
```

Idempotent — running it again when the directory exists is a no-op.

### List directory contents

```bash
# Before:
ls /data
ls -l /data

# After:
redis-cli FS.LS vol /data
redis-cli FS.LS vol /data LONG
```

`LONG` format returns `[name, type, mode, size, mtime]` per entry.

### Check if a path exists

```bash
# Before:
test -e /data/config.json && echo "exists"

# After:
redis-cli FS.TEST vol /data/config.json
# Returns 1 if exists, 0 otherwise
```

### Delete files and directories

```bash
# Before:
rm /data/old.txt
rm -rf /data/temp

# After:
redis-cli FS.RM vol /data/old.txt
redis-cli FS.RM vol /data/temp RECURSIVE
```

Returns 1 if something was deleted, 0 if the path didn't exist. Non-empty directories require `RECURSIVE`.

### Copy and move

```bash
# Before:
cp /data/config.json /data/config.json.bak
cp -r /data/src /data/src-backup
mv /data/draft.txt /data/final.txt

# After:
redis-cli FS.CP vol /data/config.json /data/config.json.bak
redis-cli FS.CP vol /data/src /data/src-backup RECURSIVE
redis-cli FS.MV vol /data/draft.txt /data/final.txt
```

Both commands auto-create parent directories at the destination.

### Search by filename

```bash
# Before:
find /data -name "*.md"
find /data -name "*.md" -type f

# After:
redis-cli FS.FIND vol /data "*.md"
redis-cli FS.FIND vol /data "*.md" TYPE file
```

Full glob syntax: `*`, `?`, `[a-z]`, `[!x]`, `\` escaping. Matches against the **basename** only, not the full path.

### Search file contents

```bash
# Before:
grep -r "TODO" /data
grep -ri "error" /data

# After:
redis-cli FS.GREP vol /data "*TODO*"
redis-cli FS.GREP vol /data "*error*" NOCASE
```

Returns `[filepath, line_number, line]` triples for each match. Uses glob patterns (not regex) — wrap the search term in `*...*` to match anywhere in a line.

### File metadata

```bash
# Before:
stat /data/config.json

# After:
redis-cli FS.STAT vol /data/config.json
```

Returns: type, mode, uid, gid, size, ctime, mtime, atime.

### Symlinks

```bash
# Before:
ln -s /data/config.json /data/current-config

# After:
redis-cli FS.LN vol /data/config.json /data/current-config
```

Read the target: `FS.READLINK vol /data/current-config`. Symlinks resolve at read time, up to 40 levels deep.

### Filesystem overview

```bash
# Before:
du -sh /data

# After:
redis-cli FS.INFO vol
```

Returns file count, directory count, symlink count, total bytes, and total inodes — all O(1).

## Key Differences and Gotchas

1. **All paths must be absolute** — start with `/`. There is no working directory.
2. **No `cd` or `pwd`** — every command takes the full path explicitly.
3. **Grep uses glob patterns, not regex** — use `*error*` not `.*error.*`. Wrap search terms in `*...*` to match substrings.
4. **FS.GREP returns triples** — each match is `[filepath, line_number, line]`, not plain text.
5. **FS.FIND matches basename only** — `FS.FIND vol / "*.md"` matches `/docs/README.md` (basename is `README.md`), not by full path.
6. **FS.ECHO auto-creates parents** — no need to `mkdir -p` before writing a file.
7. **Symlinks resolve at read time** — max 40 levels; cycles produce an error, not a hang.
8. **Large recursive operations block Redis** — keep volumes to a reasonable size. For millions of files, partition across multiple keys.
9. **No streaming reads** — `FS.CAT` returns the entire file at once. There is no offset/range read.
10. **Permission bits are metadata only** — `FS.CHMOD` and `FS.CHOWN` store values but Redis does not enforce them. Use Redis ACLs for access control.

## Volume Management Tips

**Use meaningful key names** — treat them like project or tenant identifiers:
```
redis-cli FS.ECHO project-alpha /README.md "Alpha project"
redis-cli FS.ECHO project-beta /README.md "Beta project"
```

**Delete an entire filesystem:**
```
redis-cli DEL vol
```

**Temporary filesystems** — auto-expire after a timeout:
```
redis-cli EXPIRE vol 3600
```

**List all filesystem volumes:**
```
redis-cli SCAN 0 TYPE redis-fs0
```

**Rename a volume:**
```
redis-cli RENAME staging production
```
