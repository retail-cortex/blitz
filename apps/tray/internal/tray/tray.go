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
	"net/http"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/socket"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// Status is the service as the tray sees it.
type Status struct {
	// Running means the service answers on its socket.
	Running bool
	// Installed means it starts at login (a launchd agent or systemd unit).
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
	m := Menu{Running: s.Running, Start: Item{Title: i18n.T("tray.start")}, Stop: Item{Title: i18n.T("tray.stop")}, Restart: Item{Title: i18n.T("tray.restart")}}
	switch {
	case s.Running && s.Version != "":
		m.State = Item{Title: i18n.T("tray.state.running_version", "version", s.Version)}
	case s.Running:
		m.State = Item{Title: i18n.T("tray.state.running")}
	case s.Installed:
		m.State = Item{Title: i18n.T("tray.state.stopped")}
	default:
		m.State = Item{Title: i18n.T("tray.state.not_running")}
	}
	m.Tooltip = m.State.Title
	m.Start.Enabled = !s.Running
	m.Stop.Enabled = s.Running
	m.Restart.Enabled = s.Running
	return m
}

// clients are the tray's HTTP clients, one per socket for its whole life.
// A client of its own per call would leave its connection open (the
// service keeps it, idle, as clients may reuse it): asked every 2 s, the
// tray and the service each gained a connection and its goroutines every
// time, without end.
var (
	clientsMu sync.Mutex
	clients   = map[string]*http.Client{}
)

// clientFor is the client for the service at sock.
func clientFor(sock string) *http.Client {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	c, ok := clients[sock]
	if !ok {
		c = socket.Client(sock)
		clients[sock] = c
	}
	return c
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
	c := pb.NewWorkspaceServiceClient(clientFor(sock), socket.BaseURL)
	if res, err := c.GetServiceInfo(ctx, connect.NewRequest(&pb.GetServiceInfoRequest{})); err == nil {
		st.Version, st.PID = res.Msg.Version, int(res.Msg.Pid)
	}
	return st
}

// Shutdown asks the service at sock to stop (WorkspaceService.Shutdown):
// how the tray stops one it can't signal (Windows).
func Shutdown(ctx context.Context, sock string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	c := pb.NewWorkspaceServiceClient(clientFor(sock), socket.BaseURL)
	_, err := c.Shutdown(ctx, connect.NewRequest(&pb.ShutdownRequest{}))
	return err
}

// ServiceLogDir is where the service at sock writes its log, as it says
// (ListLogDays): the log.dir it started with. "" when it isn't running,
// doesn't say, or has its log off.
func ServiceLogDir(ctx context.Context, sock string) string {
	if !socket.Running(sock) {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	c := pb.NewWorkspaceServiceClient(clientFor(sock), socket.BaseURL)
	res, err := c.ListLogDays(ctx, connect.NewRequest(&pb.ListLogDaysRequest{}))
	if err != nil {
		return ""
	}
	return res.Msg.Dir
}
