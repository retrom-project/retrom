package libraryimport

import (
	"retrom/internal/capability/engine/scummvm"
)

func (service *Service) WithScummVMDetector(detector *scummvm.Detector) *Service {
	service.scummVMDetector = detector
	return service
}
