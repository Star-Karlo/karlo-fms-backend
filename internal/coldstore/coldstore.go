// Package coldstore is the read/write addressing layer for archived rows in the
// cold S3 bucket. Retention writes aged rows here (gzipped NDJSON, one JSON
// object per line via row_to_json); the alert layer reads them back to serve a
// transparent hot+cold history, and the admin restore endpoint reads them to
// reinsert into the DB.
//
// Keeping the key format and the reader in one place means the writer and the
// two readers can never drift on how a partition is addressed.
//
// Layout (Hive-partitioned so it stays queryable by external tools too):
//
//	archive/<table>/tenant=<id>/dt=<YYYY-MM-DD>/part-<nano>.ndjson.gz
package coldstore

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Store reads and lists archive objects in the cold bucket.
type Store struct {
	s3     *s3.Client
	bucket string
}

// New builds a Store, or returns nil when bucket is empty (cold storage
// disabled) or AWS config cannot be loaded — callers treat nil as "hot only".
func New(ctx context.Context, region, bucket string) *Store {
	if bucket == "" {
		return nil
	}
	cfg, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(region))
	if err != nil {
		return nil
	}
	return &Store{s3: s3.NewFromConfig(cfg), bucket: bucket}
}

// Prefix is the listing prefix for one table+tenant.
func Prefix(table string, tenantID int64) string {
	return fmt.Sprintf("archive/%s/tenant=%d/", table, tenantID)
}

// Key is the object key for one archive part. nano makes it unique within a day.
func Key(table string, tenantID int64, partitionDay time.Time, nano int64) string {
	return fmt.Sprintf("%sdt=%s/part-%d.ndjson.gz",
		Prefix(table, tenantID), partitionDay.UTC().Format("2006-01-02"), nano)
}

// PartitionDate extracts the dt= day from a key, if present.
func PartitionDate(key string) (time.Time, bool) {
	for _, seg := range strings.Split(key, "/") {
		if v, ok := strings.CutPrefix(seg, "dt="); ok {
			if d, err := time.Parse("2006-01-02", v); err == nil {
				return d, true
			}
		}
	}
	return time.Time{}, false
}

// DayOf truncates t to midnight UTC — the granularity of a partition.
func DayOf(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// Bucket reports the configured bucket name (for the writer).
func (s *Store) Bucket() string { return s.bucket }

// Put writes an object (used by the archiver). Kept here so the writer and
// readers share one S3 client construction path.
func (s *Store) Put(ctx context.Context, in *s3.PutObjectInput) error {
	_, err := s.s3.PutObject(ctx, in)
	return err
}

// Lines returns every JSON row from the archive objects for table+tenant whose
// partition day falls in [from, to] (inclusive, day-aligned). Objects outside
// the day range are never fetched, so a narrow range stays cheap.
func (s *Store) Lines(ctx context.Context, table string, tenantID int64, from, to time.Time) ([]json.RawMessage, error) {
	fromD, toD := DayOf(from), DayOf(to)
	prefix := Prefix(table, tenantID)

	var out []json.RawMessage
	var token *string
	for {
		page, err := s.s3.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket: aws.String(s.bucket), Prefix: aws.String(prefix), ContinuationToken: token,
		})
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", prefix, err)
		}
		for _, o := range page.Contents {
			key := aws.ToString(o.Key)
			d, ok := PartitionDate(key)
			if !ok || d.Before(fromD) || d.After(toD) {
				continue
			}
			lines, err := s.fetch(ctx, key)
			if err != nil {
				return nil, err
			}
			out = append(out, lines...)
		}
		if page.IsTruncated != nil && *page.IsTruncated {
			token = page.NextContinuationToken
			continue
		}
		break
	}
	return out, nil
}

func (s *Store) fetch(ctx context.Context, key string) ([]json.RawMessage, error) {
	obj, err := s.s3.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", key, err)
	}
	defer obj.Body.Close()
	gz, err := gzip.NewReader(obj.Body)
	if err != nil {
		return nil, fmt.Errorf("gunzip %s: %w", key, err)
	}
	defer gz.Close()
	raw, err := io.ReadAll(gz)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", key, err)
	}
	var lines []json.RawMessage
	for _, ln := range bytes.Split(raw, []byte("\n")) {
		if len(bytes.TrimSpace(ln)) > 0 {
			lines = append(lines, json.RawMessage(append([]byte(nil), ln...)))
		}
	}
	return lines, nil
}
