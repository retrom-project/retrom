package filestore

import (
	"context"
	"fmt"
)

type publicationLock struct {
	token chan struct{}
	users int
}

// LockPublication coordinates one item's resumable directory publication.
// Unrelated directories can be prepared concurrently; database content claims
// remain the caller's responsibility. Waiting never outlives the request.
func (store *Store) LockPublication(ctx context.Context, itemID string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("acquire publication lock: %w", err)
	}
	store.publicationMu.Lock()
	if store.publications == nil {
		store.publications = make(map[string]*publicationLock)
	}
	lock := store.publications[itemID]
	if lock == nil {
		lock = &publicationLock{token: make(chan struct{}, 1)}
		lock.token <- struct{}{}
		store.publications[itemID] = lock
	}
	lock.users++
	store.publicationMu.Unlock()
	select {
	case <-ctx.Done():
		store.releasePublicationLock(itemID, lock)
		return nil, fmt.Errorf("wait for publication lock: %w", ctx.Err())
	case <-lock.token:
	}
	unlock := func() {
		lock.token <- struct{}{}
		store.releasePublicationLock(itemID, lock)
	}
	if err := ctx.Err(); err != nil {
		unlock()
		return nil, fmt.Errorf("acquire publication lock: %w", err)
	}
	return unlock, nil
}

func (store *Store) releasePublicationLock(itemID string, lock *publicationLock) {
	store.publicationMu.Lock()
	defer store.publicationMu.Unlock()
	lock.users--
	if lock.users == 0 {
		delete(store.publications, itemID)
	}
}
