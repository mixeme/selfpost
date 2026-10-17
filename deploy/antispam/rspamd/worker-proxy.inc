# Mounted into the sidecar as /etc/rspamd/local.d/worker-proxy.inc.
#
# The milter - the proxy worker - is the one listener SelfPost needs on the
# compose network. The fragment sets RSPAMD_LOCAL_ADDR=localhost, which keeps
# every worker on the container's loopback: the normal worker (11333), whose
# scan accepts a client-named file or shared-memory input, and the controller
# (11334), whose default password is the well-known "q1" (rspamd 4.2.2,
# conf/rspamd.conf: each worker's bind_socket is the env variable LOCAL_ADDR,
# localhost by default, and its port; conf/worker-controller.inc: password =
# "q1"; the rspamd-docker v4.2.2+0 Dockerfile sets RSPAMD_LOCAL_ADDR=* for all
# three). This line puts the proxy alone back on every address: a local.d
# scalar at priority 1 replaces the default one (contrib/libucl/ucl_parser.c,
# UCL_DUPLICATE_MERGE). The proxy still reaches the normal worker on
# localhost, as before (security audit 2026-10-17, S4).
#
# rspamd expands its template syntax in these files, comments included:
# quote none of it here.
bind_socket = "*:11332";
