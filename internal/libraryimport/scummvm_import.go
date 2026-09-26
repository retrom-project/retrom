package libraryimport

import (
	"retrom/internal/core/scummvm"
)

func (service *Service) WithScummVMDetector(detector *scummvm.Detector) *Service {
	service.scummVMDetector = detector
	return service
}
