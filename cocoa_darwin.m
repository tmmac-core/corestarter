// Small Cocoa helpers for CoreStarter on macOS (see cocoa_darwin.h).

#import <Cocoa/Cocoa.h>
#import <UserNotifications/UserNotifications.h>

#include "cocoa_darwin.h"

int cs_lang(char *buf, int len) {
	@autoreleasepool {
		NSArray<NSString *> *langs = [NSLocale preferredLanguages];
		if (langs.count == 0) return 0;
		const char *s = [langs[0] UTF8String];
		if (!s || (int)strlen(s) >= len) return 0;
		strcpy(buf, s);
		return 1;
	}
}

// Notifications need a bundle id (the helper app has one; tests do not).
static BOOL canNotify(void) { return [[NSBundle mainBundle] bundleIdentifier] != nil; }

void cs_notify_setup(void) {
	if (!canNotify()) return;
	[[UNUserNotificationCenter currentNotificationCenter]
		requestAuthorizationWithOptions:UNAuthorizationOptionAlert
		completionHandler:^(BOOL granted, NSError *e) {}];
}

void cs_notify(const char *title, const char *body) {
	if (!canNotify()) return;
	@autoreleasepool {
		UNMutableNotificationContent *c = [UNMutableNotificationContent new];
		c.title = [NSString stringWithUTF8String:title];
		c.body = [NSString stringWithUTF8String:body];
		UNNotificationRequest *r = [UNNotificationRequest requestWithIdentifier:[[NSUUID UUID] UUIDString]
			content:c trigger:nil];
		[[UNUserNotificationCenter currentNotificationCenter] addNotificationRequest:r withCompletionHandler:nil];
	}
}

int cs_recycle(const char *path) {
	@autoreleasepool {
		NSURL *url = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path]];
		__block int ok = -1;
		dispatch_semaphore_t done = dispatch_semaphore_create(0);
		[[NSWorkspace sharedWorkspace] recycleURLs:@[url] completionHandler:^(NSDictionary *moved, NSError *e) {
			ok = e == nil;
			dispatch_semaphore_signal(done);
		}];
		dispatch_semaphore_wait(done, dispatch_time(DISPATCH_TIME_NOW, 30 * NSEC_PER_SEC));
		return ok == 1;
	}
}
