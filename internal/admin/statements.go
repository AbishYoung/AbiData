package admin

import "abidata/internal/sqlgen"

// The package's hand-written statements (ADR 0013 §7), registered so the
// conformance test prepares each against the migrated schema.
var (
	sqlCreateAPIKey = sqlgen.Register("admin.CreateAPIKey", `DELETE FROM _adm_api_keys WHERE workspace_id = ?1 AND
			((revoked_at IS NOT NULL AND revoked_at < ?2) OR (expires_at IS NOT NULL AND expires_at < ?2))`)
	sqlCreateAPIKey2 = sqlgen.Register("admin.CreateAPIKey#2", `SELECT count(*) FROM _adm_api_keys WHERE workspace_id = ?1 AND revoked_at IS NULL
			AND (expires_at IS NULL OR expires_at > ?2)`)
	sqlCreateAPIKey3 = sqlgen.Register("admin.CreateAPIKey#3", `INSERT INTO _adm_api_keys (id, workspace_id, name, role, secret_hash, created_by, created_at, expires_at)
			VALUES (?,?,?,?,?,?,?,?)`)
	sqlRevokeAPIKey   = sqlgen.Register("admin.RevokeAPIKey", `UPDATE _adm_api_keys SET revoked_at = COALESCE(revoked_at, ?) WHERE id = ?`)
	sqlTouchAPIKey    = sqlgen.Register("admin.TouchAPIKey", `UPDATE _adm_api_keys SET last_used_at = ? WHERE id = ?`)
	sqlLoadAppItems   = sqlgen.Register("admin.loadAppItems", `SELECT id, kind, object_id, title, fingerprint FROM _adm_app_items WHERE app_id = ? ORDER BY position`)
	sqlWriteAppItems  = sqlgen.Register("admin.writeAppItems", `DELETE FROM _adm_app_items WHERE app_id = ?`)
	sqlWriteAppItems2 = sqlgen.Register("admin.writeAppItems#2", `INSERT INTO _adm_app_items (app_id, id, kind, object_id, title, position, fingerprint) VALUES (?,?,?,?,?,?,?)`)
	sqlCreateApp      = sqlgen.Register("admin.CreateApp", `SELECT count(*) FROM _adm_apps WHERE workspace_id = ?`)
	sqlCreateApp2     = sqlgen.Register("admin.CreateApp#2", `INSERT INTO _adm_apps (id, workspace_id, name, token, token_hash, start_item, enabled, created_by, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?)`)
	sqlUpdateApp       = sqlgen.Register("admin.UpdateApp", `UPDATE _adm_apps SET name = ?, start_item = ?, enabled = ?, updated_at = ? WHERE id = ?`)
	sqlSetAppToken     = sqlgen.Register("admin.SetAppToken", `UPDATE _adm_apps SET token = ?, token_hash = ?, updated_at = ? WHERE id = ?`)
	sqlSetAppTakenDown = sqlgen.Register("admin.SetAppTakenDown", `UPDATE _adm_apps SET taken_down_at = ?, taken_down_by = ? WHERE id = ?`)
	sqlDeleteApp       = sqlgen.Register("admin.DeleteApp", `DELETE FROM _adm_apps WHERE id = ?`)
	sqlAudit           = sqlgen.Register("admin.Audit", `INSERT INTO _adm_audit (at, user_id, action, object_kind, object_id, details)
			VALUES (?,?,?,?,?,?)`)
	sqlListAudit = sqlgen.Register("admin.ListAudit", `SELECT at, user_id, action, object_kind, object_id, details
			FROM _adm_audit ORDER BY at DESC, id DESC LIMIT ?`)
	sqlDeleteBackup      = sqlgen.Register("admin.DeleteBackup", `DELETE FROM _adm_backups WHERE id = ?`)
	sqlChangeMembership  = sqlgen.Register("admin.ChangeMembership", `SELECT role FROM _adm_memberships WHERE workspace_id = ? AND user_id = ?`)
	sqlChangeMembership2 = sqlgen.Register("admin.ChangeMembership#2", `SELECT count(*) FROM _adm_memberships WHERE workspace_id = ? AND role = 'owner'`)
	sqlChangeMembership3 = sqlgen.Register("admin.ChangeMembership#3", `DELETE FROM _adm_memberships WHERE workspace_id = ? AND user_id = ?`)
	sqlChangeMembership4 = sqlgen.Register("admin.ChangeMembership#4", `UPDATE _adm_memberships SET role = ? WHERE workspace_id = ? AND user_id = ?`)
	sqlCreateSession     = sqlgen.Register("admin.CreateSession", `INSERT INTO _adm_sessions
			(id_hash, user_id, csrf_token, created_at, last_seen_at, expires_at, ip, user_agent)
			VALUES (?,?,?,?,?,?,?,?)`)
	sqlGetSession = sqlgen.Register("admin.GetSession", `SELECT id_hash, user_id, csrf_token, created_at, last_seen_at, expires_at, ip, user_agent
			FROM _adm_sessions WHERE id_hash = ?`)
	sqlTouchSession       = sqlgen.Register("admin.TouchSession", `UPDATE _adm_sessions SET last_seen_at = ? WHERE id_hash = ?`)
	sqlDeleteSession      = sqlgen.Register("admin.DeleteSession", `DELETE FROM _adm_sessions WHERE id_hash = ?`)
	sqlDeleteUserSessions = sqlgen.Register("admin.DeleteUserSessions", `DELETE FROM _adm_sessions WHERE user_id = ?`)
	sqlPurgeSessions      = sqlgen.Register("admin.PurgeSessions", `DELETE FROM _adm_sessions WHERE expires_at <= ? OR last_seen_at <= ?`)
	sqlPurgeSessions2     = sqlgen.Register("admin.PurgeSessions#2", `DELETE FROM _adm_login_attempts WHERE window_start <= ?`)
	sqlReadAttempts       = sqlgen.Register("admin.readAttempts", `SELECT count, window_start FROM _adm_login_attempts WHERE key = ?`)
	sqlRecordLoginFailure = sqlgen.Register("admin.RecordLoginFailure", `INSERT INTO _adm_login_attempts (key, count, window_start) VALUES (?,?,?)
			ON CONFLICT(key) DO UPDATE SET count = excluded.count, window_start = excluded.window_start`)
	sqlClearLoginAttemptsPrefix = sqlgen.Register("admin.ClearLoginAttemptsPrefix", `DELETE FROM _adm_login_attempts WHERE substr(key, 1, ?) = ?`)
	sqlClearLoginAttempts       = sqlgen.Register("admin.ClearLoginAttempts", `DELETE FROM _adm_login_attempts WHERE key = ?`)
	sqlGetSetting               = sqlgen.Register("admin.GetSetting", `SELECT value FROM _adm_settings WHERE key = ?`)
	sqlPutSetting               = sqlgen.Register("admin.PutSetting", `INSERT INTO _adm_settings (key, value) VALUES (?,?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`)
	sqlListSettings = sqlgen.Register("admin.ListSettings", `SELECT key, value FROM _adm_settings`)
	sqlCountUsers   = sqlgen.Register("admin.CountUsers", `SELECT count(*) FROM _adm_users`)
	sqlUpdateUser   = sqlgen.Register("admin.UpdateUser", `UPDATE _adm_users SET
			display_name = COALESCE(:dn, display_name),
			is_platform_admin = COALESCE(:admin, is_platform_admin),
			disabled = COALESCE(:disabled, disabled)
			WHERE id = :id`)
	// A name is held by a current user, or by the user who gave it up until the hold expires.
	sqlUsernameOwner = sqlgen.Register("admin.UsernameOwner", `SELECT id FROM _adm_users WHERE username = ?1
			UNION ALL SELECT user_id FROM _adm_username_history WHERE username = ?1 AND expires_at > ?2 LIMIT 1`)
	sqlResolveOldUsername = sqlgen.Register("admin.ResolveOldUsername", `SELECT u.username FROM _adm_username_history h
			JOIN _adm_users u ON u.id = h.user_id WHERE h.username = ? AND h.expires_at > ? AND u.username IS NOT NULL`)
	sqlInitUsername = sqlgen.Register("admin.InitUsername", `UPDATE _adm_users SET username = ? WHERE id = ? AND username IS NULL`)
	sqlSetUsername  = sqlgen.Register("admin.SetUsername", `SELECT username FROM _adm_users WHERE id = ?`)
	sqlSetUsername2 = sqlgen.Register("admin.SetUsername#2", `DELETE FROM _adm_username_history WHERE username = ? AND user_id = ?`)
	sqlSetUsername3 = sqlgen.Register("admin.SetUsername#3", `UPDATE _adm_users SET username = ?, username_changed_at = ? WHERE id = ?`)
	sqlSetUsername4 = sqlgen.Register("admin.SetUsername#4", `INSERT INTO _adm_username_history (username, user_id, changed_at, expires_at) VALUES (?,?,?,?)
			ON CONFLICT(username) DO UPDATE SET user_id = excluded.user_id, changed_at = excluded.changed_at, expires_at = excluded.expires_at`)
	sqlSetPasswordHash = sqlgen.Register("admin.SetPasswordHash", `UPDATE _adm_users SET password_hash = ? WHERE id = ?`)
	sqlTouchLogin      = sqlgen.Register("admin.TouchLogin", `UPDATE _adm_users SET last_login_at = ? WHERE id = ?`)
	sqlCreateWorkspace = sqlgen.Register("admin.CreateWorkspace", `INSERT INTO _adm_workspaces (id, name, status, quota_json, created_at, created_by)
			VALUES (?,?,?,?,?,?)`)
	sqlCreateWorkspace2 = sqlgen.Register("admin.CreateWorkspace#2", `INSERT INTO _adm_memberships (workspace_id, user_id, role) VALUES (?,?,?)`)
	sqlDropWorkspace    = sqlgen.Register("admin.DropWorkspace", `DELETE FROM _adm_workspaces WHERE id = ?`)
	sqlUpdateWorkspace  = sqlgen.Register("admin.UpdateWorkspace", `UPDATE _adm_workspaces SET
			name = COALESCE(:name, name),
			status = COALESCE(:status, status),
			quota_json = COALESCE(:quota, quota_json)
			WHERE id = :id`)
	sqlSetMembership = sqlgen.Register("admin.SetMembership", `INSERT INTO _adm_memberships (workspace_id, user_id, role) VALUES (?,?,?)
			ON CONFLICT(workspace_id, user_id) DO UPDATE SET role = excluded.role`)
	// email's column collation (NOCASE, abi_ci on PostgreSQL) orders it.
	sqlListMembers = sqlgen.Register("admin.ListMembers", `SELECT u.id, u.email, u.display_name, m.role FROM _adm_memberships m
			JOIN _adm_users u ON u.id = m.user_id WHERE m.workspace_id = ?
			ORDER BY u.email, u.id`)
	sqlMigrate  = sqlgen.RegisterSQLiteOnly("admin.migrate", "PRAGMA application_id")
	sqlMigrate2 = sqlgen.RegisterSQLiteOnly("admin.migrate#2", "PRAGMA user_version")
	sqlPing     = sqlgen.Register("admin.Ping", "SELECT 1")
	sqlClose    = sqlgen.RegisterSQLiteOnly("admin.Close", "PRAGMA wal_checkpoint(TRUNCATE)")
)
