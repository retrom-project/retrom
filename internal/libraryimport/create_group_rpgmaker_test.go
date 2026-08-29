package libraryimport

import (
	"testing"

	"retrom/internal/rpgmaker/detector"
	"retrom/internal/rpgmaker/routing"
)

func TestResolveRPGCreationRouteAcceptsRegisteredRoute(t *testing.T) {
	t.Parallel()
	route, err := routing.ByRoute("rpgmaker_2000", "RPG2000_EASYRPG")
	if err != nil {
		t.Fatal(err)
	}
	target := creationTarget{
		coreID: "rpgmaker_2000", routeKey: route.RouteKey,
		runtimeFamily: routing.FamilyRPGMaker, adapterID: route.AdapterID, adapterABI: route.AdapterABI,
	}
	resolved, err := resolveRPGCreationRoute(target, detector.RPG2000)
	if err != nil {
		t.Fatalf("resolveRPGCreationRoute() error = %v", err)
	}
	if resolved.RouteKey != route.RouteKey || !resolved.SelectedForNewBindings {
		t.Fatalf("resolved route = %#v", resolved)
	}
}

func TestResolveRPGCreationRouteRejectsGenerationOrAdapterDrift(t *testing.T) {
	t.Parallel()
	route, err := routing.ByRoute("rpgmaker_2000", "RPG2000_EASYRPG")
	if err != nil {
		t.Fatal(err)
	}
	target := creationTarget{
		coreID: "rpgmaker_2000", routeKey: route.RouteKey,
		runtimeFamily: routing.FamilyRPGMaker, adapterID: route.AdapterID, adapterABI: route.AdapterABI,
	}
	if _, err := resolveRPGCreationRoute(target, detector.RPG2003); err == nil {
		t.Fatal("generation drift was accepted")
	}
	target.adapterID = "wrong-adapter"
	if _, err := resolveRPGCreationRoute(target, detector.RPG2000); err == nil {
		t.Fatal("adapter drift was accepted")
	}
}
