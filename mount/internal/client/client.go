// Package client provides typed wrappers around Redis FS.* commands.
package client

import (
	"context"
	"errors"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// Client wraps a go-redis client with FS.* command methods.
type Client struct {
	rdb *redis.Client
	key string // Redis key holding the filesystem
}

// New creates a new FS client for the given Redis key.
func New(rdb *redis.Client, key string) *Client {
	return &Client{rdb: rdb, key: key}
}

// Stat returns metadata for a path. Returns nil, nil if path does not exist.
func (c *Client) Stat(ctx context.Context, path string) (*StatResult, error) {
	res, err := c.rdb.Do(ctx, "FS.STAT", c.key, path).Slice()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil // path does not exist
		}
		return nil, err
	}
	return parseStat(res)
}

// Cat returns the file content at path.
func (c *Client) Cat(ctx context.Context, path string) ([]byte, error) {
	val, err := c.rdb.Do(ctx, "FS.CAT", c.key, path).Result()
	if err != nil {
		return nil, err
	}
	switch v := val.(type) {
	case string:
		return []byte(v), nil
	case []byte:
		return v, nil
	default:
		return nil, fmt.Errorf("unexpected CAT response type: %T", val)
	}
}

// Echo writes content to a file (creates or overwrites).
func (c *Client) Echo(ctx context.Context, path string, data []byte) error {
	return c.rdb.Do(ctx, "FS.ECHO", c.key, path, data).Err()
}

// EchoAppend appends content to a file.
func (c *Client) EchoAppend(ctx context.Context, path string, data []byte) error {
	return c.rdb.Do(ctx, "FS.ECHO", c.key, path, data, "APPEND").Err()
}

// Touch creates an empty file.
func (c *Client) Touch(ctx context.Context, path string) error {
	return c.rdb.Do(ctx, "FS.TOUCH", c.key, path).Err()
}

// Mkdir creates a directory (with PARENTS to auto-create ancestors).
func (c *Client) Mkdir(ctx context.Context, path string) error {
	return c.rdb.Do(ctx, "FS.MKDIR", c.key, path, "PARENTS").Err()
}

// Rm removes a file, directory, or symlink.
func (c *Client) Rm(ctx context.Context, path string) error {
	return c.rdb.Do(ctx, "FS.RM", c.key, path).Err()
}

// Ls returns the children of a directory.
func (c *Client) Ls(ctx context.Context, path string) ([]string, error) {
	return c.rdb.Do(ctx, "FS.LS", c.key, path).StringSlice()
}

// LsLong returns detailed directory listing.
func (c *Client) LsLong(ctx context.Context, path string) ([]LsEntry, error) {
	res, err := c.rdb.Do(ctx, "FS.LS", c.key, path, "LONG").Slice()
	if err != nil {
		return nil, err
	}
	return parseLsLong(res)
}

// Mv renames/moves a path.
func (c *Client) Mv(ctx context.Context, src, dst string) error {
	return c.rdb.Do(ctx, "FS.MV", c.key, src, dst).Err()
}

// Ln creates a symbolic link.
func (c *Client) Ln(ctx context.Context, target, linkpath string) error {
	return c.rdb.Do(ctx, "FS.LN", c.key, target, linkpath).Err()
}

// Readlink returns the target of a symbolic link.
func (c *Client) Readlink(ctx context.Context, path string) (string, error) {
	return c.rdb.Do(ctx, "FS.READLINK", c.key, path).Text()
}

// Chmod changes file permissions.
func (c *Client) Chmod(ctx context.Context, path string, mode uint32) error {
	modeStr := fmt.Sprintf("%04o", mode)
	return c.rdb.Do(ctx, "FS.CHMOD", c.key, path, modeStr).Err()
}

// Chown changes file owner and group.
func (c *Client) Chown(ctx context.Context, path string, uid, gid uint32) error {
	return c.rdb.Do(ctx, "FS.CHOWN", c.key, path, uid, gid).Err()
}

// Truncate truncates or extends a file to the given length.
func (c *Client) Truncate(ctx context.Context, path string, size int64) error {
	return c.rdb.Do(ctx, "FS.TRUNCATE", c.key, path, size).Err()
}

// Utimens sets access and modification times (milliseconds). -1 means don't change.
func (c *Client) Utimens(ctx context.Context, path string, atimeMs, mtimeMs int64) error {
	return c.rdb.Do(ctx, "FS.UTIMENS", c.key, path, atimeMs, mtimeMs).Err()
}

// Info returns filesystem-level statistics.
func (c *Client) Info(ctx context.Context) (*InfoResult, error) {
	res, err := c.rdb.Do(ctx, "FS.INFO", c.key).Slice()
	if err != nil {
		return nil, err
	}
	return parseInfo(res)
}
