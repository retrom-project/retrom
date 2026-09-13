package launch

import "testing"

func TestApplicationUsesNetplayCreationUseCase(t *testing.T) {
	creator, repository, request := netplayCreatorFixture(t)
	service := New(ServiceDependencies{Netplay: creator})
	created, err := service.CreateNetplay(t.Context(), request)
	if err != nil || created.LaunchID == "" || len(repository.writes) != 1 {
		t.Fatalf("application netplay creation: %v", err)
	}
}
