// Copyright (c) 2019 Timo Savola. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"context"
	"os"

	"github.com/tsavola/gate/packet"
	"github.com/tsavola/gate/service"
)

const (
	ServiceName    = "savo.la/gate/raster"
	ServiceVersion = "0"
)

const initialScale = 4

var inited bool

func InitServices(registry *service.Registry) (err error) {
	registry.Register(raster{})
	return
}

type raster struct{}

func (raster) ServiceName() string               { return ServiceName }
func (raster) ServiceVersion() string            { return ServiceVersion }
func (raster) Discoverable(context.Context) bool { return true }

func (raster) CreateInstance(ctx context.Context, config service.InstanceConfig) service.Instance {
	return newInstance(config.Service)
}

func (raster) RestoreInstance(ctx context.Context, config service.InstanceConfig, snapshot []byte,
) (service.Instance, error) {
	inst := newInstance(config.Service)
	if err := inst.restore(snapshot); err != nil {
		return nil, err
	}

	return inst, nil
}

type instance struct {
	packet.Service

	inCall bool
}

func newInstance(config packet.Service) *instance {
	return &instance{
		Service: config,
	}
}

func (inst *instance) restore(snapshot []byte) (err error) {
	if len(snapshot) == 0 {
		return
	}

	flags := snapshot[0]
	inst.inCall = flags&1 != 0
	return
}

func (inst *instance) Resume(ctx context.Context, send chan<- packet.Buf) {
	if inst.inCall {
		inst.handleReply(ctx, send)
	}
}

func (inst *instance) Handle(ctx context.Context, send chan<- packet.Buf, p packet.Buf) {
	if p.Domain() == packet.DomainCall {
		inst.inCall = true
		inst.handleCall(ctx, send, p)
	}
}

func (inst *instance) handleCall(ctx context.Context, send chan<- packet.Buf, p packet.Buf) {
	inst.draw(p)
	inst.handleReply(ctx, send)
}

func (inst *instance) handleReply(ctx context.Context, send chan<- packet.Buf) {
	reply := bytes.NewBuffer(packet.MakeCall(inst.Code, 8)[:packet.HeaderSize])

	select {
	case send <- reply.Bytes():
		inst.inCall = false

	case <-ctx.Done():
		return
	}
}

var shades = []rune{' ', '░', '▒', '█'}

func (inst *instance) draw(p packet.Buf) {
	palette := p.Content()[:3*256]
	pixels := p.Content()[3*256:]
	dest := bytes.NewBuffer(make([]byte, 0, 320*100*3+5))
	dest.WriteString("\033[2J")

	for y := 0; y < 200; y += 2 {
		for x := 0; x < 319; x++ {
			i := int(pixels[y*320+x])
			s := (uint(palette[3*i+2]) + uint(palette[3*i+1]) + uint(palette[3*i+0])) / 192
			dest.WriteRune(shades[s])
		}
		dest.WriteByte('\n')
	}

	os.Stdout.Write(dest.Bytes())
}

func (inst *instance) Suspend() (snapshot []byte) {
	inst.Shutdown()

	if !inst.inCall {
		return
	}

	snapshot = []byte{1}
	return
}

func (inst *instance) Shutdown() {
}

func main() {}
