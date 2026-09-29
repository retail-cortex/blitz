// Copyright 2026 Retail Cortex
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package tray is the Blitz tray icon's behaviour, apart from the icon
// library: what the service's state is, the menu for it, and what its
// items do. cmd blitz-tray draws it with fyne.io/systray.
package tray

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/socket"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// Status is the service as the tray sees it.
type Status struct {
	// Running: the service answers on its socket.
	Running bool
	// Installed: it starts at login (a launchd agent or systemd unit).
	Installed bool
	// Version and PID are what the running service says (GetServiceInfo).
	Version string
	PID     int
}

// Item is a menu item's state.
type Item struct {
	Title   string
	Enabled bool
}

// Menu is what the tray shows for a status.
type Menu struct {
	Running bool // which icon
	Tooltip string
	// State is the first line: what the service is doing (not clickable).
	State                Item
	Start, Stop, Restart Item
}

// MenuFor is the menu for s: Start when the service is stopped, Stop and
// Restart while it runs.
func MenuFor(s Status) Menu {
	m := Menu{Running: s.Running, Start: Item{Title: "Start the service"}, Stop: Item{Title: "Stop the service"}, Restart: Item{Title: "Restart the service"}}
	switch {
	case s.Running && s.Version != "":
		m.State = Item{Title: fmt.Sprintf("Blitz service: running (%s)", s.Version)}
	case s.Running:
		m.State = Item{Title: "Blitz service: running"}
	case s.Installed:
		m.State = Item{Title: "Blitz service: stopped"}
	default:
		m.State = Item{Title: "Blitz service: not running"}
	}
	m.Tooltip = m.State.Title
	m.Start.Enabled = !s.Running
	m.Stop.Enabled = s.Running
	m.Restart.Enabled = s.Running
	return m
}

// Probe asks the service at sock how it is, within a second or two.
func Probe(ctx context.Context, sock string, installed func() bool) Status {
	st := Status{Installed: installed()}
	if !socket.Running(sock) {
		return st
	}
	st.Running = true
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	c := pb.NewWorkspaceServiceClient(socket.Client(sock), socket.BaseURL)
	if res, err := c.GetServiceInfo(ctx, connect.NewRequest(&pb.GetServiceInfoRequest{})); err == nil {
		st.Version, st.PID = res.Msg.Version, int(res.Msg.Pid)
	}
	return st
}
