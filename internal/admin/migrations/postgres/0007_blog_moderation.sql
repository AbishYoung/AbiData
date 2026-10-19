-- Blog moderation (ADR 0017); see the SQLite migration.
CREATE TABLE _adm_blog_takedowns (
  id text COLLATE "C" PRIMARY KEY,
  blog_id text COLLATE "C" NOT NULL REFERENCES _adm_blogs(id) ON DELETE CASCADE,
  post_slug text COLLATE "C",
  reason_code text COLLATE "C" NOT NULL CHECK (reason_code IN ('spam','phishing','malware','illegal','harassment','copyright','other')),
  note text COLLATE "C" NOT NULL DEFAULT '',
  taken_down_by text COLLATE "C" NOT NULL,
  taken_down_at text COLLATE "C" NOT NULL,
  restored_by text COLLATE "C",
  restored_at text COLLATE "C"
);
CREATE UNIQUE INDEX _adm_blog_takedowns_active ON _adm_blog_takedowns(blog_id, coalesce(post_slug, '')) WHERE restored_at IS NULL;
CREATE INDEX _adm_blog_takedowns_blog ON _adm_blog_takedowns(blog_id, taken_down_at);

CREATE TABLE _adm_blog_reports (
  id text COLLATE "C" PRIMARY KEY,
  blog_id text COLLATE "C" NOT NULL REFERENCES _adm_blogs(id) ON DELETE CASCADE,
  post_slug text COLLATE "C",
  reason_code text COLLATE "C" NOT NULL CHECK (reason_code IN ('spam','phishing','malware','illegal','harassment','copyright','other')),
  message text COLLATE "C" NOT NULL DEFAULT '',
  created_at text COLLATE "C" NOT NULL,
  resolved_by text COLLATE "C",
  resolved_at text COLLATE "C"
);
CREATE INDEX _adm_blog_reports_open ON _adm_blog_reports(blog_id, created_at) WHERE resolved_at IS NULL;
