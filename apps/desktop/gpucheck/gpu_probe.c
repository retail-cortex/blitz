/*
 * Copyright 2026 Retail Cortex
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// WebKitGTK's report on how it draws (gpu_probe.go): a web view with the
// policy the desktop app gives it loads webkit://gpu, and the page's text
// goes to stdout.

#include "gpu_probe.h"

#include <stdio.h>
#include <webkit2/webkit2.h>

// How long webkit://gpu may take to load and report, in seconds.
#define PROBE_TIMEOUT 30

static int status = 1;

static void got_text(GObject *view, GAsyncResult *res, gpointer data) {
  GError *err = NULL;
  JSCValue *v = webkit_web_view_evaluate_javascript_finish(WEBKIT_WEB_VIEW(view), res, &err);
  if (v == NULL) {
    fprintf(stderr, "gpu_probe: %s\n", err->message);
    g_error_free(err);
  } else {
    char *text = jsc_value_to_string(v);
    printf("%s\n", text);
    g_free(text);
    g_object_unref(v);
    status = 0;
  }
  gtk_main_quit();
}

// read_page reads the report once WebKit has filled it in.
static gboolean read_page(gpointer view) {
  webkit_web_view_evaluate_javascript(view, "document.body.innerText", -1, NULL, NULL, NULL, got_text, NULL);
  return G_SOURCE_REMOVE;
}

static void load_changed(WebKitWebView *view, WebKitLoadEvent ev, gpointer data) {
  if (ev == WEBKIT_LOAD_FINISHED) {
    g_timeout_add(1500, read_page, view);
  }
}

static gboolean timed_out(gpointer data) {
  fprintf(stderr, "gpu_probe: no report after %d s\n", PROBE_TIMEOUT);
  gtk_main_quit();
  return G_SOURCE_REMOVE;
}

int gpu_probe(int on) {
  gtk_init(NULL, NULL);
  GtkWidget *win = gtk_window_new(GTK_WINDOW_TOPLEVEL);
  GtkWidget *view = webkit_web_view_new();
  webkit_settings_set_hardware_acceleration_policy(
      webkit_web_view_get_settings(WEBKIT_WEB_VIEW(view)),
      on ? WEBKIT_HARDWARE_ACCELERATION_POLICY_ON_DEMAND : WEBKIT_HARDWARE_ACCELERATION_POLICY_NEVER);
  gtk_window_set_default_size(GTK_WINDOW(win), 400, 300);
  gtk_container_add(GTK_CONTAINER(win), view);
  gtk_widget_show_all(win);
  g_signal_connect(view, "load-changed", G_CALLBACK(load_changed), NULL);
  g_timeout_add_seconds(PROBE_TIMEOUT, timed_out, NULL);
  webkit_web_view_load_uri(WEBKIT_WEB_VIEW(view), "webkit://gpu");
  gtk_main();
  fflush(stdout);
  return status;
}
