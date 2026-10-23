CREATE TABLE users (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE COLLATE NOCASE,
  pw_hash TEXT NOT NULL,
  is_admin INTEGER NOT NULL DEFAULT 0,
  disabled INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  pw_changed_at INTEGER NOT NULL,
  sig BLOB
);
CREATE TABLE totp (
  user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  secret_enc BLOB NOT NULL,
  nonce BLOB NOT NULL,
  confirmed_at INTEGER,
  last_step INTEGER NOT NULL DEFAULT 0,
  sig BLOB
);
CREATE TABLE webauthn_creds (
  id INTEGER PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  cred_id BLOB NOT NULL UNIQUE,
  public_key BLOB NOT NULL,
  sign_count INTEGER NOT NULL DEFAULT 0,
  transports TEXT NOT NULL DEFAULT '',
  label TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  last_used_at INTEGER,
  sig BLOB
);
CREATE TABLE recovery_codes (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  hash TEXT NOT NULL,
  used_at INTEGER,
  sig BLOB,
  PRIMARY KEY (user_id, hash)
);
CREATE TABLE sessions (
  id_hash BLOB PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  last_seen_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  ip TEXT NOT NULL DEFAULT '',
  ua TEXT NOT NULL DEFAULT '',
  mfa_at INTEGER
);
CREATE TABLE home_acl (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  home_name TEXT NOT NULL,
  mem_read INTEGER NOT NULL DEFAULT 0,
  mem_write INTEGER NOT NULL DEFAULT 0,
  mem_sync INTEGER NOT NULL DEFAULT 0,
  usage_read INTEGER NOT NULL DEFAULT 0,
  exec INTEGER NOT NULL DEFAULT 0,
  sig BLOB,
  PRIMARY KEY (user_id, home_name)
);
CREATE TABLE auth_events (
  at INTEGER NOT NULL,
  user_id INTEGER,
  name_tried TEXT NOT NULL DEFAULT '',
  ip TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL,
  outcome TEXT NOT NULL
);
CREATE INDEX auth_events_at ON auth_events(at);
CREATE TABLE throttle (
  key TEXT PRIMARY KEY,
  fails INTEGER NOT NULL DEFAULT 0,
  first_at INTEGER NOT NULL,
  locked_until INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE meta (
  key TEXT PRIMARY KEY,
  value BLOB
);
