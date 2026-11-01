// See webview_darwin.h. The page is the embedded ui/index.html: no network,
// no other documents, no developer tools, nothing kept on disk.

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>

#include "webview_darwin.h"
#include "_cgo_export.h"

// Menu and button labels from Go (texts_darwin.go), in this order.
enum { L_APP, L_QUIT, L_EDIT, L_UNDO, L_REDO, L_CUT, L_COPY, L_PASTE, L_ALL, L_WINDOW, L_MIN, L_CLOSE, L_YES, L_NO, L_COUNT };
static NSString *labels[L_COUNT];

static NSWindow *win;
static WKWebView *view;
static BOOL loaded; // the page started loading; any later navigation is refused
static BOOL alive;
static BOOL quitLater; // a quit (logout, restart) waits for a running save
static NSString *page; // kept to reload the page when WebKit's content process ends
static int reloads;

// The page talks to Go through window.chrome.webview.postMessage, as under
// WebView2; this shim forwards it to WebKit's message handler "bridge". The
// browser's own context menu (Reload, Back) is kept off outside text fields.
static NSString *const shim =
	@"window.chrome = { webview: { postMessage: function (m) {"
	@" window.webkit.messageHandlers.bridge.postMessage(String(m)); } } };"
	@"document.addEventListener('contextmenu', function (e) {"
	@" if (!(e.target.closest && e.target.closest('input, textarea'))) e.preventDefault(); });";

@interface CSDelegate : NSObject <NSWindowDelegate, WKNavigationDelegate, WKUIDelegate, WKScriptMessageHandler, NSApplicationDelegate>
@end

@implementation CSDelegate
- (void)userContentController:(WKUserContentController *)c didReceiveScriptMessage:(WKScriptMessage *)m {
	if (![m.body isKindOfClass:[NSString class]]) return;
	goMessage((char *)[(NSString *)m.body UTF8String]);
}

// Only the one page we load ourselves; links, drops, scripts and redirects
// that would show anything else are refused.
- (void)webView:(WKWebView *)v decidePolicyForNavigationAction:(WKNavigationAction *)a
	decisionHandler:(void (^)(WKNavigationActionPolicy))h {
	if (!loaded && [a.request.URL.absoluteString isEqualToString:@"about:blank"]) {
		loaded = YES;
		h(WKNavigationActionPolicyAllow);
		return;
	}
	h(WKNavigationActionPolicyCancel);
}

// Nothing opens new windows.
- (WKWebView *)webView:(WKWebView *)v createWebViewWithConfiguration:(WKWebViewConfiguration *)c
	forNavigationAction:(WKNavigationAction *)a windowFeatures:(WKWindowFeatures *)f {
	return nil;
}

// The content process ended (memory pressure, crash): the window would stay
// empty. Load the page again; it asks Go for its state once more (init).
- (void)webViewWebContentProcessDidTerminate:(WKWebView *)v {
	goNote("webkit: web process ended, reloading the page");
	if (!alive || !page || ++reloads > 3) return; // a page that kills it every time stays down
	loaded = NO;
	[view loadHTMLString:page baseURL:nil];
}

- (BOOL)windowShouldClose:(NSWindow *)w {
	return goCloseRequested() != 0;
}

- (void)windowWillClose:(NSNotification *)n {
	alive = NO;
	if (quitLater) {
		quitLater = NO;
		[NSApp replyToApplicationShouldTerminate:YES];
	}
	[NSApp stop:nil];
	// stop: takes effect after the next event; post one so cs_run returns now.
	[NSApp postEvent:[NSEvent otherEventWithType:NSEventTypeApplicationDefined location:NSZeroPoint
		modifierFlags:0 timestamp:0 windowNumber:0 context:nil subtype:0 data1:0 data2:0] atStart:YES];
}

// Quit (menu, Cmd+Q, the Dock) closes the window like its button: the
// background part keeps running, and a running save is waited for.
- (void)closeWindow:(id)sender {
	if (alive) [win performClose:nil];
}

// A quit from outside (logout, restart, shut down, the Dock) must never be
// refused, or macOS cancels the logout. The window closes and the process
// ends; the background part keeps running. A running save is waited for: the
// close follows once it is done (closeAsked), and with it the quit.
- (NSApplicationTerminateReply)applicationShouldTerminate:(NSApplication *)app {
	if (!alive) return NSTerminateNow;
	if (goCloseRequested() != 0) {
		[win close];
		return NSTerminateNow;
	}
	quitLater = YES;
	// A save that does not end must not hold up the logout: quit after 20 s anyway.
	dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 20 * NSEC_PER_SEC), dispatch_get_main_queue(), ^{
		if (quitLater) {
			quitLater = NO;
			[NSApp replyToApplicationShouldTerminate:YES];
		}
	});
	return NSTerminateLater;
}

// A click on the Dock icon brings the window back.
- (BOOL)applicationShouldHandleReopen:(NSApplication *)app hasVisibleWindows:(BOOL)visible {
	if (alive) [win makeKeyAndOrderFront:nil];
	return NO;
}
@end

static CSDelegate *delegate;

static NSMenuItem *item(NSMenu *m, NSString *title, SEL action, NSString *key) {
	return [m addItemWithTitle:title action:action keyEquivalent:key];
}

// bringToFront makes this app the active one. A process that only shows a
// dialog (no window, no menu bar app) first finishes launching, else macOS
// leaves the dialog behind the app that was active. Not the cooperative
// [NSApp activate] of macOS 14: a window opened from the menu bar helper or a
// dialog of a freshly opened app would then stay in the background.
static BOOL launched;

static void bringToFront(void) {
	if (!launched && ![NSApp isRunning] && [NSApp delegate] == nil) {
		[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
		[NSApp finishLaunching];
		launched = YES;
	}
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
	[NSApp activateIgnoringOtherApps:YES];
#pragma clang diagnostic pop
}

void cs_labels(const char *const *l) {
	for (int i = 0; i < L_COUNT; i++) labels[i] = [NSString stringWithUTF8String:l[i]];
}

int cs_init(const char *const *l) {
	@autoreleasepool {
		cs_labels(l);
		[NSApplication sharedApplication];
		[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
		if (!delegate) delegate = [CSDelegate new];
		[NSApp setDelegate:delegate];

		// Standard menus: without Edit, Cmd+C/V/X/A do nothing in a WKWebView.
		NSMenu *bar = [NSMenu new];
		NSMenu *app = [NSMenu new];
		[item(app, labels[L_QUIT], @selector(closeWindow:), @"q") setTarget:delegate];
		[[bar addItemWithTitle:@"" action:nil keyEquivalent:@""] setSubmenu:app];
		NSMenu *edit = [[NSMenu alloc] initWithTitle:labels[L_EDIT]];
		item(edit, labels[L_UNDO], @selector(undo:), @"z");
		[item(edit, labels[L_REDO], @selector(redo:), @"z") setKeyEquivalentModifierMask:NSEventModifierFlagCommand | NSEventModifierFlagShift];
		[edit addItem:[NSMenuItem separatorItem]];
		item(edit, labels[L_CUT], @selector(cut:), @"x");
		item(edit, labels[L_COPY], @selector(copy:), @"c");
		item(edit, labels[L_PASTE], @selector(paste:), @"v");
		item(edit, labels[L_ALL], @selector(selectAll:), @"a");
		[[bar addItemWithTitle:labels[L_EDIT] action:nil keyEquivalent:@""] setSubmenu:edit];
		NSMenu *wm = [[NSMenu alloc] initWithTitle:labels[L_WINDOW]];
		item(wm, labels[L_MIN], @selector(performMiniaturize:), @"m");
		item(wm, labels[L_CLOSE], @selector(performClose:), @"w");
		[[bar addItemWithTitle:labels[L_WINDOW] action:nil keyEquivalent:@""] setSubmenu:wm];
		[NSApp setMainMenu:bar];
		[NSApp setWindowsMenu:wm];
		return 1;
	}
}

void cs_window(const char *title, int width, int height, const void *icon, int icon_len) {
	@autoreleasepool {
		loaded = NO; // a new view loads its page once more (tests open several)
		// Fit the visible screen (a small MacBook with large text would hide "Save").
		NSRect vis = [[NSScreen mainScreen] visibleFrame];
		if (vis.size.height > 0 && height > vis.size.height - 40) height = vis.size.height - 40;
		if (vis.size.width > 0 && width > vis.size.width) width = vis.size.width;
		win = [[NSWindow alloc] initWithContentRect:NSMakeRect(0, 0, width, height)
			styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskMiniaturizable | NSWindowStyleMaskResizable
			backing:NSBackingStoreBuffered defer:NO];
		win.releasedWhenClosed = NO;
		win.title = [NSString stringWithUTF8String:title];
		win.delegate = delegate;
		win.minSize = NSMakeSize(420, 480);
		// Brand canvas until the page draws: never a white window.
		NSColor *canvas = [NSColor colorWithSRGBRed:3 / 255.0 green:5 / 255.0 blue:4 / 255.0 alpha:1];
		win.backgroundColor = canvas;
		win.appearance = [NSAppearance appearanceNamed:NSAppearanceNameDarkAqua]; // dark title bar over the dark page
		[win center];
		// Outside an app bundle (tests) the Dock shows a generic icon otherwise.
		if (icon && icon_len > 0 && ![[NSBundle mainBundle] bundleIdentifier]) {
			NSImage *img = [[NSImage alloc] initWithData:[NSData dataWithBytes:icon length:icon_len]];
			if (img) [NSApp setApplicationIconImage:img];
		}

		WKWebViewConfiguration *cfg = [WKWebViewConfiguration new];
		cfg.websiteDataStore = [WKWebsiteDataStore nonPersistentDataStore];
		WKUserContentController *ucc = [WKUserContentController new];
		[ucc addUserScript:[[WKUserScript alloc] initWithSource:shim
			injectionTime:WKUserScriptInjectionTimeAtDocumentStart forMainFrameOnly:YES]];
		[ucc addScriptMessageHandler:delegate name:@"bridge"];
		cfg.userContentController = ucc;
		cfg.preferences.javaScriptCanOpenWindowsAutomatically = NO;
		cfg.mediaTypesRequiringUserActionForPlayback = WKAudiovisualMediaTypeAll;
		view = [[WKWebView alloc] initWithFrame:win.contentView.bounds configuration:cfg];
		view.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
		view.navigationDelegate = delegate;
		view.UIDelegate = delegate;
		view.inspectable = NO;
		view.allowsBackForwardNavigationGestures = NO;
		view.allowsMagnification = NO;
		[view setValue:@NO forKey:@"drawsBackground"]; // the window's canvas shows until the page draws
		view.underPageBackgroundColor = canvas;
		[view unregisterDraggedTypes]; // nothing dropped onto the window gets opened
		[win.contentView addSubview:view];
		alive = YES;
	}
}

void cs_load(const char *html) {
	@autoreleasepool {
		[win makeKeyAndOrderFront:nil];
		bringToFront();
		page = [NSString stringWithUTF8String:html];
		[view loadHTMLString:page baseURL:nil]; // nil base: about:blank
	}
}

void cs_run(void) {
	if (alive) [NSApp run];
}

void cs_eval(const char *js) {
	if (!alive) return;
	[view evaluateJavaScript:[NSString stringWithUTF8String:js] completionHandler:nil];
}

void cs_wake(void) {
	dispatch_async(dispatch_get_main_queue(), ^{ goWake(); });
}

void cs_close(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if (alive) [win performClose:nil]; // goes through windowShouldClose
	});
}

void cs_destroy(void) {
	if (alive) [win close];
}

void cs_present(void) {
	if (!alive) return;
	if (win.miniaturized) [win deminiaturize:nil];
	[win makeKeyAndOrderFront:nil];
	bringToFront();
}

static int dialog(NSString *title, NSString *text, int kind) {
	NSAlert *a = [NSAlert new];
	a.messageText = title;
	a.informativeText = text;
	a.alertStyle = kind == 2 ? NSAlertStyleCritical : kind == 1 ? NSAlertStyleWarning : NSAlertStyleInformational;
	if (kind == 3) {
		[a addButtonWithTitle:labels[L_YES] ?: @"OK"];
		[a addButtonWithTitle:labels[L_NO] ?: @"Cancel"];
	}
	bringToFront();
	return [a runModal] == NSAlertFirstButtonReturn;
}

int cs_dialog(const char *title, const char *text, int kind) {
	NSString *t = [NSString stringWithUTF8String:title], *s = [NSString stringWithUTF8String:text];
	[NSApplication sharedApplication];
	if ([NSThread isMainThread]) return dialog(t, s, kind);
	__block int r = 0;
	dispatch_sync(dispatch_get_main_queue(), ^{ r = dialog(t, s, kind); });
	return r;
}
