.PHONY: all module mount clean test

all: module mount

module:
	$(MAKE) -C module

mount:
	$(MAKE) -C mount

clean:
	$(MAKE) -C module clean
	$(MAKE) -C mount clean
	$(RM) fs.so fs.xo path.xo

test: module
	$(MAKE) -C module test
