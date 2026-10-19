-- Blog moderation (ADR 0017). Take-downs are append-only history: a restore
-- fills restored_by/restored_at. post_slug NULL takes down the whole blog. At
-- most one active take-down per (blog, post_slug).
CREATE TABLE _adm_blog_takedowns (
  id TEXT PRIMARY KEY,
  blog_id TEXT NOT NULL REFERENCES _adm_blogs(id) ON DELETE CASCADE,
  post_slug TEXT,
  reason_code TEXT NOT NULL CHECK (reason_code IN ('spam','phishing','malware','illegal','harassment','copyright','other')),
  note TEXT NOT NULL DEFAULT '',
  taken_down_by TEXT NOT NULL,
  taken_down_at TEXT NOT NULL,
  restored_by TEXT,
  restored_at TEXT
) STRICT;
CREATE UNIQUE INDEX _adm_blog_takedowns_active ON _adm_blog_takedowns(blog_id, coalesce(post_slug, '')) WHERE restored_at IS NULL;
CREATE INDEX _adm_blog_takedowns_blog ON _adm_blog_takedowns(blog_id, taken_down_at);

-- Reports from readers ("Report this page"). No account and no address is
-- stored; the limiter keeps only a hash of the address in memory.
CREATE TABLE _adm_blog_reports (
  id TEXT PRIMARY KEY,
  blog_id TEXT NOT NULL REFERENCES _adm_blogs(id) ON DELETE CASCADE,
  post_slug TEXT,
  reason_code TEXT NOT NULL CHECK (reason_code IN ('spam','phishing','malware','illegal','harassment','copyright','other')),
  message TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  resolved_by TEXT,
  resolved_at TEXT
) STRICT;
CREATE INDEX _adm_blog_reports_open ON _adm_blog_reports(blog_id, created_at) WHERE resolved_at IS NULL;
