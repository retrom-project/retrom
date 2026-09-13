package emulationstationimport

import (
	persistence "retrom/internal/repo/emulationstationimport"
	application "retrom/internal/service/emulationstationimport"
)

func (service *Service) scanPublication() *application.ScanPublication {
	return application.NewScanPublication(persistence.NewScanPublication(service.database), service.now)
}

func (service *Service) executionControl() *application.ExecutionControl {
	return application.NewExecutionControl(persistence.NewExecutionControl(service.database), service.now)
}
