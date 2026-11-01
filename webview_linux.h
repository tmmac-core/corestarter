// CoreStarter's window on Linux: GTK 3 + WebKitGTK 4.1, kept deliberately
// small. Everything runs on the GTK main thread; Go hands work over with
// cs_wake (g_idle_add is thread-safe).
#pragma once

// cs_init initialises GTK; 0 if there is no display.
int cs_init(void);

// cs_window creates the window (hidden). width/height in logical pixels;
// icon is a PNG (may be NULL).
void cs_window(const char *title, int width, int height, const void *icon, int icon_len);

// cs_load shows the window and loads the page from a string.
void cs_load(const char *html);

// cs_run runs the main loop until the window is gone.
void cs_run(void);

// cs_eval runs a script in the page (main thread only).
void cs_eval(const char *js);

// cs_wake makes the main loop call goWake (any thread).
void cs_wake(void);

// cs_close asks the window to close like the title bar's X would (any thread).
void cs_close(void);

// cs_destroy closes the window without asking (main thread only).
void cs_destroy(void);

// cs_present brings the window to the front (main thread only).
void cs_present(void);

// cs_dialog shows a modal message (kind 0 info, 1 warning, 2 error,
// 3 yes/no question); returns 1 for OK/Yes. Main thread, GTK initialised.
int cs_dialog(const char *title, const char *text, int kind);
