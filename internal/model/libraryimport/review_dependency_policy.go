package libraryimport

func (attachment MultiDiscAttachment) Retryable() bool {
	return attachment.State == "FAILED_RETRYABLE" && attachment.JobState == "FAILED" &&
		attachment.ErrorRetryable != nil && *attachment.ErrorRetryable
}

func (attachment MultiDiscAttachment) Active() bool {
	pending := attachment.State == "QUEUED" || attachment.State == "RUNNING" || attachment.State == "FAILED_RETRYABLE"
	return pending && (attachment.JobState == "QUEUED" || attachment.JobState == "RUNNING")
}
