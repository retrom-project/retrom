package libraryimport

import (
	librarymodel "retrom/internal/model/libraryimport"
)

func (service *Service) WithScummVMDetector(detector librarymodel.ScummVMDetector) *Service {
	service.scummVMDetector = detector
	return service
}
