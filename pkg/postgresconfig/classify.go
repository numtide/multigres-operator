package postgresconfig

import "strings"

// restartContexts are the pg_settings "context" values whose parameters do not
// take effect for the running data plane on a configuration reload (SIGHUP), so
// the operator must recreate the pods to apply them:
//
//   - postmaster: settable only at server start (e.g. shared_buffers,
//     max_connections, wal_level).
//   - internal: compiled-in / read-only (e.g. block_size); never reloadable.
//   - backend / superuser-backend: fixed at backend start. A SIGHUP updates the
//     value the postmaster hands to SUBSEQUENTLY-started backends, but existing
//     backends keep their start-time value (PostgreSQL does not re-read the
//     config file mid-session; see set_config_option's PGC_BACKEND handling).
//     Because the multipooler fronts PostgreSQL with long-lived pooled backends,
//     no new backend is started on a reload, so params like log_connections /
//     log_disconnections would silently never take effect until the pods are
//     recreated. Classifying them as restart recycles those backends so the
//     change actually applies. (The pooler's ReloadConfig would even report the
//     reload as succeeded — pg_file_settings.applied is true for these — so
//     without this the operator would mark the change done while it had no
//     effect.)
//
// Every other context (sighup, superuser, user) is applied by a reload without a
// restart.
var restartContexts = map[string]bool{
	"postmaster":        true,
	"internal":          true,
	"backend":           true,
	"superuser-backend": true,
}

// RequiresRestart reports whether changing the given parameter requires a
// PostgreSQL restart to take effect, as opposed to a configuration reload
// (SIGHUP). This is the operator's static basis for the reload-vs-restart
// split: parameters that require a restart drive pod recreation, the rest can
// be applied in place by reloading the running server.
//
// Unknown parameters and namespaced (extension) parameters such as "cron.*" or
// "auto_explain.*" are not in the built-in catalog and default to restart. This
// is the conservative choice: reloading a parameter that actually needed a
// restart would silently fail to apply it, whereas needlessly restarting for a
// reload-safe one is merely more disruptive, not incorrect.
func RequiresRestart(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	// Namespaced extension parameters are custom placeholders absent from the
	// built-in catalog; classify conservatively as restart.
	if strings.Contains(lower, ".") {
		return true
	}
	entry, ok := catalog[lower]
	if !ok || entry.context == "" {
		return true
	}
	return restartContexts[entry.context]
}
