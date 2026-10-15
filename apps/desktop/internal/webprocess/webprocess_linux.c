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

// Quitting with the web processes ended first: WebKitGTK kills a web
// process it's asked to end, so the process runs no exit handlers (where
// NVIDIA's EGL crashes it).

#include <gtk/gtk.h>
#include <webkit2/webkit2.h>
#include "webprocess.h"

static void end_in(GtkWidget *widget, gpointer count) {
  if (WEBKIT_IS_WEB_VIEW(widget)) {
    webkit_web_view_terminate_web_process(WEBKIT_WEB_VIEW(widget));
    (*(int *)count)++;
  } else if (GTK_IS_CONTAINER(widget)) {
    gtk_container_forall(GTK_CONTAINER(widget), end_in, count);
  }
}

int blitz_end_web_processes(void) {
  int count = 0;
  GList *windows = gtk_window_list_toplevels();
  for (GList *w = windows; w != NULL; w = w->next) {
    end_in(GTK_WIDGET(w->data), &count);
  }
  g_list_free(windows);
  return count;
}

static gboolean end_and_quit(gpointer data) {
  blitz_end_web_processes();
  gtk_main_quit();
  return G_SOURCE_REMOVE;
}

void blitz_quit_without_web_processes(void) {
  g_idle_add(end_and_quit, NULL);
}

// The tests' web view.

static volatile gint view_ended, view_loaded;

int blitz_webprocess_init(void) {
  return gtk_init_check(NULL, NULL) ? 1 : 0;
}

void blitz_webprocess_run_loop(void) {
  gtk_main();
}

static void on_terminated(WebKitWebView *view, WebKitWebProcessTerminationReason reason, gpointer data) {
  g_atomic_int_set(&view_ended, reason == WEBKIT_WEB_PROCESS_TERMINATED_BY_API ? 1 : -1);
}

static void on_load(WebKitWebView *view, WebKitLoadEvent event, gpointer data) {
  if (event == WEBKIT_LOAD_FINISHED) {
    g_atomic_int_set(&view_loaded, 1);
  }
}

static gboolean open_view(gpointer html) {
  GtkWidget *window = gtk_offscreen_window_new();
  GtkWidget *view = webkit_web_view_new();
  g_signal_connect(view, "web-process-terminated", G_CALLBACK(on_terminated), NULL);
  g_signal_connect(view, "load-changed", G_CALLBACK(on_load), NULL);
  gtk_container_add(GTK_CONTAINER(window), view);
  gtk_widget_show_all(window);
  webkit_web_view_load_html(WEBKIT_WEB_VIEW(view), html, NULL);
  g_free(html);
  return G_SOURCE_REMOVE;
}

void blitz_webprocess_open_view(const char *html) {
  g_atomic_int_set(&view_ended, 0);
  g_atomic_int_set(&view_loaded, 0);
  g_idle_add(open_view, g_strdup(html));
}

int blitz_webprocess_view_ended(void) {
  return g_atomic_int_get(&view_ended);
}

int blitz_webprocess_view_loaded(void) {
  return g_atomic_int_get(&view_loaded);
}

static gboolean end_on_loop(gpointer data) {
  blitz_end_web_processes();
  return G_SOURCE_REMOVE;
}

void blitz_webprocess_end_on_loop(void) {
  g_idle_add(end_on_loop, NULL);
}
