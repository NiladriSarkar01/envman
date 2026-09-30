package process

import (
	"bytes"
	"io"
	"os"
	"reflect"
	"testing"
)

func TestMergedEnvironmentAppliesOverridesAndSortsKeys(t *testing.T) {
	got := mergedEnvironment(
		[]string{"ZED=last", "PORT=old", "KEEP=unchanged", "INVALID"},
		map[string]string{"PORT": "8080", "NEW": "value"},
	)
	want := []string{"KEEP=unchanged", "NEW=value", "PORT=8080", "ZED=last"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("merged environment = %#v, want %#v", got, want)
	}
}

func TestRunPassesEnvironmentOverridesToChild(t *testing.T) {
	if os.Getenv("ENVMAN_PROCESS_HELPER") == "1" {
		if os.Getenv("ENVMAN_RUN_VALUE") != "from-envman" {
			os.Exit(2)
		}
		return
	}

	var stdout, stderr bytes.Buffer
	err := Run(os.Args[0], []string{"-test.run=^TestRunPassesEnvironmentOverridesToChild$"},
		map[string]string{"ENVMAN_PROCESS_HELPER": "1", "ENVMAN_RUN_VALUE": "from-envman"},
		bytes.NewReader(nil), io.Writer(&stdout), io.Writer(&stderr))
	if err != nil {
		t.Fatalf("run child process: %v; stderr: %s", err, stderr.String())
	}
}
