// Cocoa helpers for CoreStarter on macOS (cocoa_darwin.m).
#pragma once

// cs_lang writes the user's first preferred language ("de-DE") into buf; 0 if none.
int cs_lang(char *buf, int len);

// cs_notify_setup asks once for permission to show notifications (bundle only).
void cs_notify_setup(void);

// cs_notify posts a notification (no-op outside an app bundle).
void cs_notify(const char *title, const char *body);

// cs_recycle moves a file or app to the Trash; 1 on success.
int cs_recycle(const char *path);
