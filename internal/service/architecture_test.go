package service

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestServicesDependOnBusinessPorts(t *testing.T) {
	t.Parallel()
	architecture.AssertBusinessImportsTree(t)
}

func TestServicesDoNotDependOnConcreteRepositories(t *testing.T) {
	t.Parallel()
	architecture.AssertNoImportsIn(t, architecture.PackageDirectory(t), "retrom/internal/repo/")
}
