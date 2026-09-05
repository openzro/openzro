// Package compact rewrites an archive day's many small objects into one
// object per account.
//
// The sinks flush every fifteen minutes, and a low-traffic account
// produces a parquet file of a few kilobytes each time. Measured on a
// production bucket: 13,185 objects averaging 7 KB, roughly 150 per day
// per account. Reading a single day meant opening ~150 objects, and with
// the reader's partition margin a one-day question opened 579 -- about
// 2,000 HTTP round trips to move 10 MB, which took 64 to 81 seconds.
//
// The bytes were never the problem. Parquet is built for files in the
// tens of megabytes, where the footer read that precedes every file is
// amortised over a lot of data; at 7 KB the round trip is the whole
// cost. Compaction does not make the archive smaller, it makes it
// answerable: one file per account-day turns 579 opens into 4.
//
// It also repartitions. Objects written before the sinks split a batch
// by account carry rows for accounts other than the one in their path
// (see openzro#186), and rewriting groups every row under the account it
// actually belongs to -- which is the only way those events become
// reachable again from the account that owns them.
package compact

import "context"

// ObjectStore is the write half of compaction. Reads go through DuckDB,
// which already knows how to glob and authenticate against the archive;
// this covers what DuckDB is not used for, deliberately.
//
// Writing through the same SDK the sinks write with, rather than through
// DuckDB's httpfs, is a choice about what is already proven. The sinks
// have been writing this bucket for months. Whether httpfs can write to
// GCS through an S3-compatible endpoint with HMAC credentials is a
// question nobody here has answered, and compaction is not the place to
// find out: it is the one operation in this codebase that deletes data.
type ObjectStore interface {
	// List returns every object key under prefix, in any order.
	List(ctx context.Context, prefix string) ([]string, error)
	// Put writes body at key, overwriting whatever is there.
	Put(ctx context.Context, key string, body []byte) error
	// Delete removes key. Deleting an absent key is not an error, so a
	// retried compaction converges instead of failing.
	Delete(ctx context.Context, key string) error
}
