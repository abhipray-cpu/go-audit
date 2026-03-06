// Package s3 implements the S3 cold storage adapter for go-audit.
//
// This adapter provides the ColdStorePort implementation backed by Amazon S3
// (or S3-compatible stores like MinIO/LocalStack). Versions are stored with
// zstd compression.
//
// Import path: github.com/abhipray-cpu/go-audit/adapter/coldstore/s3
package s3
