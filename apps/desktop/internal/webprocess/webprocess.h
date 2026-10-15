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

#ifndef BLITZ_WEBPROCESS_H
#define BLITZ_WEBPROCESS_H

// Ends every web view's web process, then GTK's main loop, on GTK's
// thread; callable from any thread.
void blitz_quit_without_web_processes(void);

// Ends the web process of every web view in the toplevel windows and
// returns how many views there were. On GTK's thread.
int blitz_end_web_processes(void);

// For the tests: GTK started (1) or no display (0); its main loop; a
// window with a web view showing html (on GTK's thread), and whether that
// view's web process was ended by WebKit's API (1), otherwise (-1) or not
// yet (0); whether the view has loaded.
int blitz_webprocess_init(void);
void blitz_webprocess_run_loop(void);
void blitz_webprocess_open_view(const char *html);
int blitz_webprocess_view_ended(void);
int blitz_webprocess_view_loaded(void);
void blitz_webprocess_end_on_loop(void);

#endif
