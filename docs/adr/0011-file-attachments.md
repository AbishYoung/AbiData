# ADR 0011: File attachments (workspace bucket)

Status: Proposed · 2026-10-11

## Context
Rows need documents: photos of a sighting, permit PDFs, specification sheets. The driving use case is **tracking invasive species**:
- A field crew takes several photos with the phone camera and attaches them to the sighting record.
- Coordinators view them later, in forms and reports.
- External tools use the API to upload images and to fetch them, for example for an identification service.

The platform keeps each workspace in one SQLite file and backs it up as a signed archive. Blobs do not belong in that file, because large rows hurt the writer and backups.

## Decision

**A per-workspace bucket.**
- Files live in a directory next to the workspace database: `<data>/workspaces/<id>.files/`.
- Each file is stored by **SHA-256** in `aa/bb/<sha256>`. Content-addressing deduplicates within a workspace and makes integrity checks free.
- The metadata table `_sys_files` holds:
  - `id`: a ULID, the stable handle used everywhere;
  - `sha256`, `size`, `name` (the original file name, sanitised), and `content_type` (sniffed, not trusted from the client);
  - `width` and `height` for images;
  - `created_at`, `created_by`, and `state` (`pending` or `attached`).

**A field type `attachments`.**
- The value is a JSON array of file ids, stored as TEXT.
- The type option `max_files` sets the limit per cell: 1 makes a single-document field, the default is 20 and the maximum is 100.
- Writes validate that every id exists in this workspace. A field never points into another workspace.

**Reference tracking.**
- `_sys_file_refs(file_id, table_id, row_id, column_id)` is updated in the **same transaction** as every record write: insert, update, delete, import, script writes and restore.
- A file with references is `attached`.

**Lifecycle.**
- A janitor deletes files that were uploaded but never attached within 24 hours.
- It deletes files that lost their last reference only after a grace period of 7 days. Undo and row history can still show them during that time.
- Row history keeps file ids. A history entry whose file was purged shows "file removed".

**Limits.**
- A file may be at most 25 MiB (`ABIDATA_MAX_FILE_BYTES`).
- Workspace storage quota: the existing quota gains `max_file_bytes`, which counts files' bytes. Exceeding it gives `QUOTA_EXCEEDED`.
- Image dimensions are checked with `image.DecodeConfig` before any decode, rejecting more than 40 megapixels. This prevents decompression bombs.

**HTTP API.**
- `POST /w/{ws}/files` uploads one file, editor role or above. It accepts a raw body with an `X-File-Name` header, or `multipart/form-data`. It returns the file's metadata, and the file stays `pending` until a row references it.
- `GET /w/{ws}/files/{id}` returns the metadata. `GET /w/{ws}/files/{id}/content` returns the bytes and needs viewer or above, since all members can read the whole workspace.
- Response headers:
  - `Content-Disposition: attachment` except for safe raster images (PNG, JPEG, GIF, WebP), which are served `inline`;
  - `X-Content-Type-Options: nosniff`;
  - `Content-Security-Policy: sandbox`;
  - `Cache-Control: private, max-age=31536000, immutable`, which is safe because content is addressed by its hash.
- `GET /w/{ws}/files/{id}/thumbnail` returns a JPEG of at most 320 px for images, cached on disk. It decodes with the Go standard library, which supports JPEG, PNG and GIF; for other types it returns 404.
- Attaching a file is an ordinary record write: put the id into the attachments field.
- API keys use the same routes with their role. Upload counts against the key's rate limit.
- Everything is documented in the API guide with `curl` examples.

**Backups and restore.**
- The signed backup archive includes the bucket, and the manifest lists each file's hash.
- Restore puts files back before the database is switched, so references never dangle.
- Restore-as-new copies the files too.
- Workspace deletion removes the bucket.

**Scripts.**
- The Lua `files` module provides `files.info(id)`.
- `files.read(id)` returns at most 1 MiB in UI events and 10 MiB in jobs, and is counted against the memory budget.
- `files.create(name, content, content_type)` is available in jobs only, for example to attach a generated CSV to a row.

**Import and export.**
- CSV export writes the file names, separated by `; `.
- CSV import ignores attachments columns, and says so in the mapping step.

**Where attachments appear in the UI.**
- **Datasheet cells:** a count and small thumbnails.
- **Attachment manager:**
  - drag and drop or browse to add files, plus "Take photo" on phones (`<input accept="image/*" capture="environment">`);
  - remove, reorder, download and a full-screen image viewer.
- **Forms:** an **Attachments** control showing a gallery, editable when the form allows edits.
- **Reports:** an **Image** control showing the first image, or a gallery of up to N images.

## Consequences
- The database stays small, and the files are plain files on disk. Operators back up the same data directory they already do.
- Reference tracking adds a write per changed attachments cell. It is a small cost for safe garbage collection.
- Content addressing means two rows with the same photo share storage, and deleting one row never deletes the other row's file.
