package launch

// ServiceDependencies contains complete use cases assembled at process startup.
type ServiceDependencies struct {
	Product       *ProductCreator
	Preview       *PreviewCreator
	Config        *ConfigIssuer
	Play          *PlayController
	PreviewCloser *PreviewCloser
	Content       *ContentAccess
	Sessions      *SessionQueries
	Projects      *ProjectQueries
	Indexes       *ProjectIndexes
}

type Service struct{ dependencies ServiceDependencies }

func New(dependencies ServiceDependencies) *Service { return &Service{dependencies: dependencies} }
