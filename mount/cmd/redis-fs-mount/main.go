// redis-fs-mount mounts a Redis FS filesystem via FUSE.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/redis-fs/mount/internal/client"
	"github.com/redis-fs/mount/internal/redisfs"
)

func main() {
	redisAddr := flag.String("redis", "localhost:6379", "Redis server address")
	redisPassword := flag.String("password", "", "Redis password")
	redisDB := flag.Int("db", 0, "Redis database number")
	attrTimeout := flag.Float64("attr-timeout", 1.0, "Attribute cache TTL in seconds")
	readOnly := flag.Bool("readonly", false, "Mount read-only")
	allowOther := flag.Bool("allow-other", false, "Allow other users to access mount")
	foreground := flag.Bool("foreground", true, "Run in foreground")
	debug := flag.Bool("debug", false, "Enable FUSE debug logging")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] <redis-key> <mountpoint>\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Mount a Redis FS filesystem via FUSE.\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if flag.NArg() != 2 {
		flag.Usage()
		os.Exit(1)
	}

	redisKey := flag.Arg(0)
	mountpoint := flag.Arg(1)

	// Verify mountpoint exists.
	fi, err := os.Stat(mountpoint)
	if err != nil {
		log.Fatalf("mountpoint error: %v", err)
	}
	if !fi.IsDir() {
		log.Fatalf("mountpoint %s is not a directory", mountpoint)
	}

	// Connect to Redis.
	rdb := redis.NewClient(&redis.Options{
		Addr:     *redisAddr,
		Password: *redisPassword,
		DB:       *redisDB,
		PoolSize: 16,
	})

	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("cannot connect to Redis at %s: %v", *redisAddr, err)
	}

	c := client.New(rdb, redisKey)

	uid, gid := redisfs.GetOwnership()

	opts := &redisfs.Options{
		AttrTimeout: time.Duration(*attrTimeout * float64(time.Second)),
		ReadOnly:    *readOnly,
		Debug:       *debug,
		UID:         uid,
		GID:         gid,
	}

	_ = *allowOther
	_ = *foreground

	log.Printf("Mounting Redis FS key %q at %s", redisKey, mountpoint)
	log.Printf("Redis: %s (db %d)", *redisAddr, *redisDB)

	server, err := redisfs.Mount(mountpoint, c, opts)
	if err != nil {
		log.Fatalf("mount failed: %v", err)
	}

	// Handle shutdown signals.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		log.Printf("Received signal %v, unmounting...", sig)
		err := server.Unmount()
		if err != nil {
			log.Printf("Unmount error: %v", err)
		}
	}()

	log.Printf("Filesystem mounted. Press Ctrl+C to unmount.")
	server.Wait()
	log.Printf("Unmounted.")

	rdb.Close()
}
