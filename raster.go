// Copyright (c) 2019 Timo Savola. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"log"
	"runtime"

	"github.com/tsavola/gate/packet"
	"github.com/tsavola/gate/service"
	"github.com/veandco/go-sdl2/sdl"
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

	inCall  bool
	surface *sdl.Surface
	window  *sdl.Window
	grabbed bool
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
	if !inited {
		runtime.LockOSThread()

		if err := sdl.Init(sdl.INIT_VIDEO | sdl.INIT_EVENTS); err != nil {
			panic(err)
		}

		inited = true
	}

	if inst.surface == nil {
		s, err := sdl.CreateRGBSurfaceWithFormat(0, 320, 200, 32, sdl.PIXELFORMAT_ARGB8888)
		if err == nil {
			inst.surface = s
		} else {
			log.Printf("%s: %v", ServiceName, err)
		}
	}

	if inst.window == nil {
		w, err := sdl.CreateWindow(ServiceName, sdl.WINDOWPOS_UNDEFINED, sdl.WINDOWPOS_UNDEFINED,
			320*initialScale, 200*initialScale, sdl.WINDOW_SHOWN|sdl.WINDOW_RESIZABLE)
		if err == nil {
			inst.window = w
		} else {
			log.Printf("%s: %v", ServiceName, err)
		}
	}

	inst.draw(p)
	inst.handleReply(ctx, send)
}

func (inst *instance) handleReply(ctx context.Context, send chan<- packet.Buf) {
	reply := bytes.NewBuffer(packet.MakeCall(inst.Code, 8)[:packet.HeaderSize])

	if inst.window != nil {
		for {
			event := sdl.PollEvent()
			if event == nil {
				break
			}

			b := make([]byte, 8)

			switch e := event.(type) {
			case *sdl.QuitEvent:
				b[0] = 1

			case *sdl.KeyboardEvent:
				if e.Type == sdl.KEYDOWN {
					if inst.grabbed && e.Keysym.Scancode == sdl.SCANCODE_RALT {
						inst.window.SetGrab(false)
						sdl.SetRelativeMouseMode(false)
						sdl.ShowCursor(sdl.ENABLE)
						inst.grabbed = false
					}
					b[0] = 2
				} else {
					b[0] = 3
				}
				b[1] = byte(e.Keysym.Scancode)

			case *sdl.MouseButtonEvent:
				if e.State == sdl.PRESSED {
					if !inst.grabbed {
						inst.window.SetGrab(true)
						sdl.SetRelativeMouseMode(true)
						sdl.ShowCursor(sdl.DISABLE)
						inst.grabbed = true
						continue
					}
					b[0] = 4
				} else {
					b[0] = 5
				}
				b[1] = e.Button

			case *sdl.MouseMotionEvent:
				if inst.grabbed {
					b[0] = 6
					binary.LittleEndian.PutUint16(b[4:], uint16(e.XRel))
					binary.LittleEndian.PutUint16(b[6:], uint16(e.YRel))
				}
			}

			reply.Write(b)
		}
	}

	select {
	case send <- reply.Bytes():
		inst.inCall = false

	case <-ctx.Done():
		return
	}
}

func (inst *instance) draw(p packet.Buf) {
	if inst.surface == nil || inst.window == nil {
		return
	}

	err := func() (err error) {
		if inst.surface.MustLock() {
			err = inst.surface.Lock()
			if err != nil {
				return
			}
			defer inst.surface.Unlock()
		}

		palette := p.Content()[:3*256]
		pixels := p.Content()[3*256:]
		dest := inst.surface.Pixels()

		for y := int32(0); y < 200; y++ {
			for x := int32(0); x < 320; x++ {
				i := int(pixels[y*320+x])
				dest[y*inst.surface.Pitch+x*4+0] = palette[3*i+2]
				dest[y*inst.surface.Pitch+x*4+1] = palette[3*i+1]
				dest[y*inst.surface.Pitch+x*4+2] = palette[3*i+0]
				dest[y*inst.surface.Pitch+x*4+3] = 255
			}
		}

		return
	}()
	if err == nil {
		if s, err := inst.window.GetSurface(); err == nil {
			err = inst.surface.BlitScaled(nil, s, nil)
			if err == nil {
				err = inst.window.UpdateSurface()
			}
		}
	}

	if err != nil {
		log.Printf("%s: %v", ServiceName, err)
		return
	}
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
	if inst.window != nil {
		inst.window.Destroy()
	}

	if inst.surface != nil {
		inst.surface.Free()
	}
}

func main() {}
