package libraryimport

import (
	"retrom/internal/scummvm"
)

func (service *Service) WithScummVMDetector(detector *scummvm.Detector) *Service {
	service.scummVMDetector = detector
	return service
}
