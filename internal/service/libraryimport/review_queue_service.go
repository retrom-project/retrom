package libraryimport

import model "retrom/internal/model/libraryimport"

type ReviewQueue struct {
	repository model.ReviewQueueRepository
	tags       model.ReviewQueueTags
}
