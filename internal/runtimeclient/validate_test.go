package runtimeclient

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestValidatePassesEachPlatformCoreOnceAcrossMultipleTargets(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o700); err != nil {
		t.Fatal(err)
	}
	script := `import json, sys
for line in sys.stdin:
    request=json.loads(line)
    directory=request['input']['items'][0]['directory']
    valid=(request['command']=='batch-identity' and directory=={
        'platformId':'platform', 'defaultCoreId':'multi-target',
        'allowedCoreIds':['multi-target', 'another-core']})
    result=[{'error':'' if valid else 'RUNTIME_CORE_UNAVAILABLE'}]
    print(json.dumps({'id':request['id'],'result':result}), flush=True)
`
	if err := os.WriteFile(filepath.Join(root, "scripts/runtime-cli.mjs"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	pool := newWorkerPool(t.Context(), root, "python3")
	t.Cleanup(pool.Close)
	client := &Client{pool: pool, Bindings: []Binding{
		{CoreID: "unrelated", PlatformIDs: []string{"another-platform"}},
		{CoreID: "multi-target", TargetID: "first", PlatformIDs: []string{"platform"}},
		{CoreID: "multi-target", TargetID: "second", PlatformIDs: []string{"platform"}},
		{CoreID: "another-core", PlatformIDs: []string{"platform"}},
	}}
	if err := client.Validate(t.Context(), "platform", json.RawMessage(`{}`), nil); err != nil {
		t.Fatalf("validating a core shared by multiple targets: %v", err)
	}
}
