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

// Printing with WebKitGTK on Linux: an offscreen WebKitWebView loads the
// page, then WebKitPrintOperation prints it, paginated, through GTK's
// "Print to File" printer to a temporary PDF.

#include <gtk/gtk.h>
#include <glib/gstdio.h>
#include <unistd.h>
#include <webkit2/webkit2.h>
#include "print.h"

extern void blitzPrintDone(uintptr_t handle, void *data, int len, char *err);

typedef struct {
  char *html;
  double width, height, margin;
  uintptr_t handle;
  char *path;
  GtkWidget *window;
  WebKitWebView *view;
  gboolean loaded;
  char *error;
} job;

static void finish(job *j) {
  if (j->error != NULL) {
    blitzPrintDone(j->handle, NULL, 0, j->error);
  } else {
    gchar *data = NULL;
    gsize len = 0;
    GError *err = NULL;
    if (g_file_get_contents(j->path, &data, &len, &err) && len > 0) {
      blitzPrintDone(j->handle, data, (int)len, NULL);
    } else {
      blitzPrintDone(j->handle, NULL, 0, err != NULL ? err->message : "the PDF wasn't written");
    }
    g_free(data);
    g_clear_error(&err);
  }
  g_unlink(j->path);
  // The view may outlive the window for a moment (it's in the middle of a
  // signal when a load fails): it mustn't call back into the freed job.
  g_signal_handlers_disconnect_by_data(j->view, j);
  gtk_widget_destroy(j->window);
  g_free(j->path);
  g_free(j->html);
  g_free(j->error);
  g_free(j);
}

static void print_failed(WebKitPrintOperation *op, GError *err, gpointer data) {
  job *j = data;
  if (j->error == NULL) {
    j->error = g_strdup(err->message);
  }
}

// "finished" follows "failed" too, so the job ends here either way.
static void print_finished(WebKitPrintOperation *op, gpointer data) {
  finish(data);
  g_object_unref(op);
}

static void print(job *j, WebKitWebView *view) {
  GtkPrintSettings *settings = gtk_print_settings_new();
  // GTK's "Print to File" printer, by the name GTK gives it (translated).
  gtk_print_settings_set_printer(settings, g_dgettext("gtk30", "Print to File"));
  gtk_print_settings_set(settings, GTK_PRINT_SETTINGS_OUTPUT_FILE_FORMAT, "pdf");
  gchar *uri = g_filename_to_uri(j->path, NULL, NULL);
  gtk_print_settings_set(settings, GTK_PRINT_SETTINGS_OUTPUT_URI, uri);
  g_free(uri);

  GtkPageSetup *setup = gtk_page_setup_new();
  GtkPaperSize *paper = gtk_paper_size_new_custom("blitz", "Blitz", j->width, j->height, GTK_UNIT_POINTS);
  gtk_page_setup_set_paper_size(setup, paper);
  gtk_page_setup_set_top_margin(setup, j->margin, GTK_UNIT_POINTS);
  gtk_page_setup_set_bottom_margin(setup, j->margin, GTK_UNIT_POINTS);
  gtk_page_setup_set_left_margin(setup, j->margin, GTK_UNIT_POINTS);
  gtk_page_setup_set_right_margin(setup, j->margin, GTK_UNIT_POINTS);

  WebKitPrintOperation *op = webkit_print_operation_new(view);
  webkit_print_operation_set_print_settings(op, settings);
  webkit_print_operation_set_page_setup(op, setup);
  g_signal_connect(op, "failed", G_CALLBACK(print_failed), j);
  g_signal_connect(op, "finished", G_CALLBACK(print_finished), j);
  webkit_print_operation_print(op);
  g_object_unref(settings);
  g_object_unref(setup);
  gtk_paper_size_free(paper);
}

static void load_changed(WebKitWebView *view, WebKitLoadEvent event, gpointer data) {
  job *j = data;
  if (event != WEBKIT_LOAD_FINISHED || j->loaded || j->error != NULL) {
    return;
  }
  j->loaded = TRUE;
  print(j, view);
}

static gboolean load_failed(WebKitWebView *view, WebKitLoadEvent event, gchar *uri, GError *err, gpointer data) {
  job *j = data;
  if (!j->loaded && j->error == NULL) {
    j->loaded = TRUE;
    j->error = g_strdup(err->message);
    finish(j);
  }
  return TRUE;
}

// The page itself loads; nothing after it (a link, a refresh) does.
static gboolean decide_policy(WebKitWebView *view, WebKitPolicyDecision *decision, WebKitPolicyDecisionType type,
                              gpointer data) {
  job *j = data;
  if (j->loaded && type != WEBKIT_POLICY_DECISION_TYPE_RESPONSE) {
    webkit_policy_decision_ignore(decision);
    return TRUE;
  }
  return FALSE;
}

static gboolean start(gpointer data) {
  job *j = data;
  j->window = gtk_offscreen_window_new();
  // The view keeps its context; ours is given up, or every print would
  // leave a context (and its web process's state) behind.
  WebKitWebContext *context = webkit_web_context_new_ephemeral();
  WebKitWebView *view = WEBKIT_WEB_VIEW(webkit_web_view_new_with_context(context));
  g_object_unref(context);
  j->view = view;
  webkit_settings_set_enable_javascript(webkit_web_view_get_settings(view), FALSE);
  gtk_widget_set_size_request(GTK_WIDGET(view), (int)(j->width - 2 * j->margin), (int)(j->height - 2 * j->margin));
  gtk_container_add(GTK_CONTAINER(j->window), GTK_WIDGET(view));
  gtk_widget_show_all(j->window);
  g_signal_connect(view, "load-changed", G_CALLBACK(load_changed), j);
  g_signal_connect(view, "load-failed", G_CALLBACK(load_failed), j);
  g_signal_connect(view, "decide-policy", G_CALLBACK(decide_policy), j);
  webkit_web_view_load_html(view, j->html, NULL);
  return G_SOURCE_REMOVE;
}

void blitz_print_pdf(const char *html, double width, double height, double margin, uintptr_t handle) {
  job *j = g_new0(job, 1);
  j->html = g_strdup(html); // copied: the caller frees html
  j->width = width;
  j->height = height;
  j->margin = margin;
  j->handle = handle;
  gint fd = g_file_open_tmp("blitz-XXXXXX.pdf", &j->path, NULL);
  if (fd < 0) {
    blitzPrintDone(handle, NULL, 0, "can't make a temporary file");
    g_free(j->html);
    g_free(j);
    return;
  }
  close(fd);
  g_idle_add(start, j); // on GTK's thread
}

int blitz_print_init(void) {
  return gtk_init_check(NULL, NULL) ? 1 : 0;
}

void blitz_print_run_loop(void) {
  gtk_main();
}
