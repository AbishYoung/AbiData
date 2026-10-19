import type { components } from "@/generated/api";
import { api } from "@/lib/api";

// The blog authoring API (ADR 0017): /api/v1/w/{ws}/blog, owner only.

export type BlogInfo = components["schemas"]["BlogInfo"];
export type BlogPost = components["schemas"]["BlogPost"];
export type BlogPostSummary = components["schemas"]["BlogPostSummary"];
export type BlogPostList = components["schemas"]["BlogPostList"];
export type PostInput = components["schemas"]["PostInput"];
export type SlugCheck = components["schemas"]["SlugCheck"];
export type PostStatus = BlogPost["status"];
export type Chip = BlogPost["chip"];

export const blogKeys = {
  info: (ws: string) => ["blog", ws, "info"] as const,
  posts: (ws: string, status: string, q: string) => ["blog", ws, "posts", status, q] as const,
  allPosts: (ws: string) => ["blog", ws, "posts"] as const,
  post: (ws: string, id: number) => ["blog", ws, "post", id] as const,
};

export function getBlog(ws: string): Promise<BlogInfo> {
  return api.requestData((c, init) => c.GET("/api/v1/w/{ws}/blog", { ...init, params: { path: { ws } } }), { label: "Loading blog" });
}

export function updateBlog(ws: string, body: { title?: string; intro?: string }): Promise<BlogInfo> {
  return api.requestData((c, init) => c.PATCH("/api/v1/w/{ws}/blog", { ...init, params: { path: { ws } }, body }), { label: "Saving blog settings" });
}

export function setBlogPublished(ws: string, published: boolean): Promise<BlogInfo> {
  return api.requestData((c, init) => c.PUT("/api/v1/w/{ws}/blog/published", { ...init, params: { path: { ws } }, body: { published } }), {
    label: published ? "Turning the blog on" : "Turning the blog off",
  });
}

export function listPosts(ws: string, status: string, q: string, cursor?: string): Promise<BlogPostList> {
  return api.requestData(
    (c, init) =>
      c.GET("/api/v1/w/{ws}/blog/posts", {
        ...init,
        params: { path: { ws }, query: { status: status as never, q, cursor } },
      }),
    { label: "Loading posts" },
  );
}

export function getPost(ws: string, id: number): Promise<BlogPost> {
  return api.requestData((c, init) => c.GET("/api/v1/w/{ws}/blog/posts/{id}", { ...init, params: { path: { ws, id } } }), { label: "Loading post" });
}

export function createPost(ws: string, body: PostInput): Promise<BlogPost> {
  return api.requestData((c, init) => c.POST("/api/v1/w/{ws}/blog/posts", { ...init, params: { path: { ws } }, body }), { label: "Saving post" });
}

export function updatePost(ws: string, id: number, version: number, body: PostInput): Promise<BlogPost> {
  return api.requestData(
    (c, init) => c.PATCH("/api/v1/w/{ws}/blog/posts/{id}", { ...init, params: { path: { ws, id } }, body: { ...body, _version: version } }),
    { label: "Saving post" },
  );
}

export function setPostStatus(ws: string, id: number, version: number, status: PostStatus): Promise<BlogPost> {
  return api.requestData(
    (c, init) => c.PUT("/api/v1/w/{ws}/blog/posts/{id}/status", { ...init, params: { path: { ws, id } }, body: { _version: version, status } }),
    { label: status === "draft" ? "Unpublishing post" : "Publishing post" },
  );
}

export async function deletePost(ws: string, id: number, version: number): Promise<void> {
  await api.request((c, init) => c.DELETE("/api/v1/w/{ws}/blog/posts/{id}", { ...init, params: { path: { ws, id }, query: { version } } }), {
    label: "Deleting post",
  });
}

export async function previewPost(ws: string, title: string, bodyMD: string, signal?: AbortSignal): Promise<string> {
  const res = await api.requestData(
    (c, init) => c.POST("/api/v1/w/{ws}/blog/preview", { ...init, params: { path: { ws } }, body: { title, body_md: bodyMD } }),
    { label: "Rendering preview", class: "background", signal },
  );
  return res.html;
}

export function checkSlug(ws: string, slug: string, id: number): Promise<SlugCheck> {
  return api.requestData((c, init) => c.GET("/api/v1/w/{ws}/blog/slug-check", { ...init, params: { path: { ws }, query: { slug, id } } }), {
    label: "Checking address",
    class: "background",
  });
}

// Moderation (ADR 0017): the admin console's Blogs tab.

export type Takedown = components["schemas"]["Takedown"];
export type AdminBlog = components["schemas"]["AdminBlog"];
export type AdminBlogSummary = components["schemas"]["AdminBlogSummary"];
export type TakedownReason = components["schemas"]["TakedownRequest"]["reason_code"];
export type PublicBlogsSetting = components["schemas"]["PublicBlogsSetting"];

export const REASONS: { value: TakedownReason; label: string }[] = [
  { value: "spam", label: "Spam" },
  { value: "phishing", label: "Phishing" },
  { value: "malware", label: "Malware" },
  { value: "illegal", label: "Illegal content" },
  { value: "harassment", label: "Harassment" },
  { value: "copyright", label: "Copyright" },
  { value: "other", label: "Other" },
];

export const adminBlogKeys = {
  setting: ["admin", "blogs", "setting"] as const,
  list: (q: string, filter: string) => ["admin", "blogs", "list", q, filter] as const,
  all: ["admin", "blogs"] as const,
  one: (id: string) => ["admin", "blogs", "one", id] as const,
};

export function adminGetPublicBlogs(): Promise<PublicBlogsSetting> {
  return api.requestData((c, init) => c.GET("/api/v1/admin/settings/public-blogs", init), { label: "Loading settings" });
}

export function adminPutPublicBlogs(enabled: boolean): Promise<PublicBlogsSetting> {
  return api.requestData((c, init) => c.PUT("/api/v1/admin/settings/public-blogs", { ...init, body: { enabled, forced_off: false } }), {
    label: enabled ? "Turning public blogs on" : "Turning public blogs off",
  });
}

export async function adminListBlogs(q: string, filter: string): Promise<AdminBlogSummary[]> {
  const res = await api.requestData(
    (c, init) => c.GET("/api/v1/admin/blogs", { ...init, params: { query: { q, filter: filter as never } } }),
    { label: "Loading blogs" },
  );
  return res.items ?? [];
}

export function adminGetBlog(id: string): Promise<AdminBlog> {
  return api.requestData((c, init) => c.GET("/api/v1/admin/blogs/{id}", { ...init, params: { path: { id } } }), { label: "Loading blog" });
}

export function adminTakeDown(id: string, body: { post_slug?: string; reason_code: TakedownReason; note?: string }): Promise<AdminBlog> {
  return api.requestData((c, init) => c.POST("/api/v1/admin/blogs/{id}/take-down", { ...init, params: { path: { id } }, body }), { label: "Taking down" });
}

export function adminRestore(id: string, postSlug?: string): Promise<AdminBlog> {
  return api.requestData(
    (c, init) => c.POST("/api/v1/admin/blogs/{id}/restore", { ...init, params: { path: { id } }, body: postSlug ? { post_slug: postSlug } : {} }),
    { label: "Restoring" },
  );
}

export function adminResolveReports(id: string): Promise<AdminBlog> {
  return api.requestData((c, init) => c.POST("/api/v1/admin/blogs/{id}/reports/resolve", { ...init, params: { path: { id } } }), {
    label: "Resolving reports",
  });
}
