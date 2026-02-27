# Redis FS Module - Makefile
# Modeled after Redis vectorsets module build system

uname_S := $(shell sh -c 'uname -s 2>/dev/null || echo not')

CC = cc
CFLAGS = -O2 -Wall -Wextra -Wno-unused-parameter -g $(SAN) -std=c11 -D_POSIX_C_SOURCE=200809L -D_DEFAULT_SOURCE
LDFLAGS = -lm $(SAN)

ifeq ($(uname_S),Linux)
    SHOBJ_CFLAGS ?= -W -Wall -fno-common -g -ggdb -std=c11 -O2
    SHOBJ_LDFLAGS ?= -shared
else
    SHOBJ_CFLAGS ?= -W -Wall -dynamic -fno-common -g -ggdb -std=c11 -O3
    SHOBJ_LDFLAGS ?= -bundle -undefined dynamic_lookup
endif

.SUFFIXES: .c .xo .so

all: fs.so

.c.xo:
	$(CC) -I. $(CFLAGS) $(SHOBJ_CFLAGS) -fPIC -c $< -o $@

fs.xo: fs.c fs.h path.h redismodule.h
path.xo: path.c path.h

fs.so: fs.xo path.xo
	$(CC) -o $@ $^ $(SHOBJ_LDFLAGS) $(LDFLAGS) -lc

clean:
	rm -f *.xo *.so

# E2E test: start Redis with module, run tests, then shut down
TEST_PORT ?= 6399
TEST_PIDFILE = /tmp/redis-fs-test.pid

test: fs.so
	@echo "Starting Redis on port $(TEST_PORT) with fs.so..."
	@redis-server --port $(TEST_PORT) --loadmodule $(PWD)/fs.so \
		--daemonize yes --pidfile $(TEST_PIDFILE) \
		--loglevel warning --save "" --appendonly no
	@sleep 0.5
	@echo "Running tests..."
	@python3 test.py --port $(TEST_PORT); \
		EXIT_CODE=$$?; \
		echo "Stopping Redis..."; \
		kill `cat $(TEST_PIDFILE)` 2>/dev/null || true; \
		rm -f $(TEST_PIDFILE); \
		exit $$EXIT_CODE

.PHONY: all clean test
