all: redis-fs-mount rfs

redis-fs-mount:
	go build -o redis-fs-mount ./cmd/redis-fs-mount

rfs:
	go build -o rfs ./cmd/rfs

clean:
	rm -f redis-fs-mount rfs redis-fs-cli

.PHONY: all clean redis-fs-mount rfs
