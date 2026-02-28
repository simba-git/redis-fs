package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
)

type state struct {
	StartedAt      time.Time `json:"started_at"`
	ManageRedis    bool      `json:"manage_redis"`
	RedisPID       int       `json:"redis_pid"`
	RedisAddr      string    `json:"redis_addr"`
	RedisDB        int       `json:"redis_db"`
	MountPID       int       `json:"mount_pid"`
	Mountpoint     string    `json:"mountpoint"`
	RedisKey       string    `json:"redis_key"`
	RedisLog       string    `json:"redis_log"`
	MountLog       string    `json:"mount_log"`
	RedisServerBin string    `json:"redis_server_bin"`
	MountBin       string    `json:"mount_bin"`
	ArchivePath    string    `json:"archive_path,omitempty"`
}

type config struct {
	UseExistingRedis bool
	RedisServerBin   string
	ModulePath       string
	RedisAddr        string
	RedisHost        string
	RedisPort        int
	RedisPassword    string
	RedisDB          int
	RedisKey         string
	Mountpoint       string
	MountBin         string
	ReadOnly         bool
	AllowOther       bool
	RedisLog         string
	MountLog         string
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "up":
		if err := cmdUp(); err != nil {
			fatal(err)
		}
	case "migrate":
		if err := cmdMigrate(); err != nil {
			fatal(err)
		}
	case "status":
		if err := cmdStatus(); err != nil {
			fatal(err)
		}
	case "down":
		if err := cmdDown(); err != nil {
			fatal(err)
		}
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage: %s <up|migrate|status|down>\n", filepath.Base(os.Args[0]))
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  up      Interactive wizard to start Redis + mount daemons")
	fmt.Fprintln(os.Stderr, "  migrate Import a local directory, archive it, then mount Redis in place")
	fmt.Fprintln(os.Stderr, "  status  Show status for managed daemons and mount")
	fmt.Fprintln(os.Stderr, "  down    Stop managed daemons and unmount")
}

func cmdUp() error {
	if st, err := loadState(); err == nil {
		if st.MountPID > 0 && processAlive(st.MountPID) {
			return fmt.Errorf("an existing managed mount process is running (pid %d). Run '%s down' first", st.MountPID, filepath.Base(os.Args[0]))
		}
	}

	cfg, err := runWizard(os.Stdin, os.Stdout)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	redisPID := 0
	if !cfg.UseExistingRedis {
		pid, err := startRedisDaemon(cfg)
		if err != nil {
			return err
		}
		redisPID = pid
		fmt.Printf("Started Redis daemon (pid %d) at %s\n", pid, cfg.RedisAddr)
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
		PoolSize: 4,
	})
	defer rdb.Close()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("cannot connect to Redis at %s: %w", cfg.RedisAddr, err)
	}

	if err := ensureFSModuleLoaded(ctx, rdb); err != nil {
		return err
	}

	if err := os.MkdirAll(cfg.Mountpoint, 0o755); err != nil {
		return fmt.Errorf("create mountpoint: %w", err)
	}

	if err := rdb.Do(ctx, "FS.TOUCH", cfg.RedisKey, "/.mount-check").Err(); err != nil {
		return fmt.Errorf("failed to initialize key %q: %w", cfg.RedisKey, err)
	}

	mpid, err := startMountDaemon(cfg)
	if err != nil {
		return err
	}
	fmt.Printf("Started mount daemon (pid %d)\n", mpid)

	if err := waitForMount(cfg.Mountpoint, 6*time.Second); err != nil {
		return fmt.Errorf("mount did not become ready: %w", err)
	}

	st := state{
		StartedAt:      time.Now().UTC(),
		ManageRedis:    !cfg.UseExistingRedis,
		RedisAddr:      cfg.RedisAddr,
		RedisDB:        cfg.RedisDB,
		MountPID:       mpid,
		Mountpoint:     cfg.Mountpoint,
		RedisKey:       cfg.RedisKey,
		RedisLog:       cfg.RedisLog,
		MountLog:       cfg.MountLog,
		RedisServerBin: cfg.RedisServerBin,
		MountBin:       cfg.MountBin,
	}
	if !cfg.UseExistingRedis {
		st.RedisPID = redisPID
	}

	if err := saveState(st); err != nil {
		return err
	}

	fmt.Println("All services are up.")
	fmt.Printf("Mountpoint: %s\n", cfg.Mountpoint)
	fmt.Printf("Redis key:  %s\n", cfg.RedisKey)
	fmt.Printf("Mount log:  %s\n", cfg.MountLog)
	if st.ManageRedis {
		fmt.Printf("Redis log:  %s\n", cfg.RedisLog)
	}
	return nil
}

func cmdStatus() error {
	st, err := loadState()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Println("No CLI state found. Nothing managed yet.")
			return nil
		}
		return err
	}

	fmt.Printf("Started at: %s\n", st.StartedAt.Format(time.RFC3339))
	fmt.Printf("Redis addr: %s (db %d)\n", st.RedisAddr, st.RedisDB)
	fmt.Printf("Redis key:  %s\n", st.RedisKey)
	fmt.Printf("Mountpoint: %s\n", st.Mountpoint)

	if st.ManageRedis {
		fmt.Printf("Redis daemon: %s", aliveString(st.RedisPID))
		if st.RedisPID > 0 {
			fmt.Printf(" (pid %d)", st.RedisPID)
		}
		fmt.Println()
	} else {
		fmt.Println("Redis daemon: external (not managed by CLI)")
	}

	fmt.Printf("Mount daemon: %s", aliveString(st.MountPID))
	if st.MountPID > 0 {
		fmt.Printf(" (pid %d)", st.MountPID)
	}
	fmt.Println()

	if isMounted(st.Mountpoint) {
		fmt.Println("Mount state: mounted")
	} else {
		fmt.Println("Mount state: not mounted")
	}

	if st.MountLog != "" {
		fmt.Printf("Mount log: %s\n", st.MountLog)
	}
	if st.ManageRedis && st.RedisLog != "" {
		fmt.Printf("Redis log: %s\n", st.RedisLog)
	}
	if st.ArchivePath != "" {
		fmt.Printf("Archive:   %s\n", st.ArchivePath)
	}

	return nil
}

func cmdMigrate() error {
	if st, err := loadState(); err == nil {
		if st.MountPID > 0 && processAlive(st.MountPID) {
			return fmt.Errorf("an existing managed mount process is running (pid %d). Run '%s down' first", st.MountPID, filepath.Base(os.Args[0]))
		}
	}

	cfg, sourceDir, archiveDir, err := runMigrationWizard(os.Stdin, os.Stdout)
	if err != nil {
		return err
	}

	redisPID := 0
	if !cfg.UseExistingRedis {
		pid, err := startRedisDaemon(cfg)
		if err != nil {
			return err
		}
		redisPID = pid
		fmt.Printf("Started Redis daemon (pid %d) at %s\n", pid, cfg.RedisAddr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
		PoolSize: 8,
	})
	defer rdb.Close()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("cannot connect to Redis at %s: %w", cfg.RedisAddr, err)
	}
	if err := ensureFSModuleLoaded(ctx, rdb); err != nil {
		return err
	}

	exists, err := rdb.Exists(ctx, cfg.RedisKey).Result()
	if err != nil {
		return err
	}
	if exists > 0 {
		ok, err := promptYesNo(bufio.NewReader(os.Stdin), os.Stdout, fmt.Sprintf("Redis key %q already exists. Overwrite it?", cfg.RedisKey), false)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("migration cancelled")
		}
		if err := rdb.Del(ctx, cfg.RedisKey).Err(); err != nil {
			return fmt.Errorf("delete existing redis key: %w", err)
		}
	}

	files, dirs, links, err := importDirectory(ctx, rdb, cfg.RedisKey, sourceDir)
	if err != nil {
		return err
	}
	fmt.Printf("Imported %d files, %d directories, %d symlinks into key %q\n", files, dirs, links, cfg.RedisKey)

	if _, err := os.Stat(archiveDir); err == nil {
		return fmt.Errorf("archive path already exists: %s", archiveDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.Rename(sourceDir, archiveDir); err != nil {
		return fmt.Errorf("rename source to archive failed: %w", err)
	}

	rollback := true
	defer func() {
		if rollback {
			_ = os.RemoveAll(sourceDir)
			_ = os.Rename(archiveDir, sourceDir)
		}
	}()

	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		return fmt.Errorf("recreate mountpoint: %w", err)
	}
	cfg.Mountpoint = sourceDir

	mpid, err := startMountDaemon(cfg)
	if err != nil {
		return err
	}
	fmt.Printf("Started mount daemon (pid %d)\n", mpid)

	if err := waitForMount(cfg.Mountpoint, 8*time.Second); err != nil {
		return fmt.Errorf("mount did not become ready: %w", err)
	}

	st := state{
		StartedAt:      time.Now().UTC(),
		ManageRedis:    !cfg.UseExistingRedis,
		RedisPID:       redisPID,
		RedisAddr:      cfg.RedisAddr,
		RedisDB:        cfg.RedisDB,
		MountPID:       mpid,
		Mountpoint:     cfg.Mountpoint,
		RedisKey:       cfg.RedisKey,
		RedisLog:       cfg.RedisLog,
		MountLog:       cfg.MountLog,
		RedisServerBin: cfg.RedisServerBin,
		MountBin:       cfg.MountBin,
		ArchivePath:    archiveDir,
	}
	if err := saveState(st); err != nil {
		return err
	}

	rollback = false
	fmt.Println("Migration complete.")
	fmt.Printf("Archived original directory at: %s\n", archiveDir)
	fmt.Printf("Redis-backed mount active at:   %s\n", cfg.Mountpoint)
	return nil
}

func cmdDown() error {
	st, err := loadState()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Println("No CLI state found. Nothing to stop.")
			return nil
		}
		return err
	}

	if isMounted(st.Mountpoint) {
		if err := unmount(st.Mountpoint); err != nil {
			return fmt.Errorf("unmount %s: %w", st.Mountpoint, err)
		}
		fmt.Printf("Unmounted %s\n", st.Mountpoint)
	}

	if st.MountPID > 0 {
		_ = terminatePID(st.MountPID, 2*time.Second)
	}
	if st.ManageRedis && st.RedisPID > 0 {
		_ = terminatePID(st.RedisPID, 2*time.Second)
	}

	if err := os.Remove(statePath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	fmt.Println("Stopped managed services.")
	return nil
}

func runWizard(in io.Reader, out io.Writer) (config, error) {
	return runWizardWithReader(bufio.NewReader(in), out, "~/test", true)
}

func runWizardWithReader(r *bufio.Reader, out io.Writer, defaultMount string, promptMount bool) (config, error) {
	root := repoRootFromExecutable()
	defRedisBin := defaultRedisBin()
	defMountBin := filepath.Join(root, "mount", "redis-fs-mount")
	if _, err := os.Stat(defMountBin); err != nil {
		defMountBin = "redis-fs-mount"
	}
	defModulePath := filepath.Join(root, "module", "fs.so")

	cfg := config{
		RedisAddr: "localhost:6379",
		RedisHost: "localhost",
		RedisPort: 6379,
		RedisDB:   0,
		RedisKey:  "myfs",
		RedisLog:  "/tmp/rfs-redis.log",
		MountLog:  "/tmp/rfs-mount.log",
	}

	fmt.Fprintln(out, "Redis-FS CLI setup")
	fmt.Fprintln(out, "------------------")

	useExisting, err := promptYesNo(r, out, "Use an existing Redis instance?", true)
	if err != nil {
		return cfg, err
	}
	cfg.UseExistingRedis = useExisting

	addr, err := promptString(r, out, "Redis address (host:port)", cfg.RedisAddr)
	if err != nil {
		return cfg, err
	}
	cfg.RedisAddr = addr

	host, port, err := splitAddr(cfg.RedisAddr)
	if err != nil {
		return cfg, err
	}
	cfg.RedisHost = host
	cfg.RedisPort = port

	pwd, err := promptString(r, out, "Redis password (empty for none)", "")
	if err != nil {
		return cfg, err
	}
	cfg.RedisPassword = pwd

	db, err := promptInt(r, out, "Redis DB number", cfg.RedisDB)
	if err != nil {
		return cfg, err
	}
	cfg.RedisDB = db

	if !cfg.UseExistingRedis {
		redisBin, err := promptString(r, out, "Path to redis-server binary", defRedisBin)
		if err != nil {
			return cfg, err
		}
		cfg.RedisServerBin, err = resolveBinary(redisBin)
		if err != nil {
			return cfg, err
		}
		if _, err := os.Stat(cfg.RedisServerBin); err != nil {
			return cfg, fmt.Errorf("redis-server not found at %s", cfg.RedisServerBin)
		}

		modulePath, err := promptString(r, out, "Path to module fs.so", defModulePath)
		if err != nil {
			return cfg, err
		}
		cfg.ModulePath, err = expandPath(modulePath)
		if err != nil {
			return cfg, err
		}
		if _, err := os.Stat(cfg.ModulePath); err != nil {
			return cfg, fmt.Errorf("module not found at %s", cfg.ModulePath)
		}

		redisLog, err := promptString(r, out, "Redis log file", cfg.RedisLog)
		if err != nil {
			return cfg, err
		}
		cfg.RedisLog, err = expandPath(redisLog)
		if err != nil {
			return cfg, err
		}
	}

	mountBin, err := promptString(r, out, "Path to redis-fs-mount binary", defMountBin)
	if err != nil {
		return cfg, err
	}
	cfg.MountBin, err = resolveBinary(mountBin)
	if err != nil {
		return cfg, err
	}

	key, err := promptString(r, out, "Redis filesystem key", cfg.RedisKey)
	if err != nil {
		return cfg, err
	}
	cfg.RedisKey = key

	if promptMount {
		mp, err := promptString(r, out, "Mount directory", defaultMount)
		if err != nil {
			return cfg, err
		}
		cfg.Mountpoint, err = expandPath(mp)
		if err != nil {
			return cfg, err
		}
	} else {
		mp, err := expandPath(defaultMount)
		if err != nil {
			return cfg, err
		}
		cfg.Mountpoint = mp
	}

	ro, err := promptYesNo(r, out, "Mount read-only?", false)
	if err != nil {
		return cfg, err
	}
	cfg.ReadOnly = ro

	allowOther, err := promptYesNo(r, out, "Allow other users to access mount?", false)
	if err != nil {
		return cfg, err
	}
	cfg.AllowOther = allowOther

	mlog, err := promptString(r, out, "Mount log file", cfg.MountLog)
	if err != nil {
		return cfg, err
	}
	cfg.MountLog, err = expandPath(mlog)
	if err != nil {
		return cfg, err
	}

	return cfg, nil
}

func runMigrationWizard(in io.Reader, out io.Writer) (config, string, string, error) {
	r := bufio.NewReader(in)

	source, err := promptString(r, out, "Directory to migrate", "")
	if err != nil {
		return config{}, "", "", err
	}
	source, err = expandPath(source)
	if err != nil {
		return config{}, "", "", err
	}
	fi, err := os.Stat(source)
	if err != nil {
		return config{}, "", "", fmt.Errorf("source directory error: %w", err)
	}
	if !fi.IsDir() {
		return config{}, "", "", fmt.Errorf("source path is not a directory: %s", source)
	}
	if isMounted(source) {
		return config{}, "", "", fmt.Errorf("source directory is already a mountpoint: %s", source)
	}

	archiveDefault := source + ".archive"
	archiveDir, err := promptString(r, out, "Archive directory path", archiveDefault)
	if err != nil {
		return config{}, "", "", err
	}
	archiveDir, err = expandPath(archiveDir)
	if err != nil {
		return config{}, "", "", err
	}

	confirm, err := promptYesNo(r, out, "Proceed with migration (import, archive original, mount in place)?", false)
	if err != nil {
		return config{}, "", "", err
	}
	if !confirm {
		return config{}, "", "", errors.New("migration cancelled")
	}

	cfg, err := runWizardWithReader(r, out, source, false)
	if err != nil {
		return config{}, "", "", err
	}
	cfg.Mountpoint = source
	return cfg, source, archiveDir, nil
}

func importDirectory(ctx context.Context, rdb *redis.Client, key, source string) (files int, dirs int, symlinks int, err error) {
	err = filepath.WalkDir(source, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == source {
			return nil
		}

		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		redisPath := "/" + filepath.ToSlash(rel)

		info, err := os.Lstat(path)
		if err != nil {
			return err
		}

		switch {
		case d.Type()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if err := rdb.Do(ctx, "FS.LN", key, target, redisPath).Err(); err != nil {
				return fmt.Errorf("FS.LN %s: %w", redisPath, err)
			}
			symlinks++
		case d.IsDir():
			if err := rdb.Do(ctx, "FS.MKDIR", key, redisPath, "PARENTS").Err(); err != nil {
				return fmt.Errorf("FS.MKDIR %s: %w", redisPath, err)
			}
			dirs++
		default:
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if err := rdb.Do(ctx, "FS.ECHO", key, redisPath, data).Err(); err != nil {
				return fmt.Errorf("FS.ECHO %s: %w", redisPath, err)
			}
			files++
		}

		if err := applyMetadata(ctx, rdb, key, redisPath, info); err != nil {
			return err
		}
		return nil
	})
	return files, dirs, symlinks, err
}

func applyMetadata(ctx context.Context, rdb *redis.Client, key, path string, info os.FileInfo) error {
	modeStr := fmt.Sprintf("%04o", info.Mode().Perm())
	if err := rdb.Do(ctx, "FS.CHMOD", key, path, modeStr).Err(); err != nil {
		return fmt.Errorf("FS.CHMOD %s: %w", path, err)
	}

	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		if err := rdb.Do(ctx, "FS.CHOWN", key, path, st.Uid, st.Gid).Err(); err != nil {
			return fmt.Errorf("FS.CHOWN %s: %w", path, err)
		}

		atimeMs := st.Atim.Sec*1000 + st.Atim.Nsec/1_000_000
		mtimeMs := st.Mtim.Sec*1000 + st.Mtim.Nsec/1_000_000
		if err := rdb.Do(ctx, "FS.UTIMENS", key, path, atimeMs, mtimeMs).Err(); err != nil {
			return fmt.Errorf("FS.UTIMENS %s: %w", path, err)
		}
	}
	return nil
}

func startRedisDaemon(cfg config) (int, error) {
	pidfile := fmt.Sprintf("/tmp/rfs-%d.pid", cfg.RedisPort)
	args := []string{
		"--port", strconv.Itoa(cfg.RedisPort),
		"--loadmodule", cfg.ModulePath,
		"--save", "",
		"--appendonly", "no",
		"--daemonize", "yes",
		"--pidfile", pidfile,
		"--logfile", cfg.RedisLog,
		"--dir", "/tmp",
		"--dbfilename", fmt.Sprintf("rfs-%d.rdb", cfg.RedisPort),
	}

	cmd := exec.Command(cfg.RedisServerBin, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return 0, fmt.Errorf("start redis failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		pidBytes, err := os.ReadFile(pidfile)
		if err == nil {
			pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
			if err == nil && pid > 0 {
				return pid, nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return 0, errors.New("redis started but pidfile was not found")
}

func startMountDaemon(cfg config) (int, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.MountLog), 0o755); err != nil {
		return 0, err
	}
	f, err := os.OpenFile(cfg.MountLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, err
	}

	args := []string{
		"--redis", cfg.RedisAddr,
		"--db", strconv.Itoa(cfg.RedisDB),
		"--foreground",
		cfg.RedisKey,
		cfg.Mountpoint,
	}
	if cfg.RedisPassword != "" {
		args = append([]string{"--password", cfg.RedisPassword}, args...)
	}
	if cfg.ReadOnly {
		args = append([]string{"--readonly"}, args...)
	}
	if cfg.AllowOther {
		args = append([]string{"--allow-other"}, args...)
	}

	cmd := exec.Command(cfg.MountBin, args...)
	cmd.Stdout = f
	cmd.Stderr = f
	devNull, err := os.Open(os.DevNull)
	if err == nil {
		defer devNull.Close()
		cmd.Stdin = devNull
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		_ = f.Close()
		return 0, fmt.Errorf("start mount failed: %w", err)
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	_ = f.Close()
	return pid, nil
}

func ensureFSModuleLoaded(ctx context.Context, rdb *redis.Client) error {
	res, err := rdb.Do(ctx, "COMMAND", "LIST", "FILTERBY", "MODULE", "fs").Slice()
	if err != nil {
		return fmt.Errorf("module capability check failed: %w", err)
	}
	if len(res) > 0 {
		return nil
	}
	return errors.New("Redis FS module 'fs' is not loaded")
}

func waitForMount(mountpoint string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if isMounted(mountpoint) {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return errors.New("timeout waiting for mount")
}

func isMounted(mountpoint string) bool {
	b, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return false
	}
	needle := " " + mountpoint + " "
	return strings.Contains(string(b), needle)
}

func unmount(mountpoint string) error {
	cmds := [][]string{{"fusermount", "-u", mountpoint}, {"fusermount", "-uz", mountpoint}, {"umount", "-l", mountpoint}}
	for _, c := range cmds {
		cmd := exec.Command(c[0], c[1:]...)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	return errors.New("all unmount commands failed")
}

func terminatePID(pid int, timeout time.Duration) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	_ = p.Signal(syscall.SIGTERM)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = p.Signal(syscall.SIGKILL)
	return nil
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil
}

func aliveString(pid int) string {
	if pid > 0 && processAlive(pid) {
		return "alive"
	}
	return "not running"
}

func stateDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".rfs")
}

func statePath() string {
	return filepath.Join(stateDir(), "state.json")
}

func saveState(st state) error {
	if err := os.MkdirAll(stateDir(), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statePath(), b, 0o600)
}

func loadState() (state, error) {
	var st state
	b, err := os.ReadFile(statePath())
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return st, err
	}
	return st, nil
}

func promptString(r *bufio.Reader, out io.Writer, label, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(out, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(out, "%s: ", label)
	}
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(line)
	if v == "" {
		return def, nil
	}
	return v, nil
}

func promptYesNo(r *bufio.Reader, out io.Writer, label string, def bool) (bool, error) {
	defMark := "y/N"
	if def {
		defMark = "Y/n"
	}
	fmt.Fprintf(out, "%s [%s]: ", label, defMark)
	line, err := r.ReadString('\n')
	if err != nil {
		return false, err
	}
	v := strings.ToLower(strings.TrimSpace(line))
	if v == "" {
		return def, nil
	}
	if v == "y" || v == "yes" {
		return true, nil
	}
	if v == "n" || v == "no" {
		return false, nil
	}
	return def, nil
}

func promptInt(r *bufio.Reader, out io.Writer, label string, def int) (int, error) {
	v, err := promptString(r, out, label, strconv.Itoa(def))
	if err != nil {
		return 0, err
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid integer %q", v)
	}
	return i, nil
}

func splitAddr(addr string) (string, int, error) {
	parts := strings.Split(addr, ":")
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("invalid address %q (expected host:port)", addr)
	}
	p, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, err
	}
	return parts[0], p, nil
}

func expandPath(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, p[2:])
	}
	return filepath.Abs(p)
}

func resolveBinary(p string) (string, error) {
	if strings.Contains(p, "/") {
		return expandPath(p)
	}
	lp, err := exec.LookPath(p)
	if err != nil {
		return "", fmt.Errorf("binary %q not found in PATH", p)
	}
	return lp, nil
}

func repoRootFromExecutable() string {
	exe, err := os.Executable()
	if err != nil {
		cwd, _ := os.Getwd()
		return cwd
	}
	d := filepath.Dir(exe)
	if filepath.Base(d) == "mount" {
		return filepath.Dir(d)
	}
	if filepath.Base(filepath.Dir(d)) == "mount" {
		return filepath.Dir(filepath.Dir(d))
	}
	cwd, _ := os.Getwd()
	return cwd
}

func defaultRedisBin() string {
	candidate := filepath.Join(os.Getenv("HOME"), "git", "redis", "src", "redis-server")
	if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
		return candidate
	}
	if lp, err := exec.LookPath("redis-server"); err == nil {
		return lp
	}
	return "redis-server"
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}
