import { useEffect, useRef, useState } from "react";
import { ApiError } from "@/lib/api";
import { Button, Field, Input } from "@/components/ui";
import { fieldError, label as labelClass } from "@/components/styles";
import { contentUrl, uploadFile, type UploadHandle } from "@/features/files/filesApi";
import { MAX_COVER_ALT } from "./postModel";

const ACCEPT = "image/png,image/jpeg,image/gif,image/webp";

/**
 * The post's cover image (ADR 0017): one image uploaded to the blog workspace's files, with
 * required alt text. The image is saved with the post, like the other fields.
 */
export function CoverField({
  ws,
  cover,
  alt,
  coverError,
  altError,
  onCover,
  onAlt,
}: {
  ws: string;
  cover: string;
  alt: string;
  coverError: string | undefined;
  altError: string | undefined;
  onCover: (id: string) => void;
  onAlt: (alt: string) => void;
}) {
  const fileRef = useRef<HTMLInputElement>(null);
  const upload = useRef<UploadHandle | null>(null);
  const [progress, setProgress] = useState<number | null>(null);
  const [uploadError, setUploadError] = useState<string | null>(null);

  useEffect(() => () => upload.current?.abort(), []);

  async function onFile(file: File | undefined) {
    if (!file) return;
    setUploadError(null);
    setProgress(0);
    const h = uploadFile(ws, file, (loaded, total) => setProgress(total ? Math.round((loaded / total) * 100) : 0));
    upload.current = h;
    try {
      const meta = await h.promise;
      if (!/^image\/(png|jpeg|gif|webp)$/.test(meta.content_type)) {
        setUploadError("The cover must be a PNG, JPEG, GIF or WebP image.");
        return;
      }
      onCover(meta.id);
    } catch (e) {
      if (e instanceof ApiError && e.canceled) return;
      setUploadError(e instanceof Error ? e.message : "The upload failed.");
    } finally {
      upload.current = null;
      setProgress(null);
      if (fileRef.current) fileRef.current.value = "";
    }
  }

  const error = uploadError ?? coverError;
  return (
    <fieldset className="m-0 space-y-2 border-0 p-0">
      <legend className={labelClass}>Cover image</legend>
      <div className="flex flex-wrap items-center gap-3">
        {cover && <img src={contentUrl(ws, cover)} alt="" className="h-24 max-w-48 rounded-md border border-border object-cover" />}
        <input ref={fileRef} type="file" accept={ACCEPT} className="sr-only" tabIndex={-1} aria-hidden="true" onChange={(e) => void onFile(e.target.files?.[0])} />
        <Button size="sm" disabled={progress !== null} onClick={() => fileRef.current?.click()}>
          {cover ? "Replace image" : "Upload image"}
        </Button>
        {cover && (
          <Button size="sm" variant="ghost" disabled={progress !== null} onClick={() => onCover("")}>
            Remove cover
          </Button>
        )}
        {progress !== null && (
          <span role="status" className="text-sm text-fg-muted">
            Uploading… {progress}%
          </span>
        )}
      </div>
      {error && (
        <p role="alert" className={`${fieldError} m-0`}>
          {error}
        </p>
      )}
      {cover && (
        <Field label="Cover alt text" required error={altError} hint={`What the image shows, for readers who cannot see it. ${alt.length} / ${MAX_COVER_ALT}`}>
          <Input value={alt} maxLength={MAX_COVER_ALT} onChange={(e) => onAlt(e.target.value)} />
        </Field>
      )}
    </fieldset>
  );
}
