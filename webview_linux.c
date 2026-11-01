// See webview_linux.h. The page is the embedded ui/index.html: no network,
// no other documents, no developer tools, nothing kept on disk.

#include <gtk/gtk.h>
#include <webkit2/webkit2.h>
#include <string.h>

#include "webview_linux.h"
#include "_cgo_export.h"

static GtkWidget *win;
static WebKitWebView *view;
static gboolean loaded;  // the page started loading; any later navigation is refused
static gboolean alive;
static char *page;       // kept for a reload after the web process ended
static int reloads;

// The page talks to Go through window.chrome.webview.postMessage, as under
// WebView2; this shim forwards it to WebKit's message handler "bridge".
static const char *shim =
	"window.chrome = { webview: { postMessage: function (m) {"
	" window.webkit.messageHandlers.bridge.postMessage(String(m)); } } };";

static void on_message(WebKitUserContentManager *m, WebKitJavascriptResult *r, gpointer d) {
	JSCValue *v = webkit_javascript_result_get_js_value(r);
	char *s = jsc_value_to_string(v);
	goMessage(s);
	g_free(s);
}

// Only the one page we load ourselves; links, drops, scripts and redirects
// that would show anything else are refused, and nothing opens new windows.
static gboolean on_decide(WebKitWebView *v, WebKitPolicyDecision *d, WebKitPolicyDecisionType type, gpointer u) {
	if (type == WEBKIT_POLICY_DECISION_TYPE_NAVIGATION_ACTION) {
		WebKitNavigationAction *a = webkit_navigation_policy_decision_get_navigation_action(WEBKIT_NAVIGATION_POLICY_DECISION(d));
		const char *uri = webkit_uri_request_get_uri(webkit_navigation_action_get_request(a));
		if (!loaded && uri && strcmp(uri, "about:blank") == 0) {
			loaded = TRUE;
			webkit_policy_decision_use(d);
		} else {
			webkit_policy_decision_ignore(d);
		}
		return TRUE;
	}
	if (type == WEBKIT_POLICY_DECISION_TYPE_NEW_WINDOW_ACTION) {
		webkit_policy_decision_ignore(d);
		return TRUE;
	}
	return FALSE;
}

static gboolean on_context_menu(WebKitWebView *v, WebKitContextMenu *m, GdkEvent *e, WebKitHitTestResult *h, gpointer u) {
	return TRUE; // no menu (it would offer "Inspect", "Reload", "Back")
}

static gboolean on_delete(GtkWidget *w, GdkEvent *e, gpointer u) {
	return goCloseRequested() ? FALSE : TRUE; // TRUE keeps the window
}

static void on_destroy(GtkWidget *w, gpointer u) {
	alive = FALSE;
	gtk_main_quit();
}

// The web process ended (crash, memory): the window would stay empty. Load
// the page again; it asks Go for its state once more (init).
static void on_crash(WebKitWebView *v, WebKitWebProcessTerminationReason r, gpointer u) {
	goNote((char *)(r == WEBKIT_WEB_PROCESS_CRASHED ? "webkit: web process crashed"
		: r == WEBKIT_WEB_PROCESS_EXCEEDED_MEMORY_LIMIT ? "webkit: web process ended (memory)" : "webkit: web process ended"));
	if (!alive || !page || ++reloads > 3) return; // a page that kills it every time stays down
	loaded = FALSE;
	webkit_web_view_load_html(view, page, NULL);
}

int cs_init(void) {
	g_set_prgname("core-starter"); // Wayland app id: matches core-starter.desktop
	return gtk_init_check(NULL, NULL);
}

void cs_window(const char *title, int width, int height, const void *icon, int icon_len) {
	loaded = FALSE; // a new view loads its page once more (tests open several)
	reloads = 0;
	win = gtk_window_new(GTK_WINDOW_TOPLEVEL);
	gtk_window_set_title(GTK_WINDOW(win), title);
	gtk_window_set_icon_name(GTK_WINDOW(win), "core-starter");
	if (icon && icon_len > 0) {
		GdkPixbufLoader *l = gdk_pixbuf_loader_new();
		if (gdk_pixbuf_loader_write(l, icon, icon_len, NULL) && gdk_pixbuf_loader_close(l, NULL)) {
			gtk_window_set_icon(GTK_WINDOW(win), gdk_pixbuf_loader_get_pixbuf(l));
		}
		g_object_unref(l);
	}
	// Fit the work area (a small laptop screen at 150 % would hide "Save").
	GdkDisplay *disp = gdk_display_get_default();
	GdkMonitor *mon = gdk_display_get_primary_monitor(disp);
	if (!mon && gdk_display_get_n_monitors(disp) > 0) mon = gdk_display_get_monitor(disp, 0);
	if (mon) {
		GdkRectangle wa;
		gdk_monitor_get_workarea(mon, &wa);
		if (wa.height > 0 && height > wa.height - 40) height = wa.height - 40;
		if (wa.width > 0 && width > wa.width) width = wa.width;
	}
	gtk_window_set_default_size(GTK_WINDOW(win), width, height);
	gtk_window_set_position(GTK_WINDOW(win), GTK_WIN_POS_CENTER);

	WebKitWebContext *ctx = webkit_web_context_new_ephemeral();
	WebKitUserContentManager *ucm = webkit_user_content_manager_new();
	webkit_user_content_manager_add_script(ucm, webkit_user_script_new(shim,
		WEBKIT_USER_CONTENT_INJECT_TOP_FRAME, WEBKIT_USER_SCRIPT_INJECT_AT_DOCUMENT_START, NULL, NULL));
	g_signal_connect(ucm, "script-message-received::bridge", G_CALLBACK(on_message), NULL);
	webkit_user_content_manager_register_script_message_handler(ucm, "bridge");
	view = WEBKIT_WEB_VIEW(g_object_new(WEBKIT_TYPE_WEB_VIEW, "web-context", ctx, "user-content-manager", ucm, NULL));
	g_object_unref(ctx);
	g_object_unref(ucm);

	WebKitSettings *s = webkit_web_view_get_settings(view);
	webkit_settings_set_enable_developer_extras(s, FALSE);
	webkit_settings_set_enable_write_console_messages_to_stdout(s, FALSE);
	webkit_settings_set_javascript_can_open_windows_automatically(s, FALSE);
	webkit_settings_set_enable_webgl(s, FALSE);
	webkit_settings_set_enable_back_forward_navigation_gestures(s, FALSE);
	webkit_settings_set_enable_media(s, FALSE);
	g_signal_connect(view, "decide-policy", G_CALLBACK(on_decide), NULL);
	g_signal_connect(view, "context-menu", G_CALLBACK(on_context_menu), NULL);
	g_signal_connect(view, "web-process-terminated", G_CALLBACK(on_crash), NULL);
	gtk_drag_dest_unset(GTK_WIDGET(view)); // nothing dropped onto the window gets opened

	// Brand canvas until the page draws: never a white window.
	GdkRGBA canvas = {3 / 255.0, 5 / 255.0, 4 / 255.0, 1};
	webkit_web_view_set_background_color(view, &canvas);

	gtk_container_add(GTK_CONTAINER(win), GTK_WIDGET(view));
	g_signal_connect(win, "delete-event", G_CALLBACK(on_delete), NULL);
	g_signal_connect(win, "destroy", G_CALLBACK(on_destroy), NULL);
	alive = TRUE;
}

void cs_load(const char *html) {
	gtk_widget_show_all(win);
	gtk_window_present(GTK_WINDOW(win));
	g_free(page);
	page = g_strdup(html);
	webkit_web_view_load_html(view, html, NULL); // NULL base: about:blank
}

void cs_run(void) {
	if (alive) gtk_main();
}

void cs_eval(const char *js) {
	if (!alive) return;
	// run_javascript exists in every WebKitGTK 4.1 (evaluate_javascript only
	// from 2.40, newer than Ubuntu 22.04's first release).
	G_GNUC_BEGIN_IGNORE_DEPRECATIONS
	webkit_web_view_run_javascript(view, js, NULL, NULL, NULL);
	G_GNUC_END_IGNORE_DEPRECATIONS
}

static gboolean wake_cb(gpointer d) {
	goWake();
	return G_SOURCE_REMOVE;
}

void cs_wake(void) { g_idle_add(wake_cb, NULL); }

static gboolean close_cb(gpointer d) {
	if (alive) gtk_window_close(GTK_WINDOW(win)); // goes through on_delete
	return G_SOURCE_REMOVE;
}

void cs_close(void) { g_idle_add(close_cb, NULL); }

void cs_destroy(void) {
	if (alive) gtk_widget_destroy(win);
}

void cs_present(void) {
	if (alive) gtk_window_present(GTK_WINDOW(win));
}

int cs_dialog(const char *title, const char *text, int kind) {
	GtkMessageType type = kind == 1 ? GTK_MESSAGE_WARNING : kind == 2 ? GTK_MESSAGE_ERROR : kind == 3 ? GTK_MESSAGE_QUESTION : GTK_MESSAGE_INFO;
	GtkWidget *d = gtk_message_dialog_new(alive ? GTK_WINDOW(win) : NULL, GTK_DIALOG_MODAL, type,
		kind == 3 ? GTK_BUTTONS_YES_NO : GTK_BUTTONS_OK, "%s", text);
	gtk_window_set_title(GTK_WINDOW(d), title);
	gtk_window_set_icon_name(GTK_WINDOW(d), "core-starter");
	int r = gtk_dialog_run(GTK_DIALOG(d));
	gtk_widget_destroy(d);
	while (gtk_events_pending()) gtk_main_iteration();
	return r == GTK_RESPONSE_OK || r == GTK_RESPONSE_YES;
}
