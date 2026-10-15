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

// The engine side of printpdf: print_darwin.m (WKWebView) and
// print_linux.c (WebKitGTK). Each answers through blitzPrintDone.

#ifndef BLITZ_PRINT_H
#define BLITZ_PRINT_H

#include <stdint.h>

// BLITZ_PRINT_READY is the body of an async function both printers run
// once the page has loaded, and print after: it waits for the page's
// fonts and every picture to be ready to draw, for at most 10 seconds.
// Printing straight away printed pictures as blank space when a font was
// still loading (KaTeX's, inlined with font-display: block).
#define BLITZ_PRINT_READY                                                                       \
  "await Promise.race(["                                                                       \
  "Promise.all([document.fonts.ready, ...Array.from(document.images, (i) => i.decode().catch(() => {}))]),"\
  "new Promise((r) => setTimeout(r, 10000))]);"                                                 \
  "return true;"

// Prints html on pages of width x height points with margin points on
// each side, on the UI thread, and answers handle through blitzPrintDone.
void blitz_print_pdf(const char *html, double width, double height, double margin, uintptr_t handle);

// Starts the UI toolkit without Wails; 0 when there's no display.
int blitz_print_init(void);

// Runs the UI loop on the calling (main) thread; never returns.
void blitz_print_run_loop(void);

#endif
