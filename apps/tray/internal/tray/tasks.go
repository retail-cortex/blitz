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

package tray

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/url"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/socket"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// FetchTasks asks the service for the human tasks workflows wait on, in
// every workspace (timers aren't the user's to answer), and how the user
// wants to hear of them ([ui] task_alerts: notify, open or off).
func FetchTasks(ctx context.Context, sock string) ([]*pb.HumanTask, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	r, err := pb.NewWorkspaceServiceClient(clientFor(sock), socket.BaseURL).ListHumanTasks(ctx, connect.NewRequest(&pb.ListHumanTasksRequest{}))
	if err != nil {
		return nil, "", err
	}
	var out []*pb.HumanTask
	for _, t := range r.Msg.Tasks {
		if t.Kind == "human_task" {
			out = append(out, t)
		}
	}
	return out, r.Msg.Alerts, nil
}

// Arrived returns the tasks not in seen, and makes seen the tasks now
// waiting (an answered task is forgotten).
func Arrived(seen map[string]bool, tasks []*pb.HumanTask) []*pb.HumanTask {
	var out []*pb.HumanTask
	now := map[string]bool{}
	for _, t := range tasks {
		now[t.Id] = true
		if !seen[t.Id] {
			out = append(out, t)
		}
	}
	clear(seen)
	for id := range now {
		seen[id] = true
	}
	return out
}

// TasksLink is the link that tells the desktop app tasks have arrived
// (spec_electron_038 EL-110): blitz://tasks opens its tasks window (or
// its dialog, when the app is open), with notify=1 a notification first,
// titled and worded here in the user's language. "" when alerts are off
// or nothing arrived.
func TasksLink(alerts string, arrived []*pb.HumanTask) string {
	if alerts == "off" || len(arrived) == 0 {
		return ""
	}
	if alerts == "open" {
		return "blitz://tasks"
	}
	q := url.Values{}
	q.Set("notify", "1")
	t := arrived[0]
	q.Set("title", i18n.T("tray.tasks.notify_title", "workflow", t.Workflow))
	if len(arrived) == 1 {
		q.Set("body", t.Title)
	} else {
		q.Set("body", i18n.N("tray.tasks.notify_more", len(arrived)-1, "title", t.Title))
	}
	return "blitz://tasks?" + q.Encode()
}

// TasksTitle is the tray menu's item for the waiting tasks.
func TasksTitle(n int) string { return i18n.N("tray.tasks.waiting", n) }

// dotColour is the waiting tasks' dot on the icon.
var dotColour = color.NRGBA{R: 0xE5, G: 0x48, B: 0x4D, A: 0xFF}

// WithDot draws a dot at a PNG icon's top right, for tasks waiting.
func WithDot(icon []byte) ([]byte, error) {
	src, err := png.Decode(bytes.NewReader(icon))
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	img := image.NewNRGBA(b)
	draw.Draw(img, b, src, b.Min, draw.Src)
	r := b.Dx() / 5
	cx, cy := b.Max.X-r-1, b.Min.Y+r+1
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			if dx, dy := x-cx, y-cy; dx*dx+dy*dy <= r*r {
				img.SetNRGBA(x, y, dotColour)
			}
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
