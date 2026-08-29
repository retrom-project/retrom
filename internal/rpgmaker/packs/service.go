package packs

import (
	"context"
	"database/sql"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/payloadrelease"
)

const (
	maxPackFiles = 10_000
	maxPackBytes = int64(512 << 20)
)

type Service struct {
	database *sql.DB
	blobs    *blobstore.Store
	releases *payloadrelease.Service
	now      func() time.Time
}

func New(
	database *sql.DB,
	blobs *blobstore.Store,
	releases *payloadrelease.Service,
	now func() time.Time,
) *Service {
	return &Service{database: database, blobs: blobs, releases: releases, now: now}
}

func (service *Service) ResumeQueuedJobs(ctx context.Context) {
	service.resumeQueuedJobs(ctx)
}
