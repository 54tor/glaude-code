ALTER TABLE sessions ADD COLUMN parent_hash BLOB;
CREATE INDEX sessions_parent_idx ON sessions(parent_hash);
CREATE TRIGGER sessions_cascade AFTER DELETE ON sessions BEGIN
  DELETE FROM sessions WHERE parent_hash = OLD.id_hash;
END;
