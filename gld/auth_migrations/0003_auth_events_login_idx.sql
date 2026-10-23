CREATE INDEX auth_events_user_login ON auth_events(user_id, kind, outcome, at);
