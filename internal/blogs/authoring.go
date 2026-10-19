package blogs

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"abidata/internal/admin"
	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/records"
	"abidata/internal/workspace"
)

// Limits of the blog home and the authoring API.
const (
	MaxBlogTitle     = 200
	MaxIntro         = 2000
	PublishesPerHour = 10 // blog switch and post status changes, per user
	pageSize         = 50
)

// Post chips: what the blog UI shows for a post. Text, never colour alone.
const (
	ChipDraft          = "draft"
	ChipPublished      = "published"
	ChipUnlisted       = "unlisted"
	ChipTakenDown      = "taken_down"
	ChipNeedsAttention = "needs_attention"
)

// BlogInfo is the blog home as its owner sees it.
type BlogInfo struct {
	ID                 string     `json:"id"`
	WorkspaceID        string     `json:"workspace_id"`
	Title              string     `json:"title"`
	Intro              string     `json:"intro" doc:"Plain text; blank lines separate paragraphs."`
	Published          bool       `json:"published" doc:"The owner's switch. Off: nothing of the blog is served."`
	PublishedAt        *time.Time `json:"published_at,omitempty" doc:"When the blog was first switched on."`
	Username           string     `json:"username"`
	PublicURL          string     `json:"public_url"`
	PublicBlogsEnabled bool       `json:"public_blogs_enabled" doc:"The install-wide switch. Off: no blog is served, whatever the owner's switches say."`
	Takedown           *Takedown  `json:"takedown,omitempty" doc:"An administrator's active take-down of the whole blog."`
}

// Takedown is an active take-down as the blog's owner sees it.
type Takedown struct {
	ReasonCode string    `json:"reason_code"`
	Reason     string    `json:"reason" doc:"The reason in words."`
	Note       string    `json:"note" doc:"The administrator's note to the owner."`
	At         time.Time `json:"at"`
}

// ReasonText is a take-down or report reason in words.
var ReasonText = map[string]string{
	"spam": "Spam", "phishing": "Phishing", "malware": "Malware", "illegal": "Illegal content",
	"harassment": "Harassment", "copyright": "Copyright", "other": "Other",
}

// activeTakedowns returns the blog's active take-down (nil if none) and the
// posts' by slug.
func (s *Service) activeTakedowns(ctx context.Context, blogID string) (*Takedown, map[string]*Takedown, error) {
	all, err := s.Admin.Takedowns(ctx, blogID)
	if err != nil {
		return nil, nil, err
	}
	var blog *Takedown
	posts := map[string]*Takedown{}
	for _, td := range all {
		if td.RestoredAt != nil {
			continue
		}
		t := &Takedown{ReasonCode: td.ReasonCode, Reason: ReasonText[td.ReasonCode], Note: td.Note, At: td.TakenDownAt}
		if td.PostSlug == "" {
			blog = t
		} else {
			posts[td.PostSlug] = t
		}
	}
	return blog, posts, nil
}

// BlogPostSummary is one row of the post list.
type BlogPostSummary struct {
	ID           int64      `json:"id"`
	Title        string     `json:"title"`
	Slug         string     `json:"slug"`
	Status       string     `json:"status"`
	Chip         string     `json:"chip" enum:"draft,published,unlisted,taken_down,needs_attention"`
	Problem      string     `json:"problem,omitempty" doc:"Why the post cannot be served (chip needs_attention)."`
	PublishedAt  *time.Time `json:"published_at,omitempty"`
	LastEditedAt *time.Time `json:"last_edited_at,omitempty"`
}

// BlogPostList is a page of the post list.
type BlogPostList struct {
	Items      []BlogPostSummary `json:"items"`
	NextCursor string            `json:"next_cursor,omitempty"`
}

// BlogPost is one post in the editor.
type BlogPost struct {
	ID          int64      `json:"id"`
	Version     int64      `json:"_version"`
	Title       string     `json:"title"`
	Slug        string     `json:"slug"`
	Summary     string     `json:"summary"`
	BodyMD      string     `json:"body_md"`
	Status      string     `json:"status" enum:"draft,published,unlisted"`
	Chip        string     `json:"chip" enum:"draft,published,unlisted,taken_down,needs_attention"`
	Problem     string     `json:"problem,omitempty"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	PublicURL   string     `json:"public_url" doc:"Where the post is (or would be) served."`
	Cover       string     `json:"cover,omitempty" doc:"The cover image's workspace file id."`
	CoverAlt    string     `json:"cover_alt,omitempty"`
	Takedown    *Takedown  `json:"takedown,omitempty" doc:"An administrator's active take-down of this post."`
}

// PostInput is what the editor sends; nil fields are unchanged.
type PostInput struct {
	Title       *string `json:"title,omitempty" maxLength:"200"`
	Slug        *string `json:"slug,omitempty" maxLength:"200"`
	Summary     *string `json:"summary,omitempty" maxLength:"300"`
	BodyMD      *string `json:"body_md,omitempty" maxLength:"204800"`
	PublishedAt *string `json:"published_at,omitempty" maxLength:"40" doc:"RFC 3339; changes the date shown, for back-dating."`
	Cover       *string `json:"cover,omitempty" maxLength:"26" doc:"A workspace image file id for the cover; empty removes it."`
	CoverAlt    *string `json:"cover_alt,omitempty" maxLength:"200" doc:"The cover's alt text; required with a cover."`
}

// SlugCheck is the answer to a slug check.
type SlugCheck struct {
	Slug      string `json:"slug" doc:"The slug the server would use: normalised and, if taken, suffixed."`
	Available bool   `json:"available" doc:"Whether the requested slug, normalised, is free as it is."`
}

// Service is the blog authoring API (ADR 0017): the routes under
// /api/v1/w/{ws}/blog call it. Every write goes through the records layer,
// so history, quotas, _version and the post rules behave as for any row.
type Service struct {
	Admin   *admin.Store
	Records *records.Service
	Schemas *catalog.Cache
	// PublicURL is ABIDATA_PUBLIC_URL; public links are built from it.
	PublicURL string
	// PublicEnabled reports the install-wide switch (nil: off).
	PublicEnabled func(ctx context.Context) bool
	Now           func() time.Time

	limOnce sync.Once
	lim     *hourly
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) limiter() *hourly {
	s.limOnce.Do(func() { s.lim = &hourly{counts: map[string]*window{}} })
	return s.lim
}

// HomeURL is the public address of a user's blog.
func (s *Service) HomeURL(username string) string {
	return strings.TrimRight(s.PublicURL, "/") + "/b/" + username
}

// Info returns the blog home.
func (s *Service) Info(ctx context.Context, b admin.Blog) (BlogInfo, error) {
	td, _, err := s.activeTakedowns(ctx, b.ID)
	if err != nil {
		return BlogInfo{}, err
	}
	return BlogInfo{ID: b.ID, WorkspaceID: string(b.WorkspaceID), Title: b.Title, Intro: b.Intro, Published: b.Published,
		PublishedAt: b.PublishedAt, Username: b.Username, PublicURL: s.HomeURL(b.Username),
		PublicBlogsEnabled: s.PublicEnabled != nil && s.PublicEnabled(ctx), Takedown: td}, nil
}

// UpdateInfo sets the blog's title and intro.
func (s *Service) UpdateInfo(ctx context.Context, b admin.Blog, title, intro *string) (BlogInfo, error) {
	if title != nil {
		b.Title = strings.TrimSpace(*title)
	}
	if intro != nil {
		b.Intro = strings.TrimSpace(strings.ReplaceAll(*intro, "\r\n", "\n"))
	}
	if utf8.RuneCountInString(b.Title) > MaxBlogTitle {
		return BlogInfo{}, fault.Newf(fault.BadRequest, "The blog title can be at most %d characters.", MaxBlogTitle)
	}
	if utf8.RuneCountInString(b.Intro) > MaxIntro {
		return BlogInfo{}, fault.Newf(fault.BadRequest, "The intro can be at most %d characters.", MaxIntro)
	}
	if err := s.Admin.UpdateBlog(ctx, b.ID, b.Title, b.Intro, s.now()); err != nil {
		return BlogInfo{}, err
	}
	return s.reload(ctx, b)
}

func (s *Service) reload(ctx context.Context, b admin.Blog) (BlogInfo, error) {
	b, err := s.Admin.GetBlog(ctx, b.ID)
	if err != nil {
		return BlogInfo{}, err
	}
	return s.Info(ctx, b)
}

// SetPublished switches the blog on or off. It is rate-limited and audited.
func (s *Service) SetPublished(ctx context.Context, b admin.Blog, userID string, on bool) (BlogInfo, error) {
	if on == b.Published {
		return s.Info(ctx, b)
	}
	if err := s.limiter().spend("user:"+userID, PublishesPerHour, s.now()); err != nil {
		return BlogInfo{}, err
	}
	now := s.now()
	if err := s.Admin.SetBlogPublished(ctx, b.ID, on, now); err != nil {
		return BlogInfo{}, err
	}
	action := "blog.unpublish"
	if on {
		action = "blog.publish"
	}
	_ = s.Admin.Audit(context.WithoutCancel(ctx), admin.AuditEntry{At: now, UserID: userID, Action: action, ObjectKind: "blog", ObjectID: b.ID,
		Details: map[string]any{"username": b.Username}})
	return s.reload(ctx, b)
}

// open loads the workspace's schema and pins.
func (s *Service) open(ctx context.Context, l *workspace.Lease) (*catalog.Schema, Pins, error) {
	var pins Pins
	var ok bool
	if err := l.Read(ctx, func(q dbx.Querier) error {
		var err error
		pins, ok, err = ReadPins(ctx, q)
		return err
	}); err != nil {
		return nil, Pins{}, err
	}
	sch, err := s.Schemas.Get(ctx, l)
	if err != nil {
		return nil, Pins{}, err
	}
	if !ok || len(pins.Missing(sch)) > 0 {
		return nil, Pins{}, fault.New(fault.BadRequest,
			"This blog's Posts table is missing or has been changed, so the blog editor cannot open it. Open it as a workspace to look, or ask an administrator.")
	}
	return sch, pins, nil
}

func chip(p Post, down *Takedown) string {
	if down != nil {
		return ChipTakenDown
	}
	if p.Problem() != "" {
		return ChipNeedsAttention
	}
	return p.Status
}

// ListPosts returns a page of the post list, newest edit first. status
// filters by chip ("" for all); q matches titles, ignoring case.
func (s *Service) ListPosts(ctx context.Context, l *workspace.Lease, b admin.Blog, status, q, cursor string) (BlogPostList, error) {
	sch, pins, err := s.open(ctx, l)
	if err != nil {
		return BlogPostList{}, err
	}
	_, downs, err := s.activeTakedowns(ctx, b.ID)
	if err != nil {
		return BlogPostList{}, err
	}
	var posts []Post
	var edited map[int64]time.Time
	err = l.Read(ctx, func(db dbx.Querier) error {
		var err error
		if posts, err = readPosts(ctx, db, sch, pins, postQuery{}); err != nil {
			return err
		}
		edited, err = lastEdited(ctx, db, pins.Posts)
		return err
	})
	if err != nil {
		return BlogPostList{}, err
	}
	q = strings.ToLower(strings.TrimSpace(q))
	items := []BlogPostSummary{}
	for _, p := range posts {
		c := chip(p, downs[p.Slug])
		if (status != "" && c != status) || (q != "" && !strings.Contains(strings.ToLower(p.Title), q)) {
			continue
		}
		it := BlogPostSummary{ID: p.ID, Title: p.Title, Slug: p.Slug, Status: p.Status, Chip: c, Problem: p.Problem(), PublishedAt: p.PublishedAt}
		if t, ok := edited[p.ID]; ok {
			it.LastEditedAt = &t
		}
		items = append(items, it)
	}
	slices.SortStableFunc(items, func(a, b BlogPostSummary) int {
		at, bt := time.Time{}, time.Time{}
		if a.LastEditedAt != nil {
			at = *a.LastEditedAt
		}
		if b.LastEditedAt != nil {
			bt = *b.LastEditedAt
		}
		if c := bt.Compare(at); c != 0 {
			return c
		}
		return int(b.ID - a.ID)
	})
	off, _ := strconv.Atoi(cursor)
	off = max(0, min(off, len(items)))
	out := BlogPostList{Items: items[off:min(len(items), off+pageSize)]}
	if off+pageSize < len(items) {
		out.NextCursor = strconv.Itoa(off + pageSize)
	}
	return out, nil
}

// GetPost returns one post with its body.
func (s *Service) GetPost(ctx context.Context, l *workspace.Lease, b admin.Blog, id int64) (BlogPost, error) {
	sch, pins, err := s.open(ctx, l)
	if err != nil {
		return BlogPost{}, err
	}
	return s.getPost(ctx, l, sch, pins, b, id)
}

func (s *Service) getPost(ctx context.Context, l *workspace.Lease, sch *catalog.Schema, pins Pins, b admin.Blog, id int64) (BlogPost, error) {
	var posts []Post
	if err := l.Read(ctx, func(db dbx.Querier) error {
		var err error
		posts, err = readPosts(ctx, db, sch, pins, postQuery{withBody: true, where: byID(id)})
		return err
	}); err != nil {
		return BlogPost{}, err
	}
	if len(posts) == 0 {
		return BlogPost{}, fault.E(fault.NotFound)
	}
	p := posts[0]
	_, downs, err := s.activeTakedowns(ctx, b.ID)
	if err != nil {
		return BlogPost{}, err
	}
	return BlogPost{ID: p.ID, Version: p.Version, Title: p.Title, Slug: p.Slug, Summary: p.Summary, BodyMD: p.Body,
		Status: p.Status, Chip: chip(p, downs[p.Slug]), Problem: p.Problem(), PublishedAt: p.PublishedAt,
		PublicURL: s.HomeURL(b.Username) + "/" + p.Slug, Takedown: downs[p.Slug], Cover: p.Cover, CoverAlt: p.CoverAlt}, nil
}

func (in PostInput) fields(pins Pins) (map[string]any, error) {
	c := pins.Columns
	f := map[string]any{}
	if in.Title != nil {
		t := strings.TrimSpace(*in.Title)
		if t == "" {
			return nil, fault.New(fault.BadRequest, "Enter a title.").WithDetails(map[string]any{"field": "title"})
		}
		f[c["title"]] = t
	}
	if in.Slug != nil {
		f[c["slug"]] = *in.Slug
	}
	if in.Summary != nil {
		f[c["summary"]] = strings.TrimSpace(*in.Summary)
	}
	if in.BodyMD != nil {
		if len(*in.BodyMD) > MaxBodySize {
			return nil, fault.Newf(fault.BadRequest, "A post can be at most %d KB.", MaxBodySize>>10).WithDetails(map[string]any{"field": "body_md"})
		}
		f[c["body_md"]] = *in.BodyMD
	}
	if in.PublishedAt != nil {
		if strings.TrimSpace(*in.PublishedAt) == "" {
			f[c["published_at"]] = nil
		} else if _, err := time.Parse(time.RFC3339, *in.PublishedAt); err != nil {
			return nil, fault.New(fault.BadRequest, "The publish date must be a date and time.").WithDetails(map[string]any{"field": "published_at"})
		} else {
			f[c["published_at"]] = *in.PublishedAt
		}
	}
	if in.Cover != nil || in.CoverAlt != nil {
		if _, ok := c["cover"]; !ok {
			return nil, fault.New(fault.BadRequest, "This blog gets its cover field the next time you sign in.").
				WithDetails(map[string]any{"field": "cover"})
		}
	}
	if in.Cover != nil {
		switch id := strings.TrimSpace(*in.Cover); {
		case id == "":
			f[c["cover"]] = nil
		case !ids.Valid(id):
			return nil, fault.New(fault.BadRequest, "The cover must be a workspace file.").WithDetails(map[string]any{"field": "cover"})
		default:
			f[c["cover"]] = []any{id}
		}
	}
	if in.CoverAlt != nil {
		alt := strings.TrimSpace(*in.CoverAlt)
		if utf8.RuneCountInString(alt) > MaxCoverAlt {
			return nil, fault.Newf(fault.BadRequest, "The alt text can be at most %d characters.", MaxCoverAlt).WithDetails(map[string]any{"field": "cover_alt"})
		}
		f[c["cover_alt"]] = alt
	}
	return f, nil
}

// CreatePost creates a draft.
func (s *Service) CreatePost(ctx context.Context, l *workspace.Lease, b admin.Blog, actor records.Actor, in PostInput) (BlogPost, error) {
	sch, pins, err := s.open(ctx, l)
	if err != nil {
		return BlogPost{}, err
	}
	if in.Title == nil {
		return BlogPost{}, fault.New(fault.BadRequest, "Enter a title.").WithDetails(map[string]any{"field": "title"})
	}
	f, err := in.fields(pins)
	if err != nil {
		return BlogPost{}, err
	}
	f[pins.Columns["status"]] = StatusDraft
	row, err := s.Records.Insert(ctx, l, pins.Posts, f, actor, nil)
	if err != nil {
		return BlogPost{}, err
	}
	return s.getPost(ctx, l, sch, pins, b, row.ID)
}

// UpdatePost saves the editor's fields if the post is still at version.
func (s *Service) UpdatePost(ctx context.Context, l *workspace.Lease, b admin.Blog, actor records.Actor, id, version int64, in PostInput) (BlogPost, error) {
	sch, pins, err := s.open(ctx, l)
	if err != nil {
		return BlogPost{}, err
	}
	f, err := in.fields(pins)
	if err != nil {
		return BlogPost{}, err
	}
	if len(f) > 0 {
		if _, err := s.Records.Update(ctx, l, pins.Posts, id, version, f, actor, nil); err != nil {
			return BlogPost{}, err
		}
	}
	return s.getPost(ctx, l, sch, pins, b, id)
}

// SetStatus publishes, unlists or unpublishes a post. It is rate-limited.
func (s *Service) SetStatus(ctx context.Context, l *workspace.Lease, b admin.Blog, actor records.Actor, id, version int64, status string) (BlogPost, error) {
	sch, pins, err := s.open(ctx, l)
	if err != nil {
		return BlogPost{}, err
	}
	cur, err := s.getPost(ctx, l, sch, pins, b, id)
	if err != nil {
		return BlogPost{}, err
	}
	if cur.Status == status {
		return cur, nil
	}
	if status != StatusDraft && cur.Problem != "" {
		return BlogPost{}, fault.New(fault.BadRequest, cur.Problem+" Fix it before publishing.")
	}
	if status != StatusDraft {
		blogDown, _, err := s.activeTakedowns(ctx, b.ID)
		if err != nil {
			return BlogPost{}, err
		}
		if cur.Takedown != nil || blogDown != nil {
			return BlogPost{}, fault.New(fault.BadRequest, "An administrator took this down. You can still edit it, but it cannot be published until an administrator restores it after a review.")
		}
	}
	if err := s.limiter().spend("user:"+actor.UserID, PublishesPerHour, s.now()); err != nil {
		return BlogPost{}, err
	}
	if _, err := s.Records.Update(ctx, l, pins.Posts, id, version, map[string]any{pins.Columns["status"]: status}, actor, nil); err != nil {
		return BlogPost{}, err
	}
	return s.getPost(ctx, l, sch, pins, b, id)
}

// DeletePost deletes a post as a records delete does.
func (s *Service) DeletePost(ctx context.Context, l *workspace.Lease, actor records.Actor, id, version int64) error {
	_, pins, err := s.open(ctx, l)
	if err != nil {
		return err
	}
	return s.Records.Delete(ctx, l, pins.Posts, id, version, actor, nil)
}

// CheckSlug normalises a requested slug and says whether it is free; Slug
// is the value a save would use.
func (s *Service) CheckSlug(ctx context.Context, l *workspace.Lease, requested string, self int64) (SlugCheck, error) {
	sch, pins, err := s.open(ctx, l)
	if err != nil {
		return SlugCheck{}, err
	}
	t, _ := sch.Table(pins.Posts)
	base := Slugify(requested)
	if base == "" {
		base = "post"
	}
	var out SlugCheck
	err = l.Read(ctx, func(db dbx.Querier) error {
		c, _ := t.Column(pins.Columns["slug"])
		taken, err := slugTaken(ctx, db, t.PhysicalName, c.PhysicalName, base, self)
		if err != nil {
			return err
		}
		out.Available = !taken && !reservedSlugs[base]
		out.Slug = base
		if !out.Available {
			for n := 2; n < 10000; n++ {
				suffix := "-" + strconv.Itoa(n)
				cand := trimSlug(base, MaxSlug-len(suffix)) + suffix
				if reservedSlugs[cand] {
					continue
				}
				if taken, err := slugTaken(ctx, db, t.PhysicalName, c.PhysicalName, cand, self); err != nil {
					return err
				} else if !taken {
					out.Slug = cand
					break
				}
			}
		}
		return nil
	})
	return out, err
}

// PreviewImageURL is the authenticated file route: drafts' images are not public.
func PreviewImageURL(ws ids.WorkspaceID) ImageURL {
	return func(id string) string { return "/api/v1/w/" + string(ws) + "/files/" + id + "/content" }
}

// hourly is a fixed-window counter per key (publish changes per user).
type hourly struct {
	mu     sync.Mutex
	counts map[string]*window
}

type window struct {
	start time.Time
	n     int
}

func (h *hourly) spend(key string, limit int, now time.Time) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	w, ok := h.counts[key]
	if !ok || now.Sub(w.start) >= time.Hour {
		if len(h.counts) > 10000 {
			for k, v := range h.counts {
				if now.Sub(v.start) >= time.Hour {
					delete(h.counts, k)
				}
			}
		}
		w = &window{start: now}
		h.counts[key] = w
	}
	if w.n >= limit {
		return &fault.Error{Code: fault.RateLimited, RetryAfter: w.start.Add(time.Hour).Sub(now),
			Message: "You can publish or unpublish at most 10 times an hour. Try again later."}
	}
	w.n++
	return nil
}
