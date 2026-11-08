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
static const char *MAETO_SOCKET_PATH = "/run/maeto/frr.bfd.sock";

/* Module state, shared by the hooks below. bfdd is single threaded. */
static int maeto_sockfd = -1;
static struct sockaddr_un maeto_agent_addr;
static uint64_t maeto_sent;

static int module_late_init(struct event_loop *master)
{
	const char *path = THIS_MODULE->load_args;

	if (path == NULL || path[0] == '\0')
		path = MAETO_SOCKET_PATH;

	if (strlen(path) > sizeof(maeto_agent_addr.sun_path) - 1) {
		zlog_err("maeto: socket path too long: %s", path);
		return 1;
	}

	maeto_sockfd = socket(AF_UNIX, SOCK_DGRAM, 0);
	if (maeto_sockfd == -1) {
		zlog_err("maeto: socket failed: %s", strerror(errno));
		return 1;
	}

	memset(&maeto_agent_addr, 0, sizeof(maeto_agent_addr));

	maeto_agent_addr.sun_family = AF_UNIX;
	strncpy(maeto_agent_addr.sun_path, path, sizeof(maeto_agent_addr.sun_path) - 1);

	return 0;
}

static int maeto_notif(const char *xpath, const struct lyd_node *tree)
{
	char *json = NULL;

	if (maeto_sockfd < 0)
		return 0;

	if (strncmp(xpath, "/frr-bfdd:", strlen("/frr-bfdd:")) != 0)
		return 0;

	if (lyd_print_mem(&json, tree, LYD_JSON, LYD_PRINT_SHRINK) != LY_SUCCESS)
		return 1;

	ssize_t b = sendto(maeto_sockfd, json, strlen(json), MSG_DONTWAIT,
		(struct sockaddr*)&maeto_agent_addr, sizeof(struct sockaddr_un));
	if (b == -1) {
		free(json);
		zlog_warn("couldn't send data to socket: %s", strerror(errno));
		return 1;
	}

	maeto_sent++;

	zlog_debug("Sent notification packets: %llu", maeto_sent);

	free(json);

	return 0;
}

static int maeto_fini(void)
{
	if (maeto_sockfd >= 0) {
		close(maeto_sockfd);
		maeto_sockfd = -1;
	}
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
