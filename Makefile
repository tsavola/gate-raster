GO		?= go

DESTDIR		:=
PREFIX		:= /usr/local
LIBDIR		:= $(PREFIX)/lib/gate

-include config.mk

export GO111MODULE := on

build:
	$(GO) build -trimpath $(GOBUILDFLAGS) -buildmode=plugin -o lib/gate/plugin/raster.so
	$(GO) vet ./...

install:
	install -m 755 -d $(DESTDIR)$(LIBDIR)/plugin
	install -m 644 lib/gate/plugin/raster.so $(DESTDIR)$(LIBDIR)/plugin/

clean:
	rm -rf lib

.PHONY: build install clean
