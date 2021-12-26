// Copyright (c) 2019 Timo Savola. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package raster

import (
	"bytes"
	"context"
	"encoding/binary"
	"log"
	"runtime"
	"sync"

	"gate.computer/gate/packet"
	"gate.computer/gate/service"
	"github.com/veandco/go-sdl2/sdl"
)

const (
	extName         = "raster"
	serviceName     = "savo.la/gate/raster"
	serviceRevision = "0"
)

const initialScale = 4

var Ext = service.Extend(extName, nil, func(ctx context.Context, r *service.Registry) error {
	return r.Register(raster{})
})

type raster struct{}

func (raster) Properties() service.Properties {
	return service.Properties{
		Service: service.Service{
			Name:     serviceName,
			Revision: serviceRevision,
		},
	}
}

func (raster) Discoverable(context.Context) bool {
	return true
}

func (raster) CreateInstance(ctx context.Context, config service.InstanceConfig, snapshot []byte,
) (service.Instance, error) {
	return newInstance(config.Service), nil
}

type instance struct {
	service.InstanceBase

	packet.Service

	surface *sdl.Surface
	window  *sdl.Window
	grabbed bool
}

func newInstance(config packet.Service) *instance {
	return &instance{
		Service: config,
	}
}

func (inst *instance) Handle(ctx context.Context, send chan<- packet.Thunk, p packet.Buf) (packet.Buf, error) {
	if p.Domain() != packet.DomainCall {
		return nil, nil
	}

	initRun()

	if inst.surface == nil {
		do(func() {
			s, err := sdl.CreateRGBSurfaceWithFormat(0, 320, 200, 32, sdl.PIXELFORMAT_ARGB8888)
			if err == nil {
				inst.surface = s
			} else {
				log.Printf("%s: %v", serviceName, err)
			}
		})
	}

	if inst.window == nil {
		do(func() {
			w, err := sdl.CreateWindow(serviceName, sdl.WINDOWPOS_UNDEFINED, sdl.WINDOWPOS_UNDEFINED,
				320*initialScale, 200*initialScale, sdl.WINDOW_SHOWN|sdl.WINDOW_RESIZABLE)
			if err == nil {
				subscribeWindowEvents(w)
				inst.window = w
			} else {
				log.Printf("%s: %v", serviceName, err)
			}
		})
	}

	inst.draw(p)

	reply := bytes.NewBuffer(packet.MakeCall(inst.Code, 8)[:packet.HeaderSize])

	if inst.window != nil {
		do(func() {
			for {
				event := pollWindowEvent(inst.window)
				if event == nil {
					break
				}

				b := make([]byte, 8)

				switch e := event.(type) {
				case *sdl.WindowEvent:
					if e.Event == sdl.WINDOWEVENT_CLOSE {
						b[0] = 1
					}

				case *sdl.KeyboardEvent:
					if e.Type == sdl.KEYDOWN {
						if e.Keysym.Scancode == sdl.SCANCODE_RALT {
							if inst.grabbed {
								inst.window.SetGrab(false)
								sdl.SetRelativeMouseMode(false)
								sdl.ShowCursor(sdl.ENABLE)
								inst.grabbed = false
							}
						} else {
							b[0] = 2
						}
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

				default:
					continue
				}

				reply.Write(b)
			}
		})
	}

	return reply.Bytes(), nil
}

func (inst *instance) draw(p packet.Buf) {
	if inst.surface == nil || inst.window == nil {
		return
	}

	do(func() {
		err := func() error {
			if inst.surface.MustLock() {
				if err := inst.surface.Lock(); err != nil {
					return err
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

			return nil
		}()
		if err != nil {
			log.Printf("%s: %v", serviceName, err)
			return
		}

		s, err := inst.window.GetSurface()
		if err != nil {
			log.Printf("%s: %v", serviceName, err)
			return
		}

		if err := inst.surface.BlitScaled(nil, s, nil); err != nil {
			log.Printf("%s: %v", serviceName, err)
			return
		}

		if err := inst.window.UpdateSurface(); err != nil {
			log.Printf("%s: %v", serviceName, err)
			return
		}
	})
}

func (inst *instance) Shutdown(ctx context.Context, suspend bool) ([]byte, error) {
	if inst.window != nil {
		do(func() {
			unsubscribeWindowEvents(inst.window)
			inst.window.Destroy()
			inst.window = nil
		})
	}

	if inst.surface != nil {
		do(func() {
			inst.surface.Free()
			inst.surface = nil
		})
	}

	return nil, nil
}

type task struct {
	f func()
	c chan struct{}
}

func do(f func()) {
	c := make(chan struct{})
	tasks <- task{f, c}
	<-c
}

func initRun() {
	runInit.Do(func() {
		go runTasks()
	})
}

func runTasks() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := sdl.Init(sdl.INIT_VIDEO | sdl.INIT_EVENTS); err != nil {
		panic(err)
	}
	defer sdl.Quit()

	for x := range tasks {
		for {
			event := sdl.PollEvent()
			if event == nil {
				break
			}

			var id uint32

			switch e := event.(type) {
			case *sdl.WindowEvent:
				id = e.WindowID
				clone := *e
				event = &clone

			case *sdl.KeyboardEvent:
				id = e.WindowID
				clone := *e
				event = &clone

			case *sdl.MouseButtonEvent:
				id = e.WindowID
				clone := *e
				event = &clone

			case *sdl.MouseMotionEvent:
				id = e.WindowID
				clone := *e
				event = &clone

			default:
				event = nil
			}

			if event != nil {
				if queue, ok := windowEvents[id]; ok {
					windowEvents[id] = append(queue, event)
				}
			}
		}

		x.f()
		close(x.c)
	}
}

func subscribeWindowEvents(w *sdl.Window) {
	id, err := w.GetID()
	if err != nil {
		panic(err)
	}

	windowEvents[id] = nil
}

func unsubscribeWindowEvents(w *sdl.Window) {
	id, err := w.GetID()
	if err != nil {
		panic(err)
	}

	delete(windowEvents, id)
}

func pollWindowEvent(w *sdl.Window) (e sdl.Event) {
	id, err := w.GetID()
	if err != nil {
		panic(err)
	}

	queue, ok := windowEvents[id]
	if !ok {
		panic(w)
	}
	if len(queue) == 0 {
		return nil
	}

	e = queue[0]
	windowEvents[id] = queue[1:]
	return
}

var runInit sync.Once
var tasks = make(chan task)
var windowEvents = make(map[uint32][]sdl.Event)
