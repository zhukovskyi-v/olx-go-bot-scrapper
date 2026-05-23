CREATE TABLE IF NOT EXISTS watches (
    user_id      INTEGER NOT NULL,
    url          TEXT    NOT NULL,
    created_at   INTEGER NOT NULL,
    paused       INTEGER NOT NULL DEFAULT 0,
    bootstrapped INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, url)
);

CREATE TABLE IF NOT EXISTS seen_ads (
    user_id   INTEGER NOT NULL,
    ad_id     TEXT    NOT NULL,
    seen_at   INTEGER NOT NULL,
    PRIMARY KEY (user_id, ad_id)
);

CREATE INDEX IF NOT EXISTS idx_seen_user ON seen_ads(user_id);
