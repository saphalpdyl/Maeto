// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * BFD hook for external (Maeto) use.
 * Copyright (C) 2026        Saphal K. Poudyal
 */

#include <zebra.h>

#include <sys/un.h>

#include "lib/hook.h"
#include "lib/module.h"
#include "lib/libfrr.h"
#include "lib/frrevent.h"
#include "lib/northbound.h"
#include "string.h"

/*
 * Default socket path of the Maeto agent. Override at load time with
 * `-M maeto:/path/to/agent.sock`.
 */
#define MAETO_SOCKET_PATH	   "/run/maeto/frr.bfd.sock"
#define MAETO_RETRY_INTERVAL_MSECS 1000

#define BFDD_SESSIONS_XPATH "/frr-bfdd:bfdd/bfd/sessions"

/* Module state, shared by the hooks below. bfdd is single threaded. */
static int maeto_sockfd = -1;
static struct sockaddr_un maeto_agent_addr;

static struct event_loop *maeto_master;

static struct event *t_connect;
static struct event *t_read;

static void maeto_connect(struct event *t);
static void maeto_read(struct event *t);
static char *maeto_get_bfdd_snapshot(void);

static void maeto_schedule_connect(long msecs)
{
	event_add_timer_msec(maeto_master, maeto_connect, NULL, msecs, &t_connect);
}

static void maeto_disconnect(int err)
{
	if (maeto_sockfd < 0)
		return;

	event_cancel(&t_connect);
	event_cancel(&t_read);
	close(maeto_sockfd);
	maeto_sockfd = -1;
	zlog_warn("maeto: disconnected %s", strerror(err));
	maeto_schedule_connect(MAETO_RETRY_INTERVAL_MSECS);
}

static int maeto_send(const char *buf, size_t len)
{
	if (maeto_sockfd < 0)
		return 0;

	ssize_t n;
	n = send(maeto_sockfd, buf, len, MSG_DONTWAIT | MSG_NOSIGNAL);
	if (n == -1) {
		maeto_disconnect(errno);
		return -1;
	}

	zlog_debug("maeto: sent something");
	return 0;
}

static void maeto_connect(struct event *t)
{
	const char *path = THIS_MODULE->load_args;

	if (path == NULL || path[0] == '\0')
		path = MAETO_SOCKET_PATH;

	if (strlen(path) > sizeof(maeto_agent_addr.sun_path) - 1) {
		zlog_err("maeto: socket path too long: %s", path);
		return;
	}

	maeto_sockfd = socket(AF_UNIX, SOCK_SEQPACKET | SOCK_NONBLOCK | SOCK_CLOEXEC, 0);
	if (maeto_sockfd == -1) {
		zlog_err("maeto: socket failed %s", strerror(errno));
		maeto_schedule_connect(MAETO_RETRY_INTERVAL_MSECS);
		return;
	}

	memset(&maeto_agent_addr, 0, sizeof(maeto_agent_addr));

	maeto_agent_addr.sun_family = AF_UNIX;
	strncpy(maeto_agent_addr.sun_path, path, sizeof(maeto_agent_addr.sun_path) - 1);

	if (connect(maeto_sockfd, (struct sockaddr *)&maeto_agent_addr,
		    sizeof(maeto_agent_addr)) == -1) {
		zlog_debug("maeto: failed to connect %s", strerror(errno));
		close(maeto_sockfd);
		maeto_sockfd = -1;

		maeto_schedule_connect(MAETO_RETRY_INTERVAL_MSECS);
		return;
	}

	// Successful connection; This is our RESYNC signal
	char *snapshot = maeto_get_bfdd_snapshot();
	if (snapshot == NULL) {
		maeto_disconnect(0);
		return;
	}
	ssize_t bytes = maeto_send(snapshot, strlen(snapshot));

	if (bytes == -1) {
		zlog_err("maeto: failed to send bytes");
		free(snapshot);
		return;
	}

	free(snapshot);

	zlog_info("maeto: connected");
	event_add_read(maeto_master, maeto_read, NULL, maeto_sockfd, &t_read);
}


static void maeto_read(struct event *t)
{
	char buf[1024];
	ssize_t n = recv(maeto_sockfd, &buf, sizeof(buf), 0);

	if (n == -1) {
		if (!(errno == EAGAIN || errno == EWOULDBLOCK)) {
			zlog_err("maeto: failed to recv buffer");
			maeto_disconnect(errno);
			return;
		}
	} else if (n == 0) {
		maeto_disconnect(ECONNRESET);
		return;
	} else {
	}

	event_add_read(maeto_master, maeto_read, NULL, maeto_sockfd, &t_read);
}


static int module_late_init(struct event_loop *master)
{
	maeto_master = master;
	maeto_schedule_connect(0);
	return 0;
}

// be sure to free the output json
static char *maeto_get_bfdd_snapshot(void)
{
	struct lyd_node *tree = NULL;
	if (nb_oper_iterate_legacy(BFDD_SESSIONS_XPATH, NULL, 0, NULL, NULL, &tree) != NB_OK) {
		lyd_free_all(tree);
		return NULL;
	}

	char *json = NULL;
	char *snapshot = NULL;

	if (lyd_print_mem(&json, tree, LYD_JSON, LYD_PRINT_SHRINK) != LY_SUCCESS) {
		zlog_err("maeto: failed to read bfdd/sessions tree");

		lyd_free_all(tree);
		free(json);

		return NULL;
	}

	zlog_debug("maeto: got bfd session tree: %s", json);

	if (asprintf(&snapshot, "{\"type\": \"snapshot\", \"v\":1, \"data\": %s}", json) < 0) {
		zlog_err("maeto: failed to build snapshot packet");
		snapshot = NULL;
	}

	lyd_free_all(tree);
	free(json);
	return snapshot;
}

static int maeto_notif(const char *xpath, const struct lyd_node *tree)
{
	char *tree_json = NULL;
	char *final_packet = NULL;

	if (maeto_sockfd < 0)
		return 0;

	if (strncmp(xpath, "/frr-bfdd:", strlen("/frr-bfdd:")) != 0)
		return 0;

	if (lyd_print_mem(&tree_json, tree, LYD_JSON, LYD_PRINT_SHRINK) != LY_SUCCESS)
		return 1;

	if (tree_json == NULL) {
		return -1;
	}

	if (asprintf(&final_packet, "{\"type\": \"delta\", \"v\":1, \"data\": %s}", tree_json) < 0) {
		zlog_err("maeto: failed to build delta packet");
		free(tree_json);
		return -1;
	}

	ssize_t bytes = maeto_send(final_packet, strlen(final_packet));

	free(final_packet);
	free(tree_json);

	if (bytes == -1) {
		zlog_err("maeto: failed to send bytes");
		return -1;
	}

	return 0;
}

static int maeto_fini(void)
{
	if (maeto_sockfd >= 0) {
		close(maeto_sockfd);
		maeto_sockfd = -1;
	}

	event_cancel(&t_connect);
	event_cancel(&t_read);
	return 0;
}

static int maeto_init(void)
{
	hook_register(frr_late_init, module_late_init);
	hook_register(nb_notification_tree_send, maeto_notif);
	hook_register(frr_fini, maeto_fini);
	return 0;
}

FRR_MODULE_SETUP(.name = "Maeto BFD Listener", .version = "0.0",
		 .description = "Listens to BFD events and relays it through sockets/gRPC",
		 .init = maeto_init);
