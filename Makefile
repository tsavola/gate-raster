GO		?= go

DESTDIR		:=
PREFIX		:= /usr/local
LIBDIR		:= $(PREFIX)/lib/gate

-include config.mk

export GO111MODULE := on

build:
	$(GO) build $(GOBUILDFLAGS) -buildmode=plugin -o lib/gate/plugin/savo.la/gate/raster.so
	$(GO) vet ./...

install:
	install -m 755 -d $(DESTDIR)$(LIBDIR)/plugin/savo.la/gate
	install -m 644 lib/gate/plugin/savo.la/gate/raster.so $(DESTDIR)$(LIBDIR)/plugin/savo.la/gate

clean:
	rm -rf lib

.PHONY: build install clean
