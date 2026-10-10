package ingest

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/sqlitex"
)

// UploadOptions describe an upload.
type UploadOptions struct {
	Filename  string
	Format    string // csv, xlsx, json, ndjson; empty = from the file name
	Delimiter string
	NoHeader  bool
	Sheet     string
}

// FormatFor picks the format from an explicit value or the file extension.
func FormatFor(format, filename string) (string, error) {
	f := strings.ToLower(strings.TrimSpace(format))
	if f == "" {
		switch strings.ToLower(filepath.Ext(filename)) {
		case ".csv", ".tsv", ".txt":
			f = "csv"
		case ".xlsx":
			f = "xlsx"
		case ".json":
			f = "json"
		case ".ndjson", ".jsonl":
			f = "ndjson"
		}
	}
	switch f {
	case "csv", "xlsx", "json", "ndjson":
		return f, nil
	}
	return "", fault.New(fault.BadRequest, "Unsupported file type. Upload a .csv, .xlsx, .json or .ndjson file.")
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// Upload streams file to staging (at most MaxUploadBytes), parses it into a
// staging database and returns the import's meta. Parsing is synchronous; the
// size and row caps bound it.
func (s *Service) Upload(ctx context.Context, ws ids.WorkspaceID, by string, opt UploadOptions, file io.Reader) (m ImportMeta, err error) {
	format, err := FormatFor(opt.Format, opt.Filename)
	if err != nil {
		return ImportMeta{}, err
	}
	dir, err := s.stagingDir(ws)
	if err != nil {
		return ImportMeta{}, err
	}
	if err := checkStagingQuota(dir); err != nil {
		return ImportMeta{}, err
	}
	id := ids.New()
	ext := map[string]string{"csv": "csv", "xlsx": "xlsx", "json": "json", "ndjson": "ndjson"}[format]
	srcPath, err := ids.SafeJoin(dir, id+"."+ext)
	if err != nil {
		return ImportMeta{}, err
	}
	dbPath, err := ids.SafeJoin(dir, id+".db")
	if err != nil {
		return ImportMeta{}, err
	}
	defer func() {
		if err != nil {
			s.removeFiles(ws, id)
		}
	}()
	f, err := os.OpenFile(srcPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // server-generated id
	if err != nil {
		return ImportMeta{}, err
	}
	n, err := io.Copy(f, io.LimitReader(ctxReader{ctx, file}, MaxUploadBytes+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return ImportMeta{}, err
	}
	if n > MaxUploadBytes {
		return ImportMeta{}, fault.Newf(fault.BadRequest, "The file is larger than the %d MiB upload limit.", MaxUploadBytes>>20)
	}
	if n == 0 {
		return ImportMeta{}, fault.New(fault.BadRequest, "The file is empty.")
	}

	src, err := openSource(srcPath, format, ParseOptions{Delimiter: opt.Delimiter, HasHeader: !opt.NoHeader, Sheet: opt.Sheet})
	if err != nil {
		return ImportMeta{}, err
	}
	defer src.close()

	names := cleanNames(src.header)
	stage := make([]string, src.ncols)
	for i := range stage {
		stage[i] = stagingCol(i)
	}
	c, err := sqlitex.Open(ctx, dbPath, sqlitex.Options{Kind: sqlitex.Staging, MaxPageCount: MaxImportStagingBytes / stagingPageSize})
	if err != nil {
		return ImportMeta{}, err
	}
	defer func() { _ = c.Close() }()
	if err := createStagingSchema(ctx, c, stage); err != nil {
		return ImportMeta{}, err
	}
	samples := make([][]string, src.ncols)
	rows, err := insertRows(ctx, c, stage, src.ncols, func(row []string) {
		for i, v := range row {
			if len(samples[i]) < InferSample && strings.TrimSpace(v) != "" {
				samples[i] = append(samples[i], v)
			}
		}
	}, src.next)
	if err != nil && fault.Classify(err).Code == fault.QuotaExceeded {
		return ImportMeta{}, fault.Newf(fault.QuotaExceeded, "The file's data expands to more than %d MiB; split it into smaller files.", MaxImportStagingBytes>>20)
	}
	if err != nil {
		return ImportMeta{}, err
	}
	if rows == 0 {
		return ImportMeta{}, fault.New(fault.BadRequest, "The file has a header but no data rows.")
	}
	orig := make([]string, len(src.header))
	copy(orig, src.header)
	if !src.opts.HasHeader {
		for i := range orig {
			orig[i] = ""
		}
	}
	if src.extra > 0 {
		src.warn("%d rows had values beyond the last column; the extra values were ignored.", src.extra)
	}
	warns := src.warns
	if warns == nil {
		warns = []string{}
	}
	m = ImportMeta{ID: id, SourceName: filepath.Base(opt.Filename), Format: format, Options: src.opts,
		Columns: inferColumns(names, orig, samples), RowCount: rows, Status: StatusParsed, CreatedBy: by,
		CreatedAt: s.now().UTC(), SizeBytes: n, Warnings: warns}
	if err := saveMeta(ctx, c, m); err != nil {
		return ImportMeta{}, err
	}
	return m, nil
}

// insertRows loads rows from next into data, 5,000 per transaction.
func insertRows(ctx context.Context, c *sqlitex.Conn, stage []string, ncols int, seen func([]string), next func() ([]string, error)) (int64, error) {
	cols := make([]string, ncols)
	marks := make([]string, ncols)
	for i := range cols {
		cols[i], marks[i] = catalog.Quote(stage[i]), "?"
	}
	sql := `INSERT INTO data(n, ` + strings.Join(cols, ", ") + `) VALUES (?, ` + strings.Join(marks, ", ") + `)`
	args := make([]any, ncols+1)
	var n int64
	done := false
	for !done {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		err := c.Immediate(ctx, func(tx *sqlitex.Tx) error {
			for i := 0; i < 5000; i++ {
				row, err := next()
				if errors.Is(err, io.EOF) {
					done = true
					return nil
				}
				if err != nil {
					return err
				}
				if n >= MaxRows {
					return fault.Newf(fault.BadRequest, "The file has more than %d rows.", MaxRows)
				}
				n++
				args[0] = n
				for j, v := range row {
					if v == "" {
						args[j+1] = nil
					} else {
						args[j+1] = v
					}
				}
				if seen != nil {
					seen(row)
				}
				if _, err := tx.Exec(ctx, sql, args...); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return 0, err
		}
	}
	return n, nil
}

// ImportView is an import with a preview of its first rows.
type ImportView struct {
	ImportMeta
	Preview [][]*string `json:"preview" doc:"The first 50 data rows, one entry per source column (null = empty)."`
}

// Get returns an import's meta and preview rows.
func (s *Service) Get(ctx context.Context, ws ids.WorkspaceID, id string) (ImportView, error) {
	path, err := s.importPaths(ws, id)
	if err != nil {
		return ImportView{}, err
	}
	c, err := openStaging(ctx, path)
	if err != nil {
		return ImportView{}, err
	}
	defer func() { _ = c.Close() }()
	m, err := readMeta(ctx, c)
	if err != nil {
		return ImportView{}, errImportNotFound
	}
	v := ImportView{ImportMeta: m, Preview: [][]*string{}}
	cols := make([]string, len(m.Columns))
	for i := range cols {
		cols[i] = catalog.Quote(stagingCol(i))
	}
	rows, err := c.Query(ctx, `SELECT `+strings.Join(cols, ", ")+` FROM data ORDER BY n LIMIT ?`, PreviewRows)
	if err != nil {
		return ImportView{}, err
	}
	defer rows.Close()
	for rows.Next() {
		r := make([]*string, len(cols))
		dst := make([]any, len(cols))
		for i := range r {
			dst[i] = &r[i]
		}
		if err := rows.Scan(dst...); err != nil {
			return ImportView{}, err
		}
		v.Preview = append(v.Preview, r)
	}
	return v, rows.Err()
}
