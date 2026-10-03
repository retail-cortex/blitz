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

// Printing with WebKit on macOS: an offscreen WKWebView in a borderless
// window that's never shown loads the page, then prints it, paginated, to
// a temporary PDF through NSPrintOperation.

#import <AppKit/AppKit.h>
#import <WebKit/WebKit.h>
#include "print.h"

extern void blitzPrintDone(uintptr_t handle, void *data, int len, char *err);

@interface BlitzPDFPrinter : NSObject <WKNavigationDelegate>
@property(nonatomic, strong) NSWindow *window;
@property(nonatomic, strong) WKWebView *web;
@property(nonatomic, strong) NSURL *out;
@property(nonatomic) uintptr_t handle;
@property(nonatomic) NSSize paper;
@property(nonatomic) CGFloat margin;
@property(nonatomic) BOOL loaded;
@end

// The printers at work, kept until they answer.
static NSMutableSet<BlitzPDFPrinter *> *printing;

@implementation BlitzPDFPrinter

- (void)finish:(NSString *)error {
  if (error != nil) {
    blitzPrintDone(self.handle, NULL, 0, (char *)error.UTF8String);
  } else {
    NSData *pdf = [NSData dataWithContentsOfURL:self.out];
    if (pdf == nil || pdf.length == 0) {
      blitzPrintDone(self.handle, NULL, 0, "the PDF wasn't written");
    } else {
      blitzPrintDone(self.handle, (void *)pdf.bytes, (int)pdf.length, NULL);
    }
  }
  [[NSFileManager defaultManager] removeItemAtURL:self.out error:nil];
  self.web.navigationDelegate = nil;
  [self.window close];
  [printing removeObject:self];
}

// The page itself loads; nothing after it (a link, a refresh) does.
- (void)webView:(WKWebView *)webView
    decidePolicyForNavigationAction:(WKNavigationAction *)action
                    decisionHandler:(void (^)(WKNavigationActionPolicy))decide {
  decide(self.loaded ? WKNavigationActionPolicyCancel : WKNavigationActionPolicyAllow);
}

- (void)webView:(WKWebView *)webView didFinishNavigation:(WKNavigation *)navigation {
  if (self.loaded) {
    return;
  }
  self.loaded = YES;
  [self print];
}

- (void)webView:(WKWebView *)webView didFailNavigation:(WKNavigation *)navigation withError:(NSError *)error {
  [self finish:error.localizedDescription];
}

- (void)webView:(WKWebView *)webView
    didFailProvisionalNavigation:(WKNavigation *)navigation
                       withError:(NSError *)error {
  [self finish:error.localizedDescription];
}

- (void)print {
  NSPrintInfo *info = [[NSPrintInfo alloc] initWithDictionary:@{}];
  info.paperSize = self.paper;
  info.topMargin = info.bottomMargin = info.leftMargin = info.rightMargin = self.margin;
  info.horizontalPagination = NSPrintingPaginationModeFit;
  info.verticalPagination = NSPrintingPaginationModeAutomatic;
  info.horizontallyCentered = NO;
  info.verticallyCentered = NO;
  info.jobDisposition = NSPrintSaveJob;
  info.dictionary[NSPrintJobSavingURL] = self.out;
  NSPrintOperation *op = [self.web printOperationWithPrintInfo:info];
  op.showsPrintPanel = NO;
  op.showsProgressPanel = NO;
  // Without a frame, WebKit's print view lays the page out at no size and
  // prints blank pages.
  op.view.frame = NSMakeRect(0, 0, self.paper.width - 2 * self.margin, self.paper.height - 2 * self.margin);
  [op runOperationModalForWindow:self.window
                        delegate:self
                  didRunSelector:@selector(printOperationDidRun:success:contextInfo:)
                     contextInfo:NULL];
}

// AppKit calls this on the print operation's own thread; the window is
// closed on the main one.
- (void)printOperationDidRun:(NSPrintOperation *)op success:(BOOL)success contextInfo:(void *)context {
  dispatch_async(dispatch_get_main_queue(), ^{
    [self finish:success ? nil : @"printing failed"];
  });
}

@end

void blitz_print_pdf(const char *html, double width, double height, double margin, uintptr_t handle) {
  NSString *page = [NSString stringWithUTF8String:html]; // copied: the caller frees html
  dispatch_async(dispatch_get_main_queue(), ^{
    if (printing == nil) {
      printing = [NSMutableSet new];
    }
    BlitzPDFPrinter *p = [BlitzPDFPrinter new];
    p.handle = handle;
    p.paper = NSMakeSize(width, height);
    p.margin = margin;
    NSString *name = [NSString stringWithFormat:@"blitz-%@.pdf", [NSUUID UUID].UUIDString];
    p.out = [NSURL fileURLWithPath:[NSTemporaryDirectory() stringByAppendingPathComponent:name]];
    NSRect frame = NSMakeRect(0, 0, width - 2 * margin, height - 2 * margin);
    p.window = [[NSWindow alloc] initWithContentRect:frame
                                           styleMask:NSWindowStyleMaskBorderless
                                             backing:NSBackingStoreBuffered
                                               defer:NO];
    p.window.releasedWhenClosed = NO;
    WKWebViewConfiguration *config = [WKWebViewConfiguration new];
    config.websiteDataStore = [WKWebsiteDataStore nonPersistentDataStore];
    if (@available(macOS 11.0, *)) {
      config.defaultWebpagePreferences.allowsContentJavaScript = NO;
    }
    p.web = [[WKWebView alloc] initWithFrame:frame configuration:config];
    p.web.navigationDelegate = p;
    p.window.contentView = p.web;
    [printing addObject:p];
    [p.web loadHTMLString:page baseURL:nil];
  });
}

int blitz_print_init(void) {
  return 1;
}

void blitz_print_run_loop(void) {
  [NSApplication sharedApplication];
  [NSApp setActivationPolicy:NSApplicationActivationPolicyProhibited];
  [NSApp run];
}
